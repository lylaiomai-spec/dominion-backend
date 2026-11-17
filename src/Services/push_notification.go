package Services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	vapidPrivateKeySetting = "vapid_private_key"
	vapidPublicKeySetting  = "vapid_public_key"
)

func GetOrCreateVAPIDKeys(db *sql.DB) (public, private string, err error) {
	public, _ = GetGlobalSetting(vapidPublicKeySetting, db)
	private, _ = GetGlobalSetting(vapidPrivateKeySetting, db)
	if public != "" && private != "" {
		return public, private, nil
	}

	private, public, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generate VAPID keys: %w", err)
	}

	_, err = db.Exec(
		`INSERT INTO global_settings (setting_name, setting_value) VALUES (?, ?), (?, ?)
		 ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)`,
		vapidPublicKeySetting, public, vapidPrivateKeySetting, private,
	)
	if err != nil {
		return "", "", fmt.Errorf("save VAPID keys: %w", err)
	}
	return public, private, nil
}

func SendPushToUser(db *sql.DB, userID int, notificationType, title, message string, data interface{}) {
	privateKey, err := GetGlobalSetting(vapidPrivateKeySetting, db)
	if err != nil || privateKey == "" {
		return
	}
	publicKey, err := GetGlobalSetting(vapidPublicKeySetting, db)
	if err != nil || publicKey == "" {
		return
	}
	domain, _ := GetGlobalSetting("domain", db)
	subject := "mailto:admin@example.com"
	if domain != "" {
		subject = "https://" + domain
	}

	rows, err := db.Query(
		"SELECT endpoint, p256dh, auth FROM user_push_subscriptions WHERE user_id = ?", userID,
	)
	if err != nil {
		log.Printf("SendPushToUser: loading subscriptions for user %d: %v", userID, err)
		return
	}
	defer rows.Close()

	type pushPayload struct {
		Type    string      `json:"type"`
		Title   string      `json:"title"`
		Message string      `json:"message"`
		Data    interface{} `json:"data,omitempty"`
	}
	payloadBytes, _ := json.Marshal(pushPayload{Type: notificationType, Title: title, Message: message, Data: data})
	payload := string(payloadBytes)

	for rows.Next() {
		var endpoint, p256dh, auth string
		if err := rows.Scan(&endpoint, &p256dh, &auth); err != nil {
			continue
		}
		sub := &webpush.Subscription{
			Endpoint: endpoint,
			Keys: webpush.Keys{
				P256dh: p256dh,
				Auth:   auth,
			},
		}
		resp, err := webpush.SendNotification([]byte(payload), sub, &webpush.Options{
			VAPIDPublicKey:  publicKey,
			VAPIDPrivateKey: privateKey,
			Subscriber:      subject,
			TTL:             86400,
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			log.Printf("SendPushToUser: sending to user %d endpoint %s: %v", userID, endpoint, err)
			continue
		}
		resp.Body.Close()
		// Remove expired/invalid subscriptions (410 Gone, 404 Not Found).
		if resp.StatusCode == 410 || resp.StatusCode == 404 {
			db.Exec(
				"DELETE FROM user_push_subscriptions WHERE user_id = ? AND endpoint = ?",
				userID, endpoint,
			)
		}
	}
}

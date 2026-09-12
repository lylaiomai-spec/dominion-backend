package Services

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

var pushLogger *log.Logger

func LogPush(format string, args ...interface{}) {
	pushLogger.Printf(format, args...)
}

func init() {
	if err := os.MkdirAll("logs", 0755); err == nil {
		f, err := os.OpenFile("logs/push.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			pushLogger = log.New(f, "", log.LstdFlags)
		}
	}
	if pushLogger == nil {
		pushLogger = log.New(os.Stdout, "[push] ", log.LstdFlags)
	}
}

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

func SendPushToUser(db *sql.DB, userID int, notificationType, title, message string) {
	privateKey, err := GetGlobalSetting(vapidPrivateKeySetting, db)
	if err != nil || privateKey == "" {
		pushLogger.Printf("[push] no VAPID private key for user %d\n", userID)
		return
	}
	publicKey, err := GetGlobalSetting(vapidPublicKeySetting, db)
	if err != nil || publicKey == "" {
		pushLogger.Printf("[push] no VAPID public key for user %d\n", userID)
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
		pushLogger.Printf("[push] DB query error for user %d: %v\n", userID, err)
		return
	}
	defer rows.Close()

	payload := fmt.Sprintf(`{"type":%q,"title":%q,"message":%q}`, notificationType, title, message)

	sent := 0
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
		})
		if err != nil {
			pushLogger.Printf("[push] send error for user %d endpoint %s: %v\n", userID, endpoint, err)
			continue
		}
		pushLogger.Printf("[push] sent to user %d, status %d, endpoint %s\n", userID, resp.StatusCode, endpoint)
		resp.Body.Close()
		// Remove expired/invalid subscriptions (410 Gone, 404 Not Found).
		if resp.StatusCode == 410 || resp.StatusCode == 404 {
			db.Exec(
				"DELETE FROM user_push_subscriptions WHERE user_id = ? AND endpoint = ?",
				userID, endpoint,
			)
		}
		sent++
	}
	if sent == 0 {
		pushLogger.Printf("[push] no subscriptions found for user %d\n", userID)
	}
}

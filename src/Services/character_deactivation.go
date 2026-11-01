package Services

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"database/sql"
	"fmt"
)

type CharacterDeactivationResult struct {
	CharacterID    int
	UserID         int
	TopicID        int64
	SubforumID     int
	OldTopicStatus int
	OldCharStatus  int
}

// DeactivateCharacterInTx deactivates a character within an existing transaction:
// sets character_status to Inactive, deactivates its topic, archives its profiles,
// and frees any claim slot. Returns (result, found, error); found=false when the
// character ID does not exist.
func DeactivateCharacterInTx(tx *sql.Tx, db *sql.DB, characterID int) (CharacterDeactivationResult, bool, error) {
	var res CharacterDeactivationResult
	res.CharacterID = characterID
	_ = db.QueryRow(`
		SELECT t.id, t.subforum_id, t.status, cb.character_status, cb.user_id
		FROM topics t JOIN character_base cb ON cb.topic_id = t.id
		WHERE cb.id = ?`, characterID,
	).Scan(&res.TopicID, &res.SubforumID, &res.OldTopicStatus, &res.OldCharStatus, &res.UserID)

	result, err := tx.Exec("UPDATE character_base SET character_status = ? WHERE id = ?", Entities.InactiveCharacter, characterID)
	if err != nil {
		return res, false, fmt.Errorf("failed to deactivate character: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return res, false, nil
	}

	if res.TopicID > 0 {
		if _, err := tx.Exec("UPDATE topics SET status = ? WHERE id = ?", Entities.InactiveTopic, res.TopicID); err != nil {
			return res, true, fmt.Errorf("failed to deactivate character topic: %w", err)
		}
	}
	if _, err := tx.Exec("UPDATE character_profile_base SET is_archived = 1 WHERE character_id = ?", characterID); err != nil {
		return res, true, fmt.Errorf("failed to archive character profiles: %w", err)
	}

	var claimID int
	if err := tx.QueryRow("SELECT claim_id FROM claim_record WHERE character_id = ? ORDER BY claim_date DESC LIMIT 1", characterID).Scan(&claimID); err == nil {
		_, _ = tx.Exec("UPDATE character_claim SET is_claimed = false WHERE id = ?", claimID)
		_, _ = tx.Exec("UPDATE wanted_character_base SET is_claimed = false WHERE character_claim_id = ?", claimID)
	}

	return res, true, nil
}

// PublishCharacterDeactivatedEvents fires CharacterStatusChanged, CharacterDeactivated,
// and (when the topic was previously active) TopicStatusChanged.
func PublishCharacterDeactivatedEvents(db *sql.DB, res CharacterDeactivationResult) {
	Events.Publish(db, Events.CharacterStatusChanged, Events.CharacterStatusChangedEvent{
		CharacterID: res.CharacterID,
		UserID:      res.UserID,
		OldStatus:   Entities.CharacterStatus(res.OldCharStatus),
		NewStatus:   Entities.InactiveCharacter,
	})
	Events.Publish(db, Events.CharacterDeactivated, Events.CharacterDeactivatedEvent{CharacterID: res.CharacterID})
	if res.TopicID > 0 && res.OldTopicStatus != int(Entities.InactiveTopic) {
		Events.Publish(db, Events.TopicStatusChanged, Events.TopicStatusChangedEvent{
			TopicID:    res.TopicID,
			SubforumID: res.SubforumID,
			OldStatus:  res.OldTopicStatus,
			NewStatus:  int(Entities.InactiveTopic),
		})
	}
}

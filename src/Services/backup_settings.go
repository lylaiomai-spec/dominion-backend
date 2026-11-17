package Services

import (
	"database/sql"
	"time"
)

type BackupSettingsRow struct {
	GdriveEnabled      bool
	GdriveFrequency    string
	GdriveKeep         int
	GdriveLastRun      *time.Time
	GdriveFolderID     string
	GdriveClientID     string
	GdriveClientSecret string
	GdriveRefreshToken string
}

func GetBackupSettings(db *sql.DB) (BackupSettingsRow, error) {
	var row BackupSettingsRow
	var folderID, clientID, clientSecret, refreshToken sql.NullString
	var lastRun sql.NullTime
	err := db.QueryRow(`
		SELECT gdrive_enabled, gdrive_frequency, gdrive_keep, gdrive_last_run,
		       gdrive_folder_id, gdrive_client_id, gdrive_client_secret, gdrive_refresh_token
		FROM backup_settings WHERE id = 1`).
		Scan(&row.GdriveEnabled, &row.GdriveFrequency, &row.GdriveKeep, &lastRun,
			&folderID, &clientID, &clientSecret, &refreshToken)
	if err != nil {
		return row, err
	}
	row.GdriveFolderID = folderID.String
	row.GdriveClientID = clientID.String
	row.GdriveClientSecret = clientSecret.String
	row.GdriveRefreshToken = refreshToken.String
	if lastRun.Valid {
		row.GdriveLastRun = &lastRun.Time
	}
	return row, nil
}

func SaveBackupSettings(enabled bool, frequency string, keep int, folderID, clientID, clientSecret, refreshToken string, db *sql.DB) error {
	_, err := db.Exec(`
		UPDATE backup_settings
		SET gdrive_enabled       = ?,
		    gdrive_frequency     = ?,
		    gdrive_keep          = ?,
		    gdrive_folder_id     = ?,
		    gdrive_client_id     = ?,
		    gdrive_client_secret = ?,
		    gdrive_refresh_token = ?
		WHERE id = 1`, enabled, frequency, keep, folderID, clientID, clientSecret, refreshToken)
	return err
}

// GetBackupSetting reads a single column from backup_settings by logical name.
// Used by the scheduler and GDrive provider to avoid re-reading the whole row.
func GetBackupSetting(name string, db *sql.DB) (string, error) {
	col := backupSettingCol(name)
	if col == "" {
		return "", nil
	}
	var value sql.NullString
	err := db.QueryRow("SELECT "+col+" FROM backup_settings WHERE id = 1").Scan(&value)
	return value.String, err
}

func backupSettingCol(name string) string {
	switch name {
	case "gdrive_backup_frequency":
		return "gdrive_frequency"
	case "gdrive_backup_keep":
		return "gdrive_keep"
	case "gdrive_folder_id":
		return "gdrive_folder_id"
	}
	return ""
}

package Services

import (
	"database/sql"
	"time"
)

type BackupSettingsRow struct {
	GdriveServiceAccountKey string
	GdriveEnabled           bool
	GdriveFrequency         string
	GdriveKeep              int
	GdriveLastRun           *time.Time
}

func GetBackupSettings(db *sql.DB) (BackupSettingsRow, error) {
	var row BackupSettingsRow
	var key sql.NullString
	var lastRun sql.NullTime
	err := db.QueryRow(`
		SELECT gdrive_service_account_key, gdrive_enabled, gdrive_frequency, gdrive_keep, gdrive_last_run
		FROM backup_settings WHERE id = 1`).
		Scan(&key, &row.GdriveEnabled, &row.GdriveFrequency, &row.GdriveKeep, &lastRun)
	if err != nil {
		return row, err
	}
	row.GdriveServiceAccountKey = key.String
	if lastRun.Valid {
		row.GdriveLastRun = &lastRun.Time
	}
	return row, nil
}

func SaveBackupSettings(key string, enabled bool, frequency string, keep int, db *sql.DB) error {
	_, err := db.Exec(`
		UPDATE backup_settings
		SET gdrive_service_account_key = ?,
		    gdrive_enabled             = ?,
		    gdrive_frequency           = ?,
		    gdrive_keep                = ?
		WHERE id = 1`, key, enabled, frequency, keep)
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
	case "gdrive_service_account_key":
		return "gdrive_service_account_key"
	case "gdrive_backup_enabled":
		return "gdrive_enabled"
	case "gdrive_backup_frequency":
		return "gdrive_frequency"
	case "gdrive_backup_keep":
		return "gdrive_keep"
	case "gdrive_backup_last_run":
		return "gdrive_last_run"
	}
	return ""
}

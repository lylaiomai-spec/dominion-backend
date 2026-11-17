package Services

import (
	"bytes"
	"cuento-backend/config"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"time"
)

// BackupStorageProvider is the interface backup targets must implement.
// Swap Google Drive for any other provider (S3, Backblaze, etc.) by
// implementing this interface and returning it from resolveBackupProvider.
type BackupStorageProvider interface {
	Upload(filename string, data []byte) error
	ListBackups() ([]BackupFile, error)
	Download(fileID string) (io.ReadCloser, string, error)
	Prune(keep int) error
}

// BackupFile is a provider-agnostic description of a stored backup.
type BackupFile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}

// resolveBackupProvider returns the configured provider, or nil if none is set up.
func resolveBackupProvider(db *sql.DB) BackupStorageProvider {
	settings, err := GetBackupSettings(db)
	if err != nil || settings.GdriveClientID == "" || settings.GdriveRefreshToken == "" {
		return nil
	}
	return NewGDriveProvider(db)
}

func StartBackupScheduler(db *sql.DB) {
	go func() {
		runScheduledBackupIfDue(db)
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runScheduledBackupIfDue(db)
		}
	}()
}

func runScheduledBackupIfDue(db *sql.DB) {
	settings, err := GetBackupSettings(db)
	if err != nil {
		log.Printf("Backup scheduler: failed to read settings: %v", err)
		return
	}
	if !settings.GdriveEnabled {
		return
	}
	frequency := settings.GdriveFrequency
	if frequency == "" {
		frequency = "daily"
	}

	due := settings.GdriveLastRun == nil
	if !due {
		now := time.Now().UTC()
		lastRun := settings.GdriveLastRun.UTC()
		switch frequency {
		case "daily":
			due = now.Sub(lastRun) >= 24*time.Hour
		case "weekly":
			due = now.Sub(lastRun) >= 7*24*time.Hour
		case "monthly":
			due = now.Year() > lastRun.Year() || now.Month() > lastRun.Month()
		}
	}

	if !due {
		return
	}
	log.Printf("Backup scheduler: starting %s backup", frequency)
	if err := RunCloudBackup(db); err != nil {
		log.Printf("Backup scheduler: backup failed: %v", err)
	} else {
		log.Printf("Backup scheduler: backup completed successfully")
	}
}

func RunCloudBackup(db *sql.DB) error {
	provider := resolveBackupProvider(db)
	if provider == nil {
		return fmt.Errorf("no backup provider configured")
	}

	keepStr, _ := GetBackupSetting("gdrive_backup_keep", db)
	keep := 5
	if n, err := strconv.Atoi(keepStr); err == nil && n > 0 {
		keep = n
	}

	log.Printf("Backup scheduler: dumping database")
	var buf bytes.Buffer
	if err := MysqldumpToWriter(&buf); err != nil {
		return fmt.Errorf("mysqldump: %w", err)
	}
	log.Printf("Backup scheduler: dump size %d bytes", buf.Len())

	filename := fmt.Sprintf("backup_%s.sql", time.Now().UTC().Format("2006-01-02_15-04-05"))
	log.Printf("Backup scheduler: uploading %s", filename)
	if err := provider.Upload(filename, buf.Bytes()); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	db.Exec(`UPDATE backup_settings SET gdrive_last_run = ? WHERE id = 1`, time.Now().UTC())

	if err := provider.Prune(keep); err != nil {
		log.Printf("Backup scheduler: prune failed: %v", err)
	}
	return nil
}

func MysqlRestoreFromReader(r io.Reader) error {
	cfg := config.LoadDBConfig()
	cmd := exec.Command("mysql",
		"-h", cfg.Host,
		"-P", cfg.Port,
		"-u", cfg.User,
		"-p"+cfg.Password,
		cfg.Name,
	)
	cmd.Stdin = r
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", stderrBuf.String(), err)
	}
	return nil
}

func MysqldumpToWriter(w io.Writer) error {
	cfg := config.LoadDBConfig()
	cmd := exec.Command("mysqldump",
		"-h", cfg.Host,
		"-P", cfg.Port,
		"-u", cfg.User,
		"-p"+cfg.Password,
		"--single-transaction",
		"--routines",
		"--triggers",
		cfg.Name,
	)
	cmd.Stdout = w
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", stderrBuf.String(), err)
	}
	return nil
}

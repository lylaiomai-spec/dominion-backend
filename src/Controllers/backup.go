package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func BackupDB(c *gin.Context) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}

	filename := "backup_" + time.Now().UTC().Format("2006-01-02_15-04-05") + ".sql"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/sql")
	c.Status(http.StatusOK)

	if err := Services.MysqldumpToWriter(c.Writer); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Backup failed: " + err.Error()})
		c.Abort()
	}
}

func RestoreDB(c *gin.Context) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "SQL file is required"})
		c.Abort()
		return
	}
	defer file.Close()

	if err := Services.MysqlRestoreFromReader(file); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Restore failed: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Database restored successfully"})
}

type BackupFileResponse struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	CreatedTime         string `json:"created_time"`
	CreatedTimeLocalized string `json:"created_time_localized,omitempty"`
}

type GDriveBackupSettings struct {
	ClientID         string               `json:"client_id"`
	ClientSecret     string               `json:"client_secret"`
	RefreshToken     string               `json:"refresh_token"`
	Enabled          bool                 `json:"enabled"`
	Frequency        string               `json:"frequency"`
	Keep             int                  `json:"keep"`
	FolderID         string               `json:"folder_id"`
	LastRun          *time.Time           `json:"last_run"`
	LastRunLocalized *string              `json:"last_run_localized,omitempty"`
	Backups          []BackupFileResponse `json:"backups"`
}

func GetGDriveBackupSettings(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}

	row, err := Services.GetBackupSettings(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to load settings"})
		c.Abort()
		return
	}

	var rawBackups []Services.BackupFile
	if row.GdriveClientID != "" && row.GdriveRefreshToken != "" {
		rawBackups, _ = Services.NewGDriveProvider(db).ListBackups()
	}

	backups := make([]BackupFileResponse, 0, len(rawBackups))
	for _, b := range rawBackups {
		backups = append(backups, BackupFileResponse{
			ID:                   b.ID,
			Name:                 b.Name,
			CreatedTime:          b.CreatedTime.UTC().Format(time.RFC3339),
			CreatedTimeLocalized: Services.LocalizeTime(c, b.CreatedTime),
		})
	}

	var lastRunLocalized *string
	if row.GdriveLastRun != nil {
		s := Services.LocalizeTime(c, *row.GdriveLastRun)
		lastRunLocalized = &s
	}

	c.JSON(http.StatusOK, GDriveBackupSettings{
		ClientID:         row.GdriveClientID,
		ClientSecret:     row.GdriveClientSecret,
		RefreshToken:     row.GdriveRefreshToken,
		Enabled:          row.GdriveEnabled,
		Frequency:        row.GdriveFrequency,
		Keep:             row.GdriveKeep,
		FolderID:         row.GdriveFolderID,
		LastRun:          row.GdriveLastRun,
		LastRunLocalized: lastRunLocalized,
		Backups:          backups,
	})
}

type UpdateGDriveBackupSettingsRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	Enabled      bool   `json:"enabled"`
	Frequency    string `json:"frequency"`
	Keep         int    `json:"keep"`
	FolderID     string `json:"folder_id"`
}

func UpdateGDriveBackupSettings(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}

	var req UpdateGDriveBackupSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request"})
		c.Abort()
		return
	}

	if req.Frequency != "daily" && req.Frequency != "weekly" && req.Frequency != "monthly" {
		req.Frequency = "daily"
	}
	if req.Keep < 1 {
		req.Keep = 1
	}

	if err := Services.SaveBackupSettings(req.Enabled, req.Frequency, req.Keep, req.FolderID, req.ClientID, req.ClientSecret, req.RefreshToken, db); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save settings"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings saved"})
}

func DownloadGDriveBackup(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}

	fileID := c.Param("fileId")
	if fileID == "" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Missing file ID"})
		c.Abort()
		return
	}

	provider := Services.NewGDriveProvider(db)
	reader, filename, err := provider.Download(fileID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Download failed: " + err.Error()})
		c.Abort()
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/sql")
	c.Status(http.StatusOK)
	io.Copy(c.Writer, reader)
}

func TriggerGDriveBackup(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}
	if err := Services.RunCloudBackup(db); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Backup failed: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Backup completed"})
}

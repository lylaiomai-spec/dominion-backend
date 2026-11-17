package Services

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const gdriveFolderName = "Cuento Backups"

// GDriveProvider implements BackupStorageProvider using Google Drive.
type GDriveProvider struct {
	db *sql.DB
}

func NewGDriveProvider(db *sql.DB) *GDriveProvider {
	return &GDriveProvider{db: db}
}

func (p *GDriveProvider) Upload(filename string, data []byte) error {
	svc, err := p.service()
	if err != nil {
		return err
	}
	folderID, err := p.backupFolderID(svc)
	if err != nil {
		return err
	}
	_, err = svc.Files.Create(&drive.File{
		Name:    filename,
		Parents: []string{folderID},
	}).SupportsAllDrives(true).Media(bytes.NewReader(data)).Do()
	return err
}

func (p *GDriveProvider) ListBackups() ([]BackupFile, error) {
	svc, err := p.service()
	if err != nil {
		return nil, err
	}
	folderID, err := p.backupFolderID(svc)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	list, err := svc.Files.List().Q(q).Fields("files(id,name,createdTime)").OrderBy("createdTime desc").
		SupportsAllDrives(true).IncludeItemsFromAllDrives(true).Do()
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	files := make([]BackupFile, 0, len(list.Files))
	for _, f := range list.Files {
		t, _ := time.Parse(time.RFC3339, f.CreatedTime)
		files = append(files, BackupFile{ID: f.Id, Name: f.Name, CreatedTime: t})
	}
	return files, nil
}

func (p *GDriveProvider) Download(fileID string) (io.ReadCloser, string, error) {
	svc, err := p.service()
	if err != nil {
		return nil, "", err
	}
	meta, err := svc.Files.Get(fileID).Fields("name").Do()
	if err != nil {
		return nil, "", fmt.Errorf("get file metadata: %w", err)
	}
	resp, err := svc.Files.Get(fileID).Download()
	if err != nil {
		return nil, "", fmt.Errorf("download file: %w", err)
	}
	return resp.Body, meta.Name, nil
}

func (p *GDriveProvider) Prune(keep int) error {
	if keep <= 0 {
		return nil
	}
	svc, err := p.service()
	if err != nil {
		return err
	}
	folderID, err := p.backupFolderID(svc)
	if err != nil {
		return err
	}
	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	list, err := svc.Files.List().Q(q).Fields("files(id,name)").OrderBy("createdTime desc").
		SupportsAllDrives(true).IncludeItemsFromAllDrives(true).Do()
	if err != nil {
		return fmt.Errorf("list backups for pruning: %w", err)
	}
	if len(list.Files) <= keep {
		return nil
	}
	for _, f := range list.Files[keep:] {
		if err := svc.Files.Delete(f.Id).SupportsAllDrives(true).Do(); err != nil {
			log.Printf("GDrive: failed to delete old backup %s (%s): %v", f.Name, f.Id, err)
		}
	}
	return nil
}

func (p *GDriveProvider) service() (*drive.Service, error) {
	settings, err := GetBackupSettings(p.db)
	if err != nil {
		return nil, fmt.Errorf("read backup settings: %w", err)
	}
	if settings.GdriveClientID == "" || settings.GdriveClientSecret == "" || settings.GdriveRefreshToken == "" {
		return nil, fmt.Errorf("google drive oauth2 credentials not configured")
	}
	cfg := &oauth2.Config{
		ClientID:     settings.GdriveClientID,
		ClientSecret: settings.GdriveClientSecret,
		Scopes:       []string{drive.DriveScope},
		Endpoint:     google.Endpoint,
	}
	token := &oauth2.Token{RefreshToken: settings.GdriveRefreshToken}
	svc, err := drive.NewService(context.Background(),
		option.WithTokenSource(cfg.TokenSource(context.Background(), token)),
	)
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}
	return svc, nil
}

// backupFolderID returns the configured folder ID if one is set, otherwise
// searches for (and creates) a "Cuento Backups" folder in the service account's
// Drive. Note: service accounts have no personal storage quota, so the
// auto-create path only works when a user-owned folder has been shared with the
// service account and its ID stored in backup settings.
func (p *GDriveProvider) backupFolderID(svc *drive.Service) (string, error) {
	id, _ := GetBackupSetting("gdrive_folder_id", p.db)
	if id != "" {
		return id, nil
	}
	q := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", gdriveFolderName)
	list, err := svc.Files.List().Q(q).Fields("files(id)").
		SupportsAllDrives(true).IncludeItemsFromAllDrives(true).Do()
	if err != nil {
		return "", fmt.Errorf("search backup folder: %w", err)
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}
	folder, err := svc.Files.Create(&drive.File{
		Name:     gdriveFolderName,
		MimeType: "application/vnd.google-apps.folder",
	}).SupportsAllDrives(true).Fields("id").Do()
	if err != nil {
		return "", fmt.Errorf("create backup folder: %w", err)
	}
	return folder.Id, nil
}

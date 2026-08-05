package Services

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gabriel-vasile/mimetype"
	_ "golang.org/x/image/webp"
)

var localImageDir = func() string {
	if v := os.Getenv("LOCAL_IMAGE_DIR"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "../frontend/avatars"
}()

const maxAvatarInputBytes = 10 << 20 // 10 MB

var allowedAvatarMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/bmp":  true,
	"image/webp": true,
}

func SaveUserAvatar(data []byte, userID int, db *sql.DB) (string, error) {
	wStr, _ := GetGlobalSetting("user_avatar_width", db)
	hStr, _ := GetGlobalSetting("user_avatar_height", db)
	w, _ := strconv.Atoi(wStr)
	h, _ := strconv.Atoi(hStr)
	return saveAvatar(data, "user", userID, w, h)
}

func SaveCharacterAvatar(data []byte, characterID int, db *sql.DB) (string, error) {
	wStr, _ := GetGlobalSetting("character_avatar_width", db)
	hStr, _ := GetGlobalSetting("character_avatar_height", db)
	w, _ := strconv.Atoi(wStr)
	h, _ := strconv.Atoi(hStr)
	return saveAvatar(data, "character", characterID, w, h)
}

func SaveCharacterProfileAvatar(data []byte, profileID int, db *sql.DB) (string, error) {
	wStr, _ := GetGlobalSetting("user_avatar_width", db)
	hStr, _ := GetGlobalSetting("user_avatar_height", db)
	w, _ := strconv.Atoi(wStr)
	h, _ := strconv.Atoi(hStr)
	return saveAvatar(data, "character_profile", profileID, w, h)
}

func saveAvatar(data []byte, subdir string, id int, width, height int) (string, error) {
	if len(data) > maxAvatarInputBytes {
		return "", errors.New("image exceeds maximum allowed size of 10 MB")
	}

	mime := mimetype.Detect(data)
	if !allowedAvatarMIMEs[mime.String()] {
		return "", fmt.Errorf("unsupported image type: %s", mime.String())
	}

	src, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("invalid or corrupt image data")
	}

	switch {
	case width > 0 && height > 0:
		src = imaging.Fill(src, width, height, imaging.Center, imaging.Lanczos)
	case width > 0:
		src = imaging.Resize(src, width, 0, imaging.Lanczos)
	case height > 0:
		src = imaging.Resize(src, 0, height, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85}); err != nil {
		return "", errors.New("failed to encode image")
	}

	dir := filepath.Join(localImageDir, subdir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create storage directory: %w", err)
	}

	filename := strconv.Itoa(id) + ".jpg"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, buf.Bytes(), 0664); err != nil {
		return "", fmt.Errorf("failed to write image: %w", err)
	}

	if err := chownWwwData(path); err != nil {
		// Non-fatal: file was written; log and continue.
		_ = err
	}

	return "/avatars/" + subdir + "/" + filename, nil
}

func chownWwwData(path string) error {
	grp, err := user.LookupGroup("www-data")
	if err != nil {
		return err
	}
	gid, _ := strconv.Atoi(grp.Gid)
	return os.Chown(path, -1, gid)
}

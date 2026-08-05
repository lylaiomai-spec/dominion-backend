package Services

import (
	"bytes"
	"database/sql"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"

	"github.com/disintegration/imaging"
)

// GetResizedImageURL returns a resized copy of the image at originalURL, cropped to
// width×height. Results are cached in resized_image_cache so each unique
// (url, width, height) triple is only processed once.
//
// If image uploading is disabled, the original URL is returned unchanged.
// If either dimension is 0, the image is resized to fit the non-zero dimension
// while preserving aspect ratio.
func GetResizedImageURL(originalURL string, width, height int, db *sql.DB) (string, error) {
	if val, _ := GetGlobalSetting("use_image_uploading", db); val != "y" {
		return originalURL, nil
	}

	var cached string
	err := db.QueryRow(
		"SELECT resized_url FROM resized_image_cache WHERE original_url = ? AND width = ? AND height = ?",
		originalURL, width, height,
	).Scan(&cached)
	if err == nil {
		return cached, nil
	}
	if err != sql.ErrNoRows {
		return originalURL, fmt.Errorf("cache lookup failed: %w", err)
	}

	resp, err := http.Get(originalURL)
	if err != nil {
		return originalURL, fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return originalURL, fmt.Errorf("failed to read image: %w", err)
	}

	src, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return originalURL, fmt.Errorf("failed to decode image: %w", err)
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
		return originalURL, fmt.Errorf("failed to encode image: %w", err)
	}

	result, err := UploadImageToImgbb(buf.Bytes(), db)
	if err != nil {
		return originalURL, fmt.Errorf("failed to upload resized image: %w", err)
	}

	_, _ = db.Exec(
		"INSERT INTO resized_image_cache (original_url, width, height, resized_url) VALUES (?, ?, ?, ?)",
		originalURL, width, height, result.URL,
	)

	return result.URL, nil
}

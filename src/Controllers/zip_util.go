package Controllers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".bmp": true,
}

// extractZipToTemp extracts image files from zipData into a fresh /tmp directory
// named <prefix>_*. The caller must os.RemoveAll the returned path when done.
// Files are flattened (subdirectories ignored) and path-traversal is prevented.
func extractZipToTemp(zipData []byte, prefix string) (string, error) {
	tmpDir, err := os.MkdirTemp("/tmp", prefix+"_*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("invalid zip: %w", err)
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(f.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, ".") {
			continue
		}
		if !imageExts[strings.ToLower(filepath.Ext(name))] {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("failed to open %s: %w", name, err)
		}
		dst, err := os.Create(filepath.Join(tmpDir, name))
		if err != nil {
			rc.Close()
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("failed to create %s: %w", name, err)
		}
		_, cpErr := io.Copy(dst, rc)
		rc.Close()
		dst.Close()
		if cpErr != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("failed to extract %s: %w", name, cpErr)
		}
	}

	return tmpDir, nil
}

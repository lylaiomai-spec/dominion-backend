package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)


const backendLocaleDir = "./locales"
const localeConfigPath = "src/locale_config.ts"
const localeConfigDefaultPath = "src/locale_config_default.ts"

var protectedLocaleCodes = map[string]bool{"en-CA": true}

type LocaleItem struct {
	Id                int    `json:"id"`
	HumanName         string `json:"human_name"`
	Code              string `json:"code"`
	FrontEndFileName  string `json:"front_end_file_name"`
	BackEndFileName   string `json:"back_end_file_name"`
	IsInstalled       bool   `json:"is_installed"`
}

func GetLocales(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, human_name, code, front_end_file_name, back_end_file_name, is_installed FROM locales ORDER BY id ASC")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get locales: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	locales := []LocaleItem{}
	for rows.Next() {
		var l LocaleItem
		if err := rows.Scan(&l.Id, &l.HumanName, &l.Code, &l.FrontEndFileName, &l.BackEndFileName, &l.IsInstalled); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan locale: " + err.Error()})
			c.Abort()
			return
		}
		locales = append(locales, l)
	}

	c.JSON(http.StatusOK, locales)
}

func DownloadFrontEndLocaleFile(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid locale ID"})
		c.Abort()
		return
	}

	var fileName string
	if err := db.QueryRow("SELECT front_end_file_name FROM locales WHERE id = ?", id).Scan(&fileName); err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Locale not found"})
		c.Abort()
		return
	} else if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query locale: " + err.Error()})
		c.Abort()
		return
	}

	path := filepath.Join(backendLocaleDir, filepath.Base(fileName))
	content, err := os.ReadFile(path)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Frontend locale file not found on disk"})
		c.Abort()
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+fileName)
	c.Data(http.StatusOK, "application/typescript", content)
}

func DownloadBackEndLocaleFile(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid locale ID"})
		c.Abort()
		return
	}

	var fileName string
	if err := db.QueryRow("SELECT back_end_file_name FROM locales WHERE id = ?", id).Scan(&fileName); err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Locale not found"})
		c.Abort()
		return
	} else if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query locale: " + err.Error()})
		c.Abort()
		return
	}

	path := filepath.Join(backendLocaleDir, filepath.Base(fileName))
	content, err := os.ReadFile(path)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Backend locale file not found on disk"})
		c.Abort()
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+fileName)
	c.Data(http.StatusOK, "application/json", content)
}

func UploadLocale(c *gin.Context, db *sql.DB) {
	humanName := c.PostForm("human_name")
	code := c.PostForm("code")
	if humanName == "" || code == "" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "human_name and code are required"})
		c.Abort()
		return
	}

	tsFile, err := c.FormFile("frontend_file")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "frontend_file (.ts) is required"})
		c.Abort()
		return
	}
	jsonFile, err := c.FormFile("backend_file")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "backend_file (.json) is required"})
		c.Abort()
		return
	}

	tsName := filepath.Base(tsFile.Filename)
	jsonName := filepath.Base(jsonFile.Filename)

	if !strings.HasSuffix(tsName, ".ts") {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "frontend_file must be a .ts file"})
		c.Abort()
		return
	}
	if !strings.HasSuffix(jsonName, ".json") {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "backend_file must be a .json file"})
		c.Abort()
		return
	}

	tsBase := strings.TrimSuffix(tsName, ".ts")
	jsonBase := strings.TrimSuffix(jsonName, ".json")
	if tsBase != jsonBase {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "frontend and backend files must have the same base name"})
		c.Abort()
		return
	}

	for _, name := range []string{tsName, jsonName} {
		if _, err := os.Stat(filepath.Join(backendLocaleDir, name)); err == nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "File already exists on server: " + name})
			c.Abort()
			return
		}
	}

	if err := saveUploadedFile(tsFile, filepath.Join(backendLocaleDir, tsName)); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save .ts file: " + err.Error()})
		c.Abort()
		return
	}
	if err := saveUploadedFile(jsonFile, filepath.Join(backendLocaleDir, jsonName)); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save .json file: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO locales (human_name, code, front_end_file_name, back_end_file_name, is_installed) VALUES (?, ?, ?, ?, 0)",
		humanName, code, tsName, jsonName,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save locale: " + err.Error()})
		c.Abort()
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "front_end_file_name": tsName, "back_end_file_name": jsonName})
}

func InstallLocale(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid locale ID"})
		c.Abort()
		return
	}

	var locale LocaleItem
	err = db.QueryRow(
		"SELECT id, human_name, code, front_end_file_name, back_end_file_name, is_installed FROM locales WHERE id = ?", id,
	).Scan(&locale.Id, &locale.HumanName, &locale.Code, &locale.FrontEndFileName, &locale.BackEndFileName, &locale.IsInstalled)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Locale not found"})
		c.Abort()
		return
	} else if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "DB error: " + err.Error()})
		c.Abort()
		return
	}

	if locale.IsInstalled {
		c.JSON(http.StatusOK, gin.H{"installed": locale.Code})
		return
	}

	tsContent, err := os.ReadFile(filepath.Join(backendLocaleDir, filepath.Base(locale.FrontEndFileName)))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Frontend locale file not found on disk"})
		c.Abort()
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	// Read current locale_config.ts from GitHub.
	configContent, err := getLocaleConfig(cfg)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch locale_config.ts: " + err.Error()})
		c.Abort()
		return
	}

	tsFileName := filepath.Base(locale.FrontEndFileName)
	updatedConfig := addLocaleBlock(configContent, locale.Code, strings.TrimSuffix(tsFileName, ".ts"))

	tsPath := "src/locale/" + tsFileName
	files := []Services.GitHubFile{
		{Path: tsPath, Content: string(tsContent)},
		{Path: localeConfigPath, Content: updatedConfig},
	}
	if err := Services.GitHubCommit(cfg, "Install locale: "+locale.Code, files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	_, _ = db.Exec("UPDATE locales SET is_installed = 1 WHERE id = ?", id)
	c.JSON(http.StatusOK, gin.H{"installed": locale.Code})
}

func UninstallLocale(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid locale ID"})
		c.Abort()
		return
	}

	var locale LocaleItem
	err = db.QueryRow(
		"SELECT id, human_name, code, front_end_file_name, back_end_file_name, is_installed FROM locales WHERE id = ?", id,
	).Scan(&locale.Id, &locale.HumanName, &locale.Code, &locale.FrontEndFileName, &locale.BackEndFileName, &locale.IsInstalled)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Locale not found"})
		c.Abort()
		return
	} else if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "DB error: " + err.Error()})
		c.Abort()
		return
	}

	if protectedLocaleCodes[locale.Code] {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Cannot uninstall default locale: " + locale.Code})
		c.Abort()
		return
	}

	if !locale.IsInstalled {
		c.JSON(http.StatusOK, gin.H{"uninstalled": locale.Code})
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	configContent, err := getLocaleConfig(cfg)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch locale_config.ts: " + err.Error()})
		c.Abort()
		return
	}

	tsFileName := filepath.Base(locale.FrontEndFileName)
	updatedConfig := removeLocaleBlock(configContent, strings.TrimSuffix(tsFileName, ".ts"))

	tsPath := "src/locale/" + tsFileName
	files := []Services.GitHubFile{
		{Path: localeConfigPath, Content: updatedConfig},
		{Path: tsPath, Delete: true},
	}

	if err := Services.GitHubCommit(cfg, "Uninstall locale: "+locale.Code, files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	_, _ = db.Exec("UPDATE locales SET is_installed = 0 WHERE id = ?", id)
	c.JSON(http.StatusOK, gin.H{"uninstalled": locale.Code})
}

func DeleteLocale(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid locale ID"})
		c.Abort()
		return
	}

	var locale LocaleItem
	err = db.QueryRow(
		"SELECT id, human_name, code, front_end_file_name, back_end_file_name, is_installed FROM locales WHERE id = ?", id,
	).Scan(&locale.Id, &locale.HumanName, &locale.Code, &locale.FrontEndFileName, &locale.BackEndFileName, &locale.IsInstalled)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Locale not found"})
		c.Abort()
		return
	} else if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "DB error: " + err.Error()})
		c.Abort()
		return
	}

	if protectedLocaleCodes[locale.Code] {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Cannot delete default locale: " + locale.Code})
		c.Abort()
		return
	}

	if locale.IsInstalled {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "Uninstall the locale before deleting it"})
		c.Abort()
		return
	}

	_ = os.Remove(filepath.Join(backendLocaleDir, filepath.Base(locale.FrontEndFileName)))
	_ = os.Remove(filepath.Join(backendLocaleDir, filepath.Base(locale.BackEndFileName)))

	_, _ = db.Exec("DELETE FROM locales WHERE id = ?", id)
	c.JSON(http.StatusOK, gin.H{"deleted": locale.Code})
}

// getLocaleConfig reads locale_config.ts from the repo.
// If it doesn't exist (404), it falls back to locale_config_default.ts.
func getLocaleConfig(cfg Services.GitHubConfig) (string, error) {
	content, err := Services.GitHubGetFile(cfg, localeConfigPath)
	if err == nil {
		return content, nil
	}
	ghErr, ok := err.(*Services.GitHubError)
	if !ok || ghErr.StatusCode != 404 {
		return "", err
	}
	return Services.GitHubGetFile(cfg, localeConfigDefaultPath)
}

// saveUploadedFile writes a multipart file to dst without calling os.MkdirAll,
// which would try to chmod the parent directory and fail in containers.
func saveUploadedFile(fh *multipart.FileHeader, dst string) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

// angularLocaleKeys is the set of locale keys supported by the frontend's ANGULAR_LOCALES map.
var angularLocaleKeys = map[string]bool{
	"ru": true, "zh": true, "zh-Hans": true, "zh-Hant": true,
	"de": true, "fr": true, "es": true, "pt": true, "it": true,
	"ja": true, "ko": true, "pl": true, "uk": true, "tr": true,
	"ar": true,
}

// addLocaleBlock inserts a new locale block into the LOCALES array in locale_config.ts.
// fileBase is the filename without extension (e.g. "ru" or "russian") and drives the import path.
func addLocaleBlock(config, code, fileBase string) string {
	parts := strings.SplitN(code, "-", 2)
	langPrefix := strings.ToLower(parts[0])
	translationConst := "TRANSLATIONS_" + strings.ToUpper(strings.ReplaceAll(fileBase, "-", "_"))

	angularLocale := ""
	if angularLocaleKeys[langPrefix] {
		angularLocale = fmt.Sprintf("\n  angularLocale: '%s',", langPrefix)
	}

	block := fmt.Sprintf(`  {
    code: '%s',
    langPrefixes: ['%s'],
    translations: () => import('./locale/%s').then(m => m.%s),%s
  },
`, code, langPrefix, fileBase, translationConst, angularLocale)

	// Locate the LOCALES array declaration, then find its closing ]; and insert before it.
	localesIdx := strings.Index(config, "export const LOCALES")
	if localesIdx == -1 {
		return config
	}
	closeIdx := strings.Index(config[localesIdx:], "\n];")
	if closeIdx == -1 {
		return config
	}
	insertAt := localesIdx + closeIdx
	return config[:insertAt] + "\n" + block + config[insertAt+1:]
}

// removeLocaleBlock removes the locale block whose import path matches fileBase from locale_config.ts.
// Only operates inside the LOCALES array to avoid touching the rest of the file.
func removeLocaleBlock(config, fileBase string) string {
	localesIdx := strings.Index(config, "export const LOCALES")
	if localesIdx == -1 {
		return config
	}

	before := config[:localesIdx]
	arraySection := config[localesIdx:]

	importMarker := fmt.Sprintf("import('./locale/%s')", fileBase)
	lines := strings.Split(arraySection, "\n")
	var result []string
	var buf []string // lines of the current block being buffered
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inBlock {
			if trimmed == "{" {
				inBlock = true
				buf = []string{line}
			} else {
				result = append(result, line)
			}
			continue
		}

		buf = append(buf, line)

		if trimmed == "}," {
			inBlock = false
			// Discard the entire block if it contains the target import.
			blockContainsMarker := false
			for _, bl := range buf {
				if strings.Contains(bl, importMarker) {
					blockContainsMarker = true
					break
				}
			}
			if !blockContainsMarker {
				result = append(result, buf...)
			}
			buf = nil
		}
	}
	// Flush any remaining buffered lines (shouldn't happen in a well-formed file).
	result = append(result, buf...)
	return before + strings.Join(result, "\n")
}

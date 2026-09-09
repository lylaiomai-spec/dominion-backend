package Controllers

import (
	"cuento-backend/src/Middlewares"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
)

const frontendLocaleDir = "./../frontend/src/locale"
const backendLocaleDir = "./locales"

type LocaleItem struct {
	Id                int    `json:"id"`
	HumanName         string `json:"human_name"`
	FrontEndFileName  string `json:"front_end_file_name"`
	BackEndFileName   string `json:"back_end_file_name"`
}

func GetLocales(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, human_name, front_end_file_name, back_end_file_name FROM locales ORDER BY id ASC")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get locales: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	locales := []LocaleItem{}
	for rows.Next() {
		var l LocaleItem
		if err := rows.Scan(&l.Id, &l.HumanName, &l.FrontEndFileName, &l.BackEndFileName); err != nil {
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

	path := filepath.Join(frontendLocaleDir, filepath.Base(fileName))
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

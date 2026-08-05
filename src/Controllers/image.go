package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

func UploadImage(c *gin.Context, db *sql.DB) {
	if val, _ := Services.GetGlobalSetting("use_image_uploading", db); val != "y" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Image uploading is disabled"})
		c.Abort()
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "file field is required"})
		c.Abort()
		return
	}
	defer file.Close()

	imageData, err := io.ReadAll(file)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to read file"})
		c.Abort()
		return
	}

	result, err := Services.UploadImageToImgbb(imageData, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to upload image: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	_, err = db.Exec(
		"INSERT INTO images (url, thumbnail_url, user_id, delete_url) VALUES (?, ?, ?, ?)",
		result.URL, result.ThumbnailURL, userID, result.DeleteURL,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save image: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"url":           result.URL,
		"thumbnail_url": result.ThumbnailURL,
	})
}

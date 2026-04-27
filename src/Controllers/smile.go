package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetSmileList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, text_form, url, category FROM smiles ORDER BY category")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get smiles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var list []Entities.Smile
	for rows.Next() {
		var s Entities.Smile
		var textForm, category sql.NullString
		if err := rows.Scan(&s.Id, &textForm, &s.URL, &category); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan smile: " + err.Error()})
			c.Abort()
			return
		}
		s.TextForm = textForm.String
		s.Category = category.String
		list = append(list, s)
	}

	if list == nil {
		list = []Entities.Smile{}
	}

	c.JSON(http.StatusOK, list)
}

func UploadSmile(c *gin.Context, db *sql.DB) {
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

	var textForm, category *string
	if v := c.PostForm("text_form"); v != "" {
		textForm = &v
	}
	if v := c.PostForm("category"); v != "" {
		category = &v
	}

	res, err := db.Exec(
		"INSERT INTO smiles (text_form, url, category) VALUES (?, ?, ?)",
		textForm, result.URL, category,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save smile: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()

	smile := Entities.Smile{Id: int(id), URL: result.URL}
	if textForm != nil {
		smile.TextForm = *textForm
	}
	if category != nil {
		smile.Category = *category
	}

	c.JSON(http.StatusOK, smile)
}

package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetSmileCategoryList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, name FROM smile_category ORDER BY name")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get smile categories: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var list []Entities.SmileCategory
	for rows.Next() {
		var cat Entities.SmileCategory
		if err := rows.Scan(&cat.Id, &cat.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan smile category: " + err.Error()})
			c.Abort()
			return
		}
		list = append(list, cat)
	}

	if list == nil {
		list = []Entities.SmileCategory{}
	}

	c.JSON(http.StatusOK, list)
}

type SmileCategoryRequest struct {
	Name string `json:"name" binding:"required"`
}

func CreateSmileCategory(c *gin.Context, db *sql.DB) {
	var req SmileCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "name is required"})
		c.Abort()
		return
	}

	res, err := db.Exec("INSERT INTO smile_category (name) VALUES (?)", req.Name)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create smile category: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, Entities.SmileCategory{Id: int(id), Name: req.Name})
}

func UpdateSmileCategory(c *gin.Context, db *sql.DB) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var req SmileCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "name is required"})
		c.Abort()
		return
	}

	result, err := db.Exec("UPDATE smile_category SET name = ? WHERE id = ?", req.Name, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update smile category: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Smile category not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, Entities.SmileCategory{Id: id, Name: req.Name})
}

func DeleteSmileCategory(c *gin.Context, db *sql.DB) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM smile_category WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete smile category: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Smile category not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Smile category deleted"})
}

func DeleteSmile(c *gin.Context, db *sql.DB) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM smiles WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete smile: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Smile not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Smile deleted"})
}

type UpdateSmileCategoryRequest struct {
	CategoryId *int `json:"category_id"`
}

func UpdateCategoryId(c *gin.Context, db *sql.DB) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var req UpdateSmileCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body"})
		c.Abort()
		return
	}

	result, err := db.Exec("UPDATE smiles SET category_id = ? WHERE id = ?", req.CategoryId, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update smile category: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Smile not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Smile category updated"})
}

type SmileCategoryWithSmiles struct {
	Id     int              `json:"id"`
	Name   string           `json:"name"`
	Smiles []Entities.Smile `json:"smiles"`
}

func GetSmileTree(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT sc.id, sc.name, s.id, s.text_form, s.url
		FROM smile_category sc
		LEFT JOIN smiles s ON s.category_id = sc.id
		ORDER BY sc.name, s.id`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get smiles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var tree []SmileCategoryWithSmiles
	indexMap := map[int]int{}

	for rows.Next() {
		var catId int
		var catName string
		var smileId sql.NullInt64
		var textForm sql.NullString
		var url sql.NullString

		if err := rows.Scan(&catId, &catName, &smileId, &textForm, &url); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan smile row: " + err.Error()})
			c.Abort()
			return
		}

		idx, exists := indexMap[catId]
		if !exists {
			tree = append(tree, SmileCategoryWithSmiles{Id: catId, Name: catName, Smiles: []Entities.Smile{}})
			idx = len(tree) - 1
			indexMap[catId] = idx
		}

		if smileId.Valid {
			tree[idx].Smiles = append(tree[idx].Smiles, Entities.Smile{
				Id:       int(smileId.Int64),
				TextForm: textForm.String,
				URL:      url.String,
			})
		}
	}

	if tree == nil {
		tree = []SmileCategoryWithSmiles{}
	}

	c.JSON(http.StatusOK, tree)
}

func GetSmileList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT s.id, s.text_form, s.url, sc.id, sc.name
		FROM smiles s
		LEFT JOIN smile_category sc ON sc.id = s.category_id
		ORDER BY sc.name, s.id`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get smiles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var list []Entities.Smile
	for rows.Next() {
		var s Entities.Smile
		var textForm sql.NullString
		var catId sql.NullInt64
		var catName sql.NullString
		if err := rows.Scan(&s.Id, &textForm, &s.URL, &catId, &catName); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan smile: " + err.Error()})
			c.Abort()
			return
		}
		s.TextForm = textForm.String
		if catId.Valid {
			s.Category = &Entities.SmileCategory{Id: int(catId.Int64), Name: catName.String}
		}
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

	var textForm *string
	if v := c.PostForm("text_form"); v != "" {
		textForm = &v
	}
	var categoryId *int
	if v := c.PostForm("category_id"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			categoryId = &parsed
		}
	}

	res, err := db.Exec(
		"INSERT INTO smiles (text_form, url, category_id) VALUES (?, ?, ?)",
		textForm, result.URL, categoryId,
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
	if categoryId != nil {
		var cat Entities.SmileCategory
		if err := db.QueryRow("SELECT id, name FROM smile_category WHERE id = ?", *categoryId).Scan(&cat.Id, &cat.Name); err == nil {
			smile.Category = &cat
		}
	}

	c.JSON(http.StatusOK, smile)
}

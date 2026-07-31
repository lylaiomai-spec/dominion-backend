package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type PanelListItem struct {
	Key      string `json:"key"`
	IsHidden bool   `json:"is_hidden"`
}

type Panel struct {
	Key         string  `json:"key"`
	Content     *string `json:"content"`
	ContentHtml *string `json:"content_html"`
	IsHidden    bool    `json:"is_hidden"`
}

type UpdatePanelRequest struct {
	Content  *string `json:"content"`
	IsHidden *bool   `json:"is_hidden"`
}

type WidgetListItem struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	TemplateName string `json:"template_name"`
}

type CreateWidgetRequest struct {
	Name       string  `json:"name" binding:"required"`
	TemplateId int     `json:"template_id" binding:"required"`
	Config     *string `json:"config"`
}

type UpdateWidgetRequest struct {
	Name       *string `json:"name"`
	TemplateId *int    `json:"template_id"`
	Config     *string `json:"config"`
}

type EntityFieldItem struct {
	MachineFieldName string `json:"machine_field_name"`
	HumanFieldName   string `json:"human_field_name"`
	DataType         string `json:"data_type"`
	RenderType       string `json:"render_type"`
}

func GetEntityFields(c *gin.Context, db *sql.DB) {
	entityType := c.Param("entity_type")

	config, err := Services.GetFieldConfig(entityType, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get entity fields: " + err.Error()})
		c.Abort()
		return
	}

	fields := make([]EntityFieldItem, 0, len(config))
	for _, f := range config {
		fields = append(fields, EntityFieldItem{
			MachineFieldName: f.MachineFieldName,
			HumanFieldName:   f.HumanFieldName,
			DataType:         f.FieldType,
			RenderType:       f.ContentFieldType,
		})
	}

	c.JSON(http.StatusOK, fields)
}

func GetWidgetTypeList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, name FROM widget_types ORDER BY name")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get widget type list: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	types := make([]Entities.WidgetType, 0)
	for rows.Next() {
		var wt Entities.WidgetType
		if err := rows.Scan(&wt.Id, &wt.Name); err != nil {
			continue
		}
		types = append(types, wt)
	}

	c.JSON(http.StatusOK, types)
}

func GetWidgetList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT w.id, w.name, wt.name
		FROM widgets w
		JOIN widget_types wt ON w.template_id = wt.id
		ORDER BY w.name
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get widget list: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	widgets := make([]WidgetListItem, 0)
	for rows.Next() {
		var w WidgetListItem
		if err := rows.Scan(&w.Id, &w.Name, &w.TemplateName); err != nil {
			continue
		}
		widgets = append(widgets, w)
	}

	c.JSON(http.StatusOK, widgets)
}

func GetWidgetTypeConfigTemplate(c *gin.Context, db *sql.DB) {
	name := c.Param("name")

	var configTemplate sql.NullString
	err := db.QueryRow("SELECT config_template FROM widget_types WHERE name = ?", name).Scan(&configTemplate)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Widget type not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get widget type: " + err.Error()})
		}
		c.Abort()
		return
	}

	if configTemplate.Valid {
		c.String(http.StatusOK, configTemplate.String)
	} else {
		c.String(http.StatusOK, "")
	}
}

func GetWidget(c *gin.Context, db *sql.DB) {
	id := c.Param("id")

	var w Entities.Widget
	var config sql.NullString
	err := db.QueryRow("SELECT id, name, template_id, config FROM widgets WHERE id = ?", id).
		Scan(&w.Id, &w.Name, &w.TemplateId, &config)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Widget not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get widget: " + err.Error()})
		}
		c.Abort()
		return
	}

	if config.Valid {
		w.Config = &config.String
	}

	c.JSON(http.StatusOK, w)
}

func CreateWidget(c *gin.Context, db *sql.DB) {
	var req CreateWidgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec("INSERT INTO widgets (name, template_id, config) VALUES (?, ?, ?)", req.Name, req.TemplateId, req.Config)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create widget: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func DeleteWidget(c *gin.Context, db *sql.DB) {
	id := c.Param("id")

	_, err := db.Exec("DELETE FROM widgets WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete widget: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Widget deleted successfully"})
}

func UpdateWidget(c *gin.Context, db *sql.DB) {
	id := c.Param("id")

	var req UpdateWidgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	_, err := db.Exec(
		"UPDATE widgets SET name = COALESCE(?, name), template_id = COALESCE(?, template_id), config = COALESCE(?, config) WHERE id = ?",
		req.Name, req.TemplateId, req.Config, id,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update widget: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Widget updated successfully"})
}

func GetPanelList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT `key`, is_hidden FROM widget_panels ORDER BY `key`")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get panel list: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	panels := make([]PanelListItem, 0)
	for rows.Next() {
		var p PanelListItem
		if err := rows.Scan(&p.Key, &p.IsHidden); err != nil {
			continue
		}
		panels = append(panels, p)
	}

	c.JSON(http.StatusOK, panels)
}

func GetPanelByName(c *gin.Context, db *sql.DB) {
	key := c.Param("key")

	var p Panel
	var content sql.NullString
	err := db.QueryRow("SELECT `key`, content, is_hidden FROM widget_panels WHERE `key` = ?", key).
		Scan(&p.Key, &content, &p.IsHidden)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Panel not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get panel: " + err.Error()})
		}
		c.Abort()
		return
	}

	if content.Valid {
		p.Content = &content.String
	}

	c.JSON(http.StatusOK, p)
}

func UpdatePanelByName(c *gin.Context, db *sql.DB) {
	key := c.Param("key")

	var req UpdatePanelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	_, err := db.Exec(
		"UPDATE widget_panels SET content = COALESCE(?, content), is_hidden = COALESCE(?, is_hidden) WHERE `key` = ?",
		req.Content, req.IsHidden, key,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update panel: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Panel updated successfully"})
}

func PanelPreview(c *gin.Context, db *sql.DB) {
	var req UpdatePanelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	html := ""
	if req.Content != nil {
		html = Services.RenderPanelContent(*req.Content, db)
	}

	c.String(http.StatusOK, html)
}

func RenderWidget(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid widget ID"})
		c.Abort()
		return
	}

	html, err := Services.RenderWidget(id, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to render widget: " + err.Error()})
		c.Abort()
		return
	}

	if c.Query("innerOnly") == "true" {
		start := strings.Index(html, ">")
		end := strings.LastIndex(html, "</div>")
		if start != -1 && end != -1 && end > start {
			html = html[start+1 : end]
		}
	}

	c.String(http.StatusOK, html)
}

func GetPanelContentByName(c *gin.Context, db *sql.DB) {
	key := c.Param("key")

	var content sql.NullString
	err := db.QueryRow("SELECT content FROM widget_panels WHERE `key` = ?", key).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Panel not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get panel: " + err.Error()})
		}
		c.Abort()
		return
	}

	html := ""
	if content.Valid {
		html = Services.RenderPanelContent(content.String, db)
	}

	c.String(http.StatusOK, html)
}

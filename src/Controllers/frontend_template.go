package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const customTemplatesFile = "src/environments/custom_templates.json"

type frontendComponent struct {
	Name                string `json:"name"`
	TemplatePath        string `json:"template_path"`
	DefaultTemplatePath string `json:"default_template_path"`
	Description         string `json:"description"`
	Active              bool   `json:"active"`
}

type frontendComponentDef struct {
	Name                string
	TemplatePath        string
	DefaultTemplatePath string
	DescriptionKey      string
}

var frontendComponentDefs = []frontendComponentDef{
	{
		Name:                "src/app/components/header",
		TemplatePath:        "src/app/components/header/header.custom.component.html",
		DefaultTemplatePath: "src/app/components/header/header.component.html",
		DescriptionKey:      "frontend_component.src_app_components_header.description",
	},
	{
		Name:                "src/app/components/category",
		TemplatePath:        "src/app/components/category/category.custom.component.html",
		DefaultTemplatePath: "src/app/components/category/category.component.html",
		DescriptionKey:      "frontend_component.src_app_components_category.description",
	},
	{
		Name:                "src/app/components/footer-statistics",
		TemplatePath:        "src/app/components/footer-statistics/footer-statistics.custom.component.html",
		DefaultTemplatePath: "src/app/components/footer-statistics/footer-statistics.component.html",
		DescriptionKey:      "frontend_component.src_app_components_footer_statistics.description",
	},
	{
		Name:                "src/app/components/episode-header",
		TemplatePath:        "src/app/components/episode-header/episode-header.custom.component.html",
		DefaultTemplatePath: "src/app/components/episode-header/episode-header.component.html",
		DescriptionKey:      "frontend_component.src_app_components_episode_header.description",
	},
	{
		Name:                "src/app/components/character-sheet-header",
		TemplatePath:        "src/app/components/character-sheet-header/character-sheet-header.custom.component.html",
		DefaultTemplatePath: "src/app/components/character-sheet-header/character-sheet-header.component.html",
		DescriptionKey:      "frontend_component.src_app_components_character_sheet_header.description",
	},
	{
		Name:                "src/app/components/wanted-character-header",
		TemplatePath:        "src/app/components/wanted-character-header/wanted-character-header.custom.component.html",
		DefaultTemplatePath: "src/app/components/wanted-character-header/wanted-character-header.component.html",
		DescriptionKey:      "frontend_component.src_app_components_wanted_character_header.description",
	},
}

func getComponentFile(c *gin.Context, db *sql.DB, path string) {
	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}
	content, err := Services.GitHubGetFile(cfg, path)
	if err != nil {
		var ghErr *Services.GitHubError
		if errors.As(err, &ghErr) && ghErr.StatusCode == http.StatusNotFound {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "File not found in repository: " + path})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch file from GitHub: " + err.Error()})
		}
		c.Abort()
		return
	}
	c.String(http.StatusOK, content)
}

func findComponentDef(name string) (frontendComponentDef, bool) {
	for _, def := range frontendComponentDefs {
		if def.Name == name {
			return def, true
		}
	}
	return frontendComponentDef{}, false
}

type customTemplateEntry struct {
	Component       string `json:"component"`
	DefaultTemplate string `json:"default_template"`
	Template        string `json:"template"`
}

func readActiveCustomTemplates(cfg Services.GitHubConfig) map[string]bool {
	data, err := Services.GitHubGetFile(cfg, customTemplatesFile)
	if err != nil {
		return map[string]bool{}
	}
	var entries []customTemplateEntry
	if err := json.Unmarshal([]byte(data), &entries); err != nil {
		return map[string]bool{}
	}
	active := make(map[string]bool, len(entries))
	for _, e := range entries {
		active[e.Component] = true
	}
	return active
}

func GetFrontendComponents(c *gin.Context, db *sql.DB) {
	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	lang := Services.GetUserLanguage(userID, db)
	localizer := Services.NewLocalizer(lang)
	active := readActiveCustomTemplates(cfg)

	result := make([]frontendComponent, len(frontendComponentDefs))
	for i, def := range frontendComponentDefs {
		result[i] = frontendComponent{
			Name:                def.Name,
			TemplatePath:        def.TemplatePath,
			DefaultTemplatePath: def.DefaultTemplatePath,
			Description:         Services.T(localizer, def.DescriptionKey),
			Active:              active[def.Name],
		}
	}
	c.JSON(http.StatusOK, result)
}

// GetFrontendComponentTemplate returns the latest saved DB version of the component template,
// falling back to the default (non-custom) file from GitHub if no DB record exists.
func GetFrontendComponentTemplate(c *gin.Context, db *sql.DB) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	def, ok := findComponentDef(name)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + name})
		c.Abort()
		return
	}

	var templateText string
	err := db.QueryRow(
		"SELECT template_text FROM custom_templates WHERE template_file_name = ? ORDER BY id DESC LIMIT 1",
		def.TemplatePath,
	).Scan(&templateText)
	if err == nil {
		c.String(http.StatusOK, templateText)
		return
	}
	if err != sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "DB error: " + err.Error()})
		c.Abort()
		return
	}

	// No DB record — fetch the unmodified default from GitHub
	getComponentFile(c, db, def.DefaultTemplatePath)
}

func GetFrontendComponentVersion(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid template ID"})
		c.Abort()
		return
	}

	var v struct {
		Id               int    `json:"id"`
		Name             string `json:"name"`
		TemplateFileName string `json:"template_file_name"`
		TemplateText     string `json:"template_text"`
		IsActive         bool   `json:"is_active"`
	}
	err = db.QueryRow(
		"SELECT id, name, template_file_name, template_text, is_active FROM custom_templates WHERE id = ?", id,
	).Scan(&v.Id, &v.Name, &v.TemplateFileName, &v.TemplateText, &v.IsActive)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Template version not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "DB error: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, v)
}

func GetFrontendComponentDefaultTemplate(c *gin.Context, db *sql.DB) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	def, ok := findComponentDef(name)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + name})
		c.Abort()
		return
	}
	getComponentFile(c, db, def.DefaultTemplatePath)
}

type CustomTemplateVersion struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	TemplateFileName string `json:"template_file_name"`
	IsActive         bool   `json:"is_active"`
}

func GetFrontendComponentVersions(c *gin.Context, db *sql.DB) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	def, ok := findComponentDef(name)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + name})
		c.Abort()
		return
	}

	rows, err := db.Query(
		"SELECT id, name, template_file_name, is_active FROM custom_templates WHERE template_file_name = ? ORDER BY id DESC",
		def.TemplatePath,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query versions: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	versions := []CustomTemplateVersion{}
	for rows.Next() {
		var v CustomTemplateVersion
		if err := rows.Scan(&v.Id, &v.Name, &v.TemplateFileName, &v.IsActive); err != nil {
			continue
		}
		versions = append(versions, v)
	}
	c.JSON(http.StatusOK, versions)
}

type saveComponentTemplateRequest struct {
	ID            *int   `json:"id"`
	ComponentName string `json:"component_name" binding:"required"`
	Name          string `json:"name"           binding:"required"`
	Content       string `json:"content"        binding:"required"`
}

// UnpublishFrontendComponentTemplate removes a component from the active custom_templates.json
// on GitHub (so Angular falls back to the default) and clears is_active in the DB.
func UnpublishFrontendComponentTemplate(c *gin.Context, db *sql.DB) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	def, ok := findComponentDef(name)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + name})
		c.Abort()
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	// Read current active components from GitHub JSON and remove this one.
	active := readActiveCustomTemplates(cfg)
	delete(active, def.Name)

	var entries []customTemplateEntry
	for _, d := range frontendComponentDefs {
		if active[d.Name] {
			entries = append(entries, customTemplateEntry{
				Component:       d.Name,
				DefaultTemplate: d.DefaultTemplatePath,
				Template:        d.TemplatePath,
			})
		}
	}
	if entries == nil {
		entries = []customTemplateEntry{}
	}

	content, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to serialize custom templates"})
		c.Abort()
		return
	}

	files := []Services.GitHubFile{{Path: customTemplatesFile, Content: string(content)}}
	if err := Services.GitHubCommit(cfg, "Unpublish custom template: "+def.Name, files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	_, _ = db.Exec("UPDATE custom_templates SET is_active = 0 WHERE template_file_name = ?", def.TemplatePath)

	c.JSON(http.StatusOK, gin.H{"unpublished": def.Name})
}

// SaveFrontendComponentTemplate saves a new template version to the database.
func SaveFrontendComponentTemplate(c *gin.Context, db *sql.DB) {
	var req saveComponentTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	def, ok := findComponentDef(req.ComponentName)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + req.ComponentName})
		c.Abort()
		return
	}

	sanitized := Services.SanitizeTemplate(req.Content)

	var rowID int64
	if req.ID != nil {
		res, err := db.Exec(
			"UPDATE custom_templates SET name = ?, template_text = ? WHERE id = ? AND template_file_name = ?",
			req.Name, sanitized, *req.ID, def.TemplatePath,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update template: " + err.Error()})
			c.Abort()
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Template version not found"})
			c.Abort()
			return
		}
		rowID = int64(*req.ID)
	} else {
		result, err := db.Exec(
			"INSERT INTO custom_templates (name, template_file_name, template_text, is_active) VALUES (?, ?, ?, 0)",
			req.Name, def.TemplatePath, sanitized,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save template: " + err.Error()})
			c.Abort()
			return
		}
		rowID, _ = result.LastInsertId()
	}
	c.JSON(http.StatusOK, gin.H{"id": rowID, "template_file_name": def.TemplatePath})
}

// PublishFrontendComponentTemplate commits the specified DB version to GitHub as the active custom template.
func PublishFrontendComponentTemplate(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid template ID"})
		c.Abort()
		return
	}

	var templateFileName, templateText string
	if err := db.QueryRow(
		"SELECT template_file_name, template_text FROM custom_templates WHERE id = ?", id,
	).Scan(&templateFileName, &templateText); err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Template version not found"})
		c.Abort()
		return
	} else if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "DB error: " + err.Error()})
		c.Abort()
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	files := []Services.GitHubFile{{Path: templateFileName, Content: templateText}}
	if err := Services.GitHubCommit(cfg, "Publish custom template: "+templateFileName, files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	// Mark this version as active, deactivate all others for the same file.
	_, _ = db.Exec("UPDATE custom_templates SET is_active = 0 WHERE template_file_name = ?", templateFileName)
	_, _ = db.Exec("UPDATE custom_templates SET is_active = 1 WHERE id = ?", id)

	c.JSON(http.StatusOK, gin.H{"published": templateFileName})
}

type updateEnvRequest struct {
	ActiveComponents []string `json:"active_components"`
}

func UpdateFrontendEnv(c *gin.Context, db *sql.DB) {
	var req updateEnvRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	activeSet := make(map[string]bool, len(req.ActiveComponents))
	for _, name := range req.ActiveComponents {
		activeSet[name] = true
	}

	var entries []customTemplateEntry
	for _, def := range frontendComponentDefs {
		if activeSet[def.Name] {
			entries = append(entries, customTemplateEntry{
				Component:       def.Name,
				DefaultTemplate: def.DefaultTemplatePath,
				Template:        def.TemplatePath,
			})
		}
	}
	if entries == nil {
		entries = []customTemplateEntry{}
	}

	content, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to serialize custom templates"})
		c.Abort()
		return
	}

	files := []Services.GitHubFile{{Path: customTemplatesFile, Content: string(content)}}
	if err := Services.GitHubCommit(cfg, "Update custom templates configuration", files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"active_components": req.ActiveComponents})
}

type commitRequest struct {
	Message string                `json:"message" binding:"required"`
	Files   []Services.GitHubFile `json:"files"   binding:"required,min=1"`
}

func CommitFrontendTemplates(c *gin.Context, db *sql.DB) {
	var req commitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	if err := Services.GitHubCommit(cfg, req.Message, req.Files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"committed": len(req.Files)})
}

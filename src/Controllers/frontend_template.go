package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
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
		// Missing file = no active custom templates, not an error
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

func GetFrontendComponentTemplate(c *gin.Context, db *sql.DB) {
	name := strings.TrimPrefix(c.Param("name"), "/")
	def, ok := findComponentDef(name)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + name})
		c.Abort()
		return
	}
	getComponentFile(c, db, def.TemplatePath)
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

type updateComponentTemplateRequest struct {
	Name    string `json:"name"    binding:"required"`
	Content string `json:"content" binding:"required"`
}

func UpdateFrontendComponentTemplate(c *gin.Context, db *sql.DB) {
	var req updateComponentTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	def, ok := findComponentDef(req.Name)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Unknown component: " + req.Name})
		c.Abort()
		return
	}

	cfg, err := Services.GetGitHubConfig(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub config error: " + err.Error()})
		c.Abort()
		return
	}

	files := []Services.GitHubFile{{Path: def.TemplatePath, Content: Services.SanitizeTemplate(req.Content)}}
	if err := Services.GitHubCommit(cfg, "Update "+def.Name+" template", files); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "GitHub commit failed: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"committed": def.TemplatePath})
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

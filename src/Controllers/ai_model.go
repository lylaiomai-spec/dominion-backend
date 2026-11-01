package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/MCP"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type AIModelResponse struct {
	Id           int                          `json:"id"`
	Name         string                       `json:"name"`
	Size         Entities.AIModelSize         `json:"size"`
	ApiAddress   string                       `json:"api_address"`
	ApiKey       string                       `json:"api_key"`
	MachineName  string                       `json:"machine_name"`
	IsActive     bool                         `json:"is_active"`
	ProtocolType Entities.AIModelProtocolType `json:"protocol_type"`
	ModelType    Entities.AIModelType         `json:"model_type"`
}

type CreateAIModelRequest struct {
	Name         string                       `json:"name" binding:"required"`
	Size         Entities.AIModelSize         `json:"size" binding:"required"`
	ApiAddress   string                       `json:"api_address" binding:"required"`
	ApiKey       string                       `json:"api_key" binding:"required"`
	MachineName  string                       `json:"machine_name" binding:"required"`
	IsActive     bool                         `json:"is_active"`
	ProtocolType Entities.AIModelProtocolType `json:"protocol_type" binding:"required"`
	ModelType    Entities.AIModelType         `json:"model_type" binding:"required"`
}

type UpdateAIModelRequest struct {
	Name         *string                       `json:"name"`
	Size         *Entities.AIModelSize         `json:"size"`
	ApiAddress   *string                       `json:"api_address"`
	ApiKey       *string                       `json:"api_key"`
	MachineName  *string                       `json:"machine_name"`
	IsActive     *bool                         `json:"is_active"`
	ProtocolType *Entities.AIModelProtocolType `json:"protocol_type"`
	ModelType    *Entities.AIModelType         `json:"model_type"`
}

func scanAIModel(row *sql.Row) (AIModelResponse, error) {
	var m AIModelResponse
	err := row.Scan(&m.Id, &m.Name, &m.Size, &m.ApiAddress, &m.ApiKey, &m.MachineName, &m.IsActive, &m.ProtocolType, &m.ModelType)
	return m, err
}

func AdminListAIModels(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	rows, err := db.Query(
		`SELECT id, name, size, api_address, api_key, machine_name, is_active, protocol_type, model_type
		 FROM ai_models ORDER BY size ASC, id ASC`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get AI models: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	models := []AIModelResponse{}
	for rows.Next() {
		var m AIModelResponse
		if err := rows.Scan(&m.Id, &m.Name, &m.Size, &m.ApiAddress, &m.ApiKey, &m.MachineName, &m.IsActive, &m.ProtocolType, &m.ModelType); err != nil {
			continue
		}
		models = append(models, m)
	}
	c.JSON(http.StatusOK, models)
}

func AdminGetAIModel(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid model ID"})
		c.Abort()
		return
	}

	m, err := scanAIModel(db.QueryRow(
		`SELECT id, name, size, api_address, api_key, machine_name, is_active, protocol_type, model_type
		 FROM ai_models WHERE id = ?`, id))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "AI model not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, m)
}

func AdminCreateAIModel(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	var req CreateAIModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		`INSERT INTO ai_models (name, size, api_address, api_key, machine_name, is_active, protocol_type, model_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Size, req.ApiAddress, req.ApiKey, req.MachineName, req.IsActive, req.ProtocolType, req.ModelType,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create AI model: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()
	m, err := scanAIModel(db.QueryRow(
		`SELECT id, name, size, api_address, api_key, machine_name, is_active, protocol_type, model_type
		 FROM ai_models WHERE id = ?`, newID))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch created AI model"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, m)
}

func AdminUpdateAIModel(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid model ID"})
		c.Abort()
		return
	}

	var req UpdateAIModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.Name != nil {
		db.Exec("UPDATE ai_models SET name = ? WHERE id = ?", *req.Name, id)
	}
	if req.Size != nil {
		db.Exec("UPDATE ai_models SET size = ? WHERE id = ?", *req.Size, id)
	}
	if req.ApiAddress != nil {
		db.Exec("UPDATE ai_models SET api_address = ? WHERE id = ?", *req.ApiAddress, id)
	}
	if req.ApiKey != nil {
		db.Exec("UPDATE ai_models SET api_key = ? WHERE id = ?", *req.ApiKey, id)
	}
	if req.MachineName != nil {
		db.Exec("UPDATE ai_models SET machine_name = ? WHERE id = ?", *req.MachineName, id)
	}
	if req.IsActive != nil {
		db.Exec("UPDATE ai_models SET is_active = ? WHERE id = ?", *req.IsActive, id)
	}
	if req.ProtocolType != nil {
		db.Exec("UPDATE ai_models SET protocol_type = ? WHERE id = ?", *req.ProtocolType, id)
	}
	if req.ModelType != nil {
		db.Exec("UPDATE ai_models SET model_type = ? WHERE id = ?", *req.ModelType, id)
	}

	m, err := scanAIModel(db.QueryRow(
		`SELECT id, name, size, api_address, api_key, machine_name, is_active, protocol_type, model_type
		 FROM ai_models WHERE id = ?`, id))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "AI model not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, m)
}

// AdminSwitchAIPool switches the active AI model pool at runtime without a restart.
// Accepts {"source": "db"} or {"source": "openrouter", "api_key": "sk-..."}.
func AdminSwitchAIPool(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	var req struct {
		Source   string `json:"source" binding:"required"`
		APIKey   string `json:"api_key"`
		FreeOnly *bool  `json:"free_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	switch req.Source {
	case "db":
		db.Exec(`UPDATE global_settings SET setting_value = 'db' WHERE setting_name = 'model_pool_source'`)
		MCP.SwitchPool(MCP.NewAIModelPool(db))
	case "openrouter":
		if req.APIKey == "" {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "api_key is required for openrouter"})
			c.Abort()
			return
		}
		db.Exec(`UPDATE global_settings SET setting_value = 'openrouter' WHERE setting_name = 'model_pool_source'`)
		db.Exec(`UPDATE global_settings SET setting_value = ? WHERE setting_name = 'openrouter_api_key'`, req.APIKey)
		freeOnlyStr := "n"
		if req.FreeOnly != nil {
			if *req.FreeOnly {
				freeOnlyStr = "y"
			}
			db.Exec(`UPDATE global_settings SET setting_value = ? WHERE setting_name = 'openrouter_use_free_only'`, freeOnlyStr)
		} else {
			freeOnlyStr, _ = Services.GetGlobalSetting("openrouter_use_free_only", db)
		}
		MCP.SwitchPool(MCP.NewOpenRouterModelPool(req.APIKey, freeOnlyStr != "n"))
	case "groq":
		if req.APIKey == "" {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "api_key is required for groq"})
			c.Abort()
			return
		}
		db.Exec(`UPDATE global_settings SET setting_value = 'groq' WHERE setting_name = 'model_pool_source'`)
		db.Exec(`UPDATE global_settings SET setting_value = ? WHERE setting_name = 'groq_api_key'`, req.APIKey)
		MCP.SwitchPool(MCP.NewGroqModelPool(req.APIKey))
	default:
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "unknown source: " + req.Source})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"source": req.Source})
}

func AdminGetPoolStatus(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	source, _ := Services.GetGlobalSetting("model_pool_source", db)
	orApiKey, _ := Services.GetGlobalSetting("openrouter_api_key", db)
	freeOnly, _ := Services.GetGlobalSetting("openrouter_use_free_only", db)
	groqApiKey, _ := Services.GetGlobalSetting("groq_api_key", db)
	c.JSON(http.StatusOK, gin.H{
		"source":                   source,
		"openrouter_api_key":       orApiKey,
		"openrouter_use_free_only": freeOnly,
		"groq_api_key":             groqApiKey,
	})
}

func AdminListAvailablePoolModels(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	source, _ := Services.GetGlobalSetting("model_pool_source", db)
	switch source {
	case "openrouter":
		apiKey, _ := Services.GetGlobalSetting("openrouter_api_key", db)
		if apiKey == "" {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "OpenRouter API key is not configured"})
			c.Abort()
			return
		}
		models, err := MCP.FetchOpenRouterModels(apiKey)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadGateway, Message: "Failed to fetch models from OpenRouter: " + err.Error()})
			c.Abort()
			return
		}
		c.JSON(http.StatusOK, gin.H{"source": "openrouter", "models": models})
	case "groq":
		apiKey, _ := Services.GetGlobalSetting("groq_api_key", db)
		if apiKey == "" {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Groq API key is not configured"})
			c.Abort()
			return
		}
		models, err := MCP.FetchGroqModels(apiKey)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadGateway, Message: "Failed to fetch models from Groq: " + err.Error()})
			c.Abort()
			return
		}
		c.JSON(http.StatusOK, gin.H{"source": "groq", "models": models})
	default:
		c.JSON(http.StatusOK, gin.H{"source": "db", "models": []any{}})
	}
}

func AdminDeleteAIModel(c *gin.Context, db *sql.DB) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Superuser access required"})
		c.Abort()
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid model ID"})
		c.Abort()
		return
	}

	res, err := db.Exec("DELETE FROM ai_models WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete AI model: " + err.Error()})
		c.Abort()
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "AI model not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "AI model deleted"})
}

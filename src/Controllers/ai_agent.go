package Controllers

import (
	"cuento-backend/src/Middlewares"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type AiAgentResponse struct {
	ID                    int                              `json:"id"`
	Title                 string                           `json:"title"`
	ShortDescription      *string                          `json:"short_description"`
	TotalImplementations  int                              `json:"total_implementations"`
	ActiveImplementations int                              `json:"active_implementations"`
	Implementations       []AiAgentImplementationResponse  `json:"implementations,omitempty"`
}

type AiAgentImplementationResponse struct {
	ID       int             `json:"id"`
	AgentID  int             `json:"agent_id"`
	Title    string          `json:"title"`
	Config   json.RawMessage `json:"config"`
	IsActive bool            `json:"is_active"`
}

type CreateAiAgentImplementationRequest struct {
	AgentID  int             `json:"agent_id" binding:"required"`
	Title    string          `json:"title" binding:"required"`
	Config   json.RawMessage `json:"config"`
	IsActive *bool           `json:"is_active"`
}

type UpdateAiAgentImplementationRequest struct {
	Title    *string         `json:"title"`
	Config   json.RawMessage `json:"config"`
	IsActive *bool           `json:"is_active"`
}


// ── Agents (read-only) ────────────────────────────────────────────────────────

func AdminListAiAgents(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT a.id, a.title, a.short_description,
		       COUNT(i.id)                                       AS total_implementations,
		       SUM(CASE WHEN i.is_active = 1 THEN 1 ELSE 0 END) AS active_implementations
		FROM ai_agents a
		LEFT JOIN ai_agent_implementation i ON i.agent_id = a.id
		GROUP BY a.id
		ORDER BY a.id ASC`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get AI agents: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var agents []AiAgentResponse
	for rows.Next() {
		var a AiAgentResponse
		if err := rows.Scan(&a.ID, &a.Title, &a.ShortDescription, &a.TotalImplementations, &a.ActiveImplementations); err != nil {
			continue
		}
		agents = append(agents, a)
	}
	if agents == nil {
		agents = []AiAgentResponse{}
	}
	c.JSON(http.StatusOK, agents)
}

func AdminGetAiAgent(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid AI agent ID"})
		c.Abort()
		return
	}

	var a AiAgentResponse
	err = db.QueryRow(`
		SELECT a.id, a.title, a.short_description,
		       COUNT(i.id)                                       AS total_implementations,
		       SUM(CASE WHEN i.is_active = 1 THEN 1 ELSE 0 END) AS active_implementations
		FROM ai_agents a
		LEFT JOIN ai_agent_implementation i ON i.agent_id = a.id
		WHERE a.id = ?
		GROUP BY a.id`, id).
		Scan(&a.ID, &a.Title, &a.ShortDescription, &a.TotalImplementations, &a.ActiveImplementations)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "AI agent not found"})
		c.Abort()
		return
	}

	rows, err := db.Query("SELECT id, agent_id, title, config, is_active FROM ai_agent_implementation WHERE agent_id = ? ORDER BY id ASC", id)
	if err == nil {
		defer rows.Close()
		a.Implementations = []AiAgentImplementationResponse{}
		for rows.Next() {
			var impl AiAgentImplementationResponse
			var config sql.NullString
			if err := rows.Scan(&impl.ID, &impl.AgentID, &impl.Title, &config, &impl.IsActive); err == nil {
				if config.Valid {
					impl.Config = json.RawMessage(config.String)
				}
				a.Implementations = append(a.Implementations, impl)
			}
		}
	}

	c.JSON(http.StatusOK, a)
}

// ── Implementations (full CRUD + call) ───────────────────────────────────────

func scanImplementation(row *sql.Row) (AiAgentImplementationResponse, error) {
	var impl AiAgentImplementationResponse
	var config sql.NullString
	err := row.Scan(&impl.ID, &impl.AgentID, &impl.Title, &config, &impl.IsActive)
	if err != nil {
		return impl, err
	}
	if config.Valid {
		impl.Config = json.RawMessage(config.String)
	}
	return impl, nil
}


func AdminListAiAgentImplementations(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, agent_id, title, config, is_active FROM ai_agent_implementation ORDER BY id ASC")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get implementations: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var impls []AiAgentImplementationResponse
	for rows.Next() {
		var impl AiAgentImplementationResponse
		var config sql.NullString
		if err := rows.Scan(&impl.ID, &impl.AgentID, &impl.Title, &config, &impl.IsActive); err != nil {
			continue
		}
		if config.Valid {
			impl.Config = json.RawMessage(config.String)
		}
		impls = append(impls, impl)
	}
	if impls == nil {
		impls = []AiAgentImplementationResponse{}
	}
	c.JSON(http.StatusOK, impls)
}

func AdminGetAiAgentImplementation(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid implementation ID"})
		c.Abort()
		return
	}

	impl, err := scanImplementation(db.QueryRow("SELECT id, agent_id, title, config, is_active FROM ai_agent_implementation WHERE id = ?", id))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Implementation not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, impl)
}

func AdminCreateAiAgentImplementation(c *gin.Context, db *sql.DB) {
	var req CreateAiAgentImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var configStr *string
	if req.Config != nil {
		s := string(req.Config)
		configStr = &s
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	res, err := db.Exec(
		"INSERT INTO ai_agent_implementation (agent_id, title, config, is_active) VALUES (?, ?, ?, ?)",
		req.AgentID, req.Title, configStr, isActive,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create implementation: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()

	impl, err := scanImplementation(db.QueryRow("SELECT id, agent_id, title, config, is_active FROM ai_agent_implementation WHERE id = ?", newID))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch created implementation"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, impl)
}

func AdminUpdateAiAgentImplementation(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid implementation ID"})
		c.Abort()
		return
	}

	var req UpdateAiAgentImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.Title != nil {
		db.Exec("UPDATE ai_agent_implementation SET title = ? WHERE id = ?", *req.Title, id)
	}
	if req.Config != nil {
		db.Exec("UPDATE ai_agent_implementation SET config = ? WHERE id = ?", string(req.Config), id)
	}
	if req.IsActive != nil {
		db.Exec("UPDATE ai_agent_implementation SET is_active = ? WHERE id = ?", *req.IsActive, id)
	}

	impl, err := scanImplementation(db.QueryRow("SELECT id, agent_id, title, config, is_active FROM ai_agent_implementation WHERE id = ?", id))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Implementation not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, impl)
}

func AdminDeleteAiAgentImplementation(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid implementation ID"})
		c.Abort()
		return
	}

	res, err := db.Exec("DELETE FROM ai_agent_implementation WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete implementation: " + err.Error()})
		c.Abort()
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Implementation not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Implementation deleted"})
}

func dispatchAgentHandler(handler string, implementationID int, db *sql.DB) (interface{}, error) {
	switch handler {
	case "GameDigest":
		return GameDigest(implementationID, db)
	default:
		return nil, fmt.Errorf("unknown agent handler: %s", handler)
	}
}

func AdminCallAiAgentImplementation(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid implementation ID"})
		c.Abort()
		return
	}

	var handler string
	var isActive bool
	err = db.QueryRow(`
		SELECT a.handler, i.is_active
		FROM ai_agent_implementation i
		JOIN ai_agents a ON a.id = i.agent_id
		WHERE i.id = ?`, id).
		Scan(&handler, &isActive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Implementation not found"})
		c.Abort()
		return
	}

	if !isActive {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Implementation is not active"})
		c.Abort()
		return
	}

	result, err := dispatchAgentHandler(handler, id, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, result)
}

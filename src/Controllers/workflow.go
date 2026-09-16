package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const workflowSelectQuery = "SELECT id, event_name, subforum_ids, handler_function, config, event_config FROM workflows"

func scanWorkflow(row *sql.Row) (*Entities.Workflow, error) {
	var w Entities.Workflow
	var config []byte
	var eventConfig []byte
	if err := row.Scan(&w.Id, &w.EventName, &w.SubforumIds, &w.HandlerFunction, &config, &eventConfig); err != nil {
		return nil, err
	}
	if len(config) > 0 {
		w.Config = json.RawMessage(config)
	}
	if len(eventConfig) > 0 {
		raw := json.RawMessage(eventConfig)
		w.EventConfig = &raw
	}
	return &w, nil
}

func AdminListWorkflows(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(workflowSelectQuery)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to list workflows: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	list := []Entities.Workflow{}
	for rows.Next() {
		var w Entities.Workflow
		var config []byte
		var eventConfig []byte
		if err := rows.Scan(&w.Id, &w.EventName, &w.SubforumIds, &w.HandlerFunction, &config, &eventConfig); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan workflow: " + err.Error()})
			c.Abort()
			return
		}
		if len(config) > 0 {
			w.Config = json.RawMessage(config)
		}
		if len(eventConfig) > 0 {
			raw := json.RawMessage(eventConfig)
			w.EventConfig = &raw
		}
		list = append(list, w)
	}

	c.JSON(http.StatusOK, list)
}

func AdminCreateWorkflow(c *gin.Context, db *sql.DB) {
	var req struct {
		EventName       string           `json:"event_name" binding:"required"`
		SubforumIds     string           `json:"subforum_ids" binding:"required"`
		HandlerFunction string           `json:"handler_function" binding:"required"`
		Config          json.RawMessage  `json:"config" binding:"required"`
		EventConfig     *json.RawMessage `json:"event_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var eventConfigArg interface{}
	if req.EventConfig != nil {
		eventConfigArg = []byte(*req.EventConfig)
	}

	res, err := db.Exec(
		"INSERT INTO workflows (event_name, subforum_ids, handler_function, config, event_config) VALUES (?, ?, ?, ?, ?)",
		req.EventName, req.SubforumIds, req.HandlerFunction, []byte(req.Config), eventConfigArg,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create workflow: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	row := db.QueryRow(workflowSelectQuery+" WHERE id = ?", id)
	workflow, err := scanWorkflow(row)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch workflow: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, workflow)
}

func AdminUpdateWorkflow(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req struct {
		EventName       *string          `json:"event_name"`
		SubforumIds     *string          `json:"subforum_ids"`
		HandlerFunction *string          `json:"handler_function"`
		Config          json.RawMessage  `json:"config"`
		EventConfig     *json.RawMessage `json:"event_config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM workflows WHERE id = ?)", id).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Workflow not found"})
		c.Abort()
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.EventName != nil {
		setClauses = append(setClauses, "event_name = ?")
		args = append(args, *req.EventName)
	}
	if req.SubforumIds != nil {
		setClauses = append(setClauses, "subforum_ids = ?")
		args = append(args, *req.SubforumIds)
	}
	if req.HandlerFunction != nil {
		setClauses = append(setClauses, "handler_function = ?")
		args = append(args, *req.HandlerFunction)
	}
	if req.Config != nil {
		setClauses = append(setClauses, "config = ?")
		args = append(args, []byte(req.Config))
	}
	if req.EventConfig != nil {
		setClauses = append(setClauses, "event_config = ?")
		args = append(args, []byte(*req.EventConfig))
	}

	if len(setClauses) > 0 {
		args = append(args, id)
		query := "UPDATE workflows SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
		if _, err := db.Exec(query, args...); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update workflow: " + err.Error()})
			c.Abort()
			return
		}
	}

	row := db.QueryRow(workflowSelectQuery+" WHERE id = ?", id)
	workflow, err := scanWorkflow(row)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch workflow: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, workflow)
}

func AdminDeleteWorkflow(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	res, err := db.Exec("DELETE FROM workflows WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete workflow: " + err.Error()})
		c.Abort()
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Workflow not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workflow deleted"})
}

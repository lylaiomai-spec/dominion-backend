package Controllers

import (
	"cuento-backend/config"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
)

func BackupDB(c *gin.Context) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}
	cfg := config.LoadDBConfig()

	filename := fmt.Sprintf("backup_%s.sql", time.Now().UTC().Format("2006-01-02_15-04-05"))

	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "application/sql")

	cmd := exec.Command("mysqldump",
		"-h", cfg.Host,
		"-P", cfg.Port,
		"-u", cfg.User,
		"-p"+cfg.Password,
		"--single-transaction",
		"--routines",
		"--triggers",
		cfg.Name,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start backup: " + err.Error()})
		c.Abort()
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start backup: " + err.Error()})
		c.Abort()
		return
	}

	if err := cmd.Start(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start mysqldump: " + err.Error()})
		c.Abort()
		return
	}

	c.Status(http.StatusOK)
	io.Copy(c.Writer, stdout)

	if err := cmd.Wait(); err != nil {
		errMsg, _ := io.ReadAll(stderr)
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "mysqldump failed: " + string(errMsg)})
		c.Abort()
		return
	}
}

func RestoreDB(c *gin.Context) {
	if !Services.IsSuperuser(c) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Forbidden"})
		c.Abort()
		return
	}
	cfg := config.LoadDBConfig()

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "SQL file is required"})
		c.Abort()
		return
	}
	defer file.Close()

	cmd := exec.Command("mysql",
		"-h", cfg.Host,
		"-P", cfg.Port,
		"-u", cfg.User,
		"-p"+cfg.Password,
		cfg.Name,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start restore: " + err.Error()})
		c.Abort()
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start restore: " + err.Error()})
		c.Abort()
		return
	}

	if err := cmd.Start(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start mysql: " + err.Error()})
		c.Abort()
		return
	}

	if _, err := io.Copy(stdin, file); err != nil {
		stdin.Close()
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to pipe SQL file: " + err.Error()})
		c.Abort()
		return
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		errMsg, _ := io.ReadAll(stderr)
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Restore failed: " + string(errMsg)})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Database restored successfully"})
}

package test

import (
	"bytes"
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
)

func TestRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	t.Run("Successful Registration", func(t *testing.T) {
		user := Entities.User{
			Username: "testuser",
			Password: "hashedpassword",
		}
		body, _ := json.Marshal(user)

		// Use sqlmock.AnyArg() for the password because it will be hashed
		mock.ExpectExec("INSERT INTO users").
			WithArgs(user.Username, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery("SELECT id FROM roles WHERE name = ?").
			WithArgs("user").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

		mock.ExpectExec("INSERT INTO user_role").
			WithArgs(1, 1).
			WillReturnResult(sqlmock.NewResult(1, 1))

		r := gin.New()
		r.Use(Middlewares.ErrorMiddleware())
		r.POST("/register", func(c *gin.Context) {
			Controllers.Register(c, db)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(body))
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d. Body: %s", w.Code, w.Body.String())
		}

		var response Entities.User
		json.Unmarshal(w.Body.Bytes(), &response)
		if response.Username != user.Username {
			t.Errorf("Expected username %s, got %s", user.Username, response.Username)
		}
	})
}

func TestLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	t.Run("Successful Login", func(t *testing.T) {
		creds := Controllers.Credentials{
			Username: "testuser",
			Password: "hashedpassword",
		}
		body, _ := json.Marshal(creds)

		dummyUser := Entities.User{}
		dummyUser.HashPassword("hashedpassword")

		mock.ExpectQuery("SELECT id, username, avatar, password, interface_language, interface_timezone, user_status FROM users WHERE username = ?").
			WithArgs(creds.Username).
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "avatar", "password", "interface_language", "interface_timezone", "user_status"}).
				AddRow(1, "testuser", nil, dummyUser.Password, nil, nil, 0))

		mock.ExpectQuery("SELECT r.id, r.name FROM roles r").
			WithArgs(1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "user"))

		r := gin.New()
		r.Use(Middlewares.ErrorMiddleware())
		r.POST("/login", func(c *gin.Context) {
			Controllers.Login(c, db)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(body))
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Blocked User Login", func(t *testing.T) {
		creds := Controllers.Credentials{
			Username: "blockeduser",
			Password: "hashedpassword",
		}
		body, _ := json.Marshal(creds)

		mock.ExpectQuery("SELECT id, username, avatar, password, interface_language, interface_timezone, user_status FROM users WHERE username = ?").
			WithArgs(creds.Username).
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "avatar", "password", "interface_language", "interface_timezone", "user_status"}).
				AddRow(2, "blockeduser", nil, "hashedpassword", nil, nil, 1)) // 1 = ArchivedUser

		r := gin.New()
		r.Use(Middlewares.ErrorMiddleware())
		r.POST("/login", func(c *gin.Context) {
			Controllers.Login(c, db)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/login", bytes.NewBuffer(body))
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d. Body: %s", w.Code, w.Body.String())
		}
	})
}

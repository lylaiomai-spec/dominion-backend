package test

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRegister_CreatesUserWithUserRole(t *testing.T) {
	f := newFixture(t)
	w := f.do(t, "POST", "/register", 0, Entities.User{Username: "newcomer", Password: "secret"})
	expectStatus(t, w, http.StatusCreated)

	var userID int
	if err := f.db.QueryRow("SELECT id FROM users WHERE username = 'newcomer'").Scan(&userID); err != nil {
		t.Fatalf("reading new user: %v", err)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM user_role WHERE user_id = ? AND role_id = ?", userID, roleUser); got != 1 {
		t.Errorf("expected the new user to get the user role, got %d rows", got)
	}
}

func TestRegister_Rejected(t *testing.T) {
	t.Run("duplicate username", func(t *testing.T) {
		f := newFixture(t)
		w := f.do(t, "POST", "/register", 0, Entities.User{Username: "member", Password: "secret"})
		if w.Code == http.StatusCreated {
			t.Errorf("expected duplicate registration to fail, got 201")
		}
		if got := f.count(t, "SELECT COUNT(*) FROM users WHERE username = 'member'"); got != 1 {
			t.Errorf("expected one user named member, got %d", got)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		f := newFixture(t)
		expectStatus(t, f.do(t, "POST", "/register", 0, "not an object"), http.StatusBadRequest)
	})
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		archived bool
		want     int
	}{
		{"valid credentials", "member", fixturePassword, false, http.StatusOK},
		{"wrong password", "member", "wrong", false, http.StatusUnauthorized},
		{"unknown user", "nobody", fixturePassword, false, http.StatusUnauthorized},
		{"archived user", "member", fixturePassword, true, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.archived {
				f.exec(t, "UPDATE users SET user_status = ? WHERE id = ?", Entities.ArchivedUser, f.member)
			}

			w := f.do(t, "POST", "/login", 0, Controllers.Credentials{Username: tt.username, Password: tt.password})
			expectStatus(t, w, tt.want)

			if tt.want == http.StatusOK {
				var response struct {
					AccessToken string        `json:"access_token"`
					User        Entities.User `json:"user"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("decoding response: %v", err)
				}
				if response.AccessToken == "" {
					t.Error("expected an access token")
				}
				if response.User.Id != f.member {
					t.Errorf("expected user %d, got %d", f.member, response.User.Id)
				}
			}
		})
	}
}

package test

import (
	"cuento-backend/src/Entities"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestGetUnreadNotifications_Access(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		f := newFixture(t)
		expectStatus(t, f.do(t, "GET", "/notifications/unread", 0, nil), http.StatusUnauthorized)
	})

	// The guest role holds /notifications/unread, so only a user without any role is refused.
	t.Run("user without any role", func(t *testing.T) {
		f := newFixture(t)
		expectStatus(t, f.do(t, "GET", "/notifications/unread", f.noRole, nil), http.StatusForbidden)
	})
}

func TestGetUnreadNotifications_OnlyOwnUnread(t *testing.T) {
	f := newFixture(t)
	own := f.addNotification(t, f.member)
	read := f.addNotification(t, f.member)
	f.exec(t, "UPDATE notifications SET is_read = TRUE WHERE id = ?", read)
	f.addNotification(t, f.admin)

	w := f.do(t, "GET", "/notifications/unread", f.member, nil)
	expectStatus(t, w, http.StatusOK)

	var response map[string][]Entities.MentionNotification
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	mentions := response["mention"]
	if len(mentions) != 1 {
		t.Fatalf("expected 1 unread mention, got %d: %s", len(mentions), w.Body.String())
	}
	if mentions[0].Id != own {
		t.Errorf("expected notification %d, got %d", own, mentions[0].Id)
	}
	if mentions[0].Data.UserName != "author" {
		t.Errorf("expected mention author 'author', got %q", mentions[0].Data.UserName)
	}
	if mentions[0].DateCreatedLocalized == "" {
		t.Error("expected a localized creation date")
	}
}

func TestDismissNotification(t *testing.T) {
	tests := []struct {
		name     string
		owner    func(f *fixture) int
		caller   func(f *fixture) int
		want     int
		wantRead bool
	}{
		{"unauthenticated", func(f *fixture) int { return f.member }, func(f *fixture) int { return 0 }, http.StatusUnauthorized, false},
		{"guest role cannot dismiss", func(f *fixture) int { return f.guestUser }, func(f *fixture) int { return f.guestUser }, http.StatusForbidden, false},
		{"owner dismisses", func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusOK, true},
		{"another user's notification stays unread", func(f *fixture) int { return f.admin }, func(f *fixture) int { return f.member }, http.StatusOK, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			id := f.addNotification(t, tt.owner(f))

			w := f.do(t, "POST", fmt.Sprintf("/notifications/dismiss/%d", id), tt.caller(f), nil)
			expectStatus(t, w, tt.want)

			var isRead bool
			if err := f.db.QueryRow("SELECT is_read FROM notifications WHERE id = ?", id).Scan(&isRead); err != nil {
				t.Fatalf("reading notification: %v", err)
			}
			if isRead != tt.wantRead {
				t.Errorf("expected is_read=%v, got %v", tt.wantRead, isRead)
			}
		})
	}
}

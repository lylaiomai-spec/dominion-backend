package test

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"net/http"
	"testing"
)

func TestCreateWantedCharacter_Access(t *testing.T) {
	tests := []struct {
		name     string
		subforum func(f *fixture) int
		listed   bool
		creator  func(f *fixture) int
		want     int
	}{
		{"unauthenticated", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return 0 }, http.StatusUnauthorized},
		{"subforum permission but guest role only", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.guestUser }, http.StatusForbidden},
		{"member in subforum with create permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, http.StatusCreated},
		{"member in subforum without create permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"member not listed on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"member listed on private subforum", func(f *fixture) int { return f.subPrivate }, true, func(f *fixture) int { return f.member }, http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			sub := tt.subforum(f)
			creator := tt.creator(f)
			if tt.listed {
				f.exec(t, "INSERT INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)", sub, creator)
			}

			w := f.do(t, "POST", "/wanted-character/create", creator, Controllers.CreateWantedCharacterRequest{SubforumID: sub, Name: "Baron"})
			expectStatus(t, w, tt.want)

			wantRows := 0
			if tt.want == http.StatusCreated {
				wantRows = 1
			}
			got := f.count(t, "SELECT COUNT(*) FROM topics WHERE name = 'Baron' AND subforum_id = ? AND type = ?", sub, Entities.WantedCharacterTopic)
			if got != wantRows {
				t.Errorf("expected %d stored wanted-character topics, got %d", wantRows, got)
			}
		})
	}
}

func TestCreateWantedCharacter_InvalidBody(t *testing.T) {
	f := newFixture(t)
	w := f.do(t, "POST", "/wanted-character/create", f.member, map[string]interface{}{"subforum_id": f.subA})
	expectStatus(t, w, http.StatusBadRequest)
}

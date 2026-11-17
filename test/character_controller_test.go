package test

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestGetCharacter_Access(t *testing.T) {
	tests := []struct {
		name     string
		subforum func(f *fixture) int
		listed   bool
		viewer   func(f *fixture) int
		want     int
	}{
		{"anonymous reads public subforum", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return 0 }, http.StatusOK},
		{"member reads subforum with read permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, http.StatusOK},
		{"member reads subforum without read permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.member }, http.StatusNotFound},
		{"member not listed on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return f.member }, http.StatusNotFound},
		{"member listed on private subforum", func(f *fixture) int { return f.subPrivate }, true, func(f *fixture) int { return f.member }, http.StatusOK},
		{"anonymous on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return 0 }, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			sub := tt.subforum(f)
			characterID, _ := f.addCharacter(t, sub, f.admin, "Erzulie")
			viewer := tt.viewer(f)
			if tt.listed {
				f.exec(t, "INSERT INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)", sub, viewer)
			}

			w := f.do(t, "GET", fmt.Sprintf("/character/get/%d", characterID), viewer, nil)
			expectStatus(t, w, tt.want)
			if tt.want == http.StatusOK {
				var character Entities.Character
				if err := json.Unmarshal(w.Body.Bytes(), &character); err != nil {
					t.Fatalf("decoding character: %v", err)
				}
				if character.Name != "Erzulie" {
					t.Errorf("expected character name Erzulie, got %q", character.Name)
				}
			}
		})
	}
}

func TestGetCharacter_CanEdit(t *testing.T) {
	tests := []struct {
		name   string
		viewer func(f *fixture) int
		want   bool
	}{
		{"owner with edit-own permission", func(f *fixture) int { return f.member }, true},
		{"other user without edit-others permission", func(f *fixture) int { return f.admin }, false},
		{"anonymous", func(f *fixture) int { return 0 }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			characterID, _ := f.addCharacter(t, f.subA, f.member, "Erzulie")

			w := f.do(t, "GET", fmt.Sprintf("/character/get/%d", characterID), tt.viewer(f), nil)
			expectStatus(t, w, http.StatusOK)
			var character Entities.Character
			if err := json.Unmarshal(w.Body.Bytes(), &character); err != nil {
				t.Fatalf("decoding character: %v", err)
			}
			if character.CanEdit == nil || *character.CanEdit != tt.want {
				t.Errorf("expected can_edit=%v, got %v", tt.want, character.CanEdit)
			}
		})
	}
}

func TestGetCharacter_NotFound(t *testing.T) {
	f := newFixture(t)
	expectStatus(t, f.do(t, "GET", "/character/get/999", f.member, nil), http.StatusNotFound)
}

func TestCreateCharacter_Access(t *testing.T) {
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

			w := f.do(t, "POST", "/character/create", creator, Controllers.CreateCharacterRequest{SubforumID: sub, Name: "Erzulie"})
			expectStatus(t, w, tt.want)

			wantRows := 0
			if tt.want == http.StatusCreated {
				wantRows = 1
			}
			got := f.count(t, `SELECT COUNT(*) FROM character_base cb JOIN topics t ON t.id = cb.topic_id
				WHERE cb.name = 'Erzulie' AND cb.user_id = ? AND t.subforum_id = ? AND t.type = ?`, creator, sub, Entities.CharacterSheetTopic)
			if got != wantRows {
				t.Errorf("expected %d stored characters, got %d", wantRows, got)
			}
		})
	}
}

func TestCreateCharacter_InvalidBody(t *testing.T) {
	f := newFixture(t)
	w := f.do(t, "POST", "/character/create", f.member, map[string]interface{}{"subforum_id": f.subA})
	expectStatus(t, w, http.StatusBadRequest)
}

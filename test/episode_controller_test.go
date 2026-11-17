package test

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// GetEpisode is not registered in main.go, so it is called directly here.
func TestGetEpisode(t *testing.T) {
	f := newFixture(t)
	characterID, _ := f.addCharacter(t, f.subA, f.member, "Erzulie")
	topicID := f.addTopic(t, f.subA, f.member, Entities.EpisodeTopic, "Crossroads")
	episodeID := f.insert(t, "INSERT INTO episode_base (topic_id, name) VALUES (?, 'Crossroads')", topicID)
	f.exec(t, "INSERT INTO episode_character (episode_id, character_id) VALUES (?, ?)", episodeID, characterID)

	r := gin.New()
	r.GET("/episode/:id", func(c *gin.Context) { Controllers.GetEpisode(c, f.db) })

	t.Run("existing episode", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/episode/%d", episodeID), nil))
		expectStatus(t, w, http.StatusOK)

		var episode Entities.Episode
		if err := json.Unmarshal(w.Body.Bytes(), &episode); err != nil {
			t.Fatalf("decoding episode: %v", err)
		}
		if episode.Name != "Crossroads" {
			t.Errorf("expected episode name Crossroads, got %q", episode.Name)
		}
	})
}

func TestCreateEpisode_Access(t *testing.T) {
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

			w := f.do(t, "POST", "/episode/create", creator, Controllers.CreateEpisodeRequest{SubforumID: sub, Name: "Crossroads"})
			expectStatus(t, w, tt.want)

			wantRows := 0
			if tt.want == http.StatusCreated {
				wantRows = 1
			}
			got := f.count(t, `SELECT COUNT(*) FROM episode_base eb JOIN topics t ON t.id = eb.topic_id
				WHERE eb.name = 'Crossroads' AND t.subforum_id = ? AND t.type = ?`, sub, Entities.EpisodeTopic)
			if got != wantRows {
				t.Errorf("expected %d stored episodes, got %d", wantRows, got)
			}
		})
	}
}

func TestCreateEpisode_LinksCharacters(t *testing.T) {
	f := newFixture(t)
	first, _ := f.addCharacter(t, f.subA, f.member, "Erzulie")
	second, _ := f.addCharacter(t, f.subA, f.admin, "Ogoun")

	w := f.do(t, "POST", "/episode/create", f.member, Controllers.CreateEpisodeRequest{
		SubforumID:   f.subA,
		Name:         "Crossroads",
		CharacterIDs: []int{first, second},
	})
	expectStatus(t, w, http.StatusCreated)

	var response struct {
		EpisodeID int `json:"episode_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM episode_character WHERE episode_id = ? AND character_id IN (?, ?)", response.EpisodeID, first, second); got != 2 {
		t.Errorf("expected both characters linked to the episode, got %d", got)
	}
}

func TestCreateEpisode_InvalidBody(t *testing.T) {
	f := newFixture(t)
	w := f.do(t, "POST", "/episode/create", f.member, map[string]interface{}{"subforum_id": f.subA})
	expectStatus(t, w, http.StatusBadRequest)
}

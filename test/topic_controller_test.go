package test

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestGetTopic_Access(t *testing.T) {
	tests := []struct {
		name     string
		subforum func(f *fixture) int
		listed   bool // add the viewer to private_subforum_users
		viewer   func(f *fixture) int
		want     int
	}{
		{"anonymous reads public subforum", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return 0 }, http.StatusOK},
		{"member reads subforum with read permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, http.StatusOK},
		{"member reads subforum without read permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.member }, http.StatusNotFound},
		{"admin reads subforum B", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.admin }, http.StatusOK},
		{"member not listed on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return f.member }, http.StatusNotFound},
		{"member listed on private subforum", func(f *fixture) int { return f.subPrivate }, true, func(f *fixture) int { return f.member }, http.StatusOK},
		{"anonymous on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return 0 }, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			sub := tt.subforum(f)
			topicID := f.addTopic(t, sub, f.admin, Entities.GeneralTopic, "Crossroads")
			viewer := tt.viewer(f)
			if tt.listed {
				f.exec(t, "INSERT INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)", sub, viewer)
			}

			w := f.do(t, "GET", fmt.Sprintf("/topic/get/%d", topicID), viewer, nil)
			expectStatus(t, w, tt.want)
			if tt.want == http.StatusOK {
				var topic Entities.Topic
				if err := json.Unmarshal(w.Body.Bytes(), &topic); err != nil {
					t.Fatalf("decoding topic: %v", err)
				}
				if topic.Name != "Crossroads" {
					t.Errorf("expected topic name Crossroads, got %q", topic.Name)
				}
			}
		})
	}
}

func TestGetTopic_NotFound(t *testing.T) {
	f := newFixture(t)
	expectStatus(t, f.do(t, "GET", "/topic/get/999", f.member, nil), http.StatusNotFound)
}

func TestCreatePost_Access(t *testing.T) {
	tests := []struct {
		name     string
		subforum func(f *fixture) int
		listed   bool
		author   func(f *fixture) int
		want     int
	}{
		{"member posts in subforum with post permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, http.StatusCreated},
		{"member posts in subforum without post permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"anonymous posts in subforum without guest permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return 0 }, http.StatusForbidden},
		{"member not listed on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"member listed on private subforum", func(f *fixture) int { return f.subPrivate }, true, func(f *fixture) int { return f.member }, http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			sub := tt.subforum(f)
			topicID := f.addTopic(t, sub, f.admin, Entities.GeneralTopic, "Crossroads")
			author := tt.author(f)
			if tt.listed {
				f.exec(t, "INSERT INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)", sub, author)
			}

			w := f.do(t, "POST", "/post/create", author, Controllers.CreatePostRequest{TopicID: topicID, Content: "Hello"})
			expectStatus(t, w, tt.want)

			wantPosts := 0
			if tt.want == http.StatusCreated {
				wantPosts = 1
			}
			if got := f.count(t, "SELECT COUNT(*) FROM posts WHERE topic_id = ? AND content = 'Hello'", topicID); got != wantPosts {
				t.Errorf("expected %d stored posts, got %d", wantPosts, got)
			}
		})
	}
}

func TestCreatePost_TopicState(t *testing.T) {
	t.Run("missing topic", func(t *testing.T) {
		f := newFixture(t)
		w := f.do(t, "POST", "/post/create", f.member, Controllers.CreatePostRequest{TopicID: 999, Content: "Hello"})
		expectStatus(t, w, http.StatusNotFound)
	})

	t.Run("full topic", func(t *testing.T) {
		f := newFixture(t)
		topicID := f.addTopic(t, f.subA, f.admin, Entities.GeneralTopic, "Crossroads")
		f.exec(t, "UPDATE topics SET status = ? WHERE id = ?", Entities.FullTopic, topicID)
		w := f.do(t, "POST", "/post/create", f.member, Controllers.CreatePostRequest{TopicID: topicID, Content: "Hello"})
		expectStatus(t, w, http.StatusForbidden)
	})

	t.Run("invalid body", func(t *testing.T) {
		f := newFixture(t)
		w := f.do(t, "POST", "/post/create", f.member, "not an object")
		expectStatus(t, w, http.StatusBadRequest)
	})
}

func TestUpdatePost_Access(t *testing.T) {
	tests := []struct {
		name     string
		subforum func(f *fixture) int
		listed   bool
		author   func(f *fixture) int // post author
		editor   func(f *fixture) int
		want     int
	}{
		{"unauthenticated", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return 0 }, http.StatusUnauthorized},
		{"subforum permission but guest role only", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.guestUser }, func(f *fixture) int { return f.guestUser }, http.StatusForbidden},
		{"author with edit-own permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusOK},
		{"author without subforum permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"someone else's post without edit-others permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.admin }, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"author not listed on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"author listed on private subforum", func(f *fixture) int { return f.subPrivate }, true, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			sub := tt.subforum(f)
			author := tt.author(f)
			topicID := f.addTopic(t, sub, author, Entities.GeneralTopic, "Crossroads")
			postID := f.addPost(t, topicID, author, "Original")
			editor := tt.editor(f)
			if tt.listed {
				f.exec(t, "INSERT INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)", sub, editor)
			}

			w := f.do(t, "POST", fmt.Sprintf("/post/update/%d", postID), editor, Controllers.UpdatePostRequest{Content: "Edited"})
			expectStatus(t, w, tt.want)

			wantContent := "Original"
			if tt.want == http.StatusOK {
				wantContent = "Edited"
			}
			var content string
			if err := f.db.QueryRow("SELECT content FROM posts WHERE id = ?", postID).Scan(&content); err != nil {
				t.Fatalf("reading post: %v", err)
			}
			if content != wantContent {
				t.Errorf("expected content %q, got %q", wantContent, content)
			}
		})
	}
}

func TestUpdatePost_NotFound(t *testing.T) {
	f := newFixture(t)
	w := f.do(t, "POST", "/post/update/999", f.member, Controllers.UpdatePostRequest{Content: "Edited"})
	expectStatus(t, w, http.StatusNotFound)
}

func TestUpdateTopic_Access(t *testing.T) {
	tests := []struct {
		name     string
		subforum func(f *fixture) int
		listed   bool
		author   func(f *fixture) int
		editor   func(f *fixture) int
		want     int
	}{
		{"unauthenticated", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return 0 }, http.StatusUnauthorized},
		{"subforum permission but guest role only", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.guestUser }, func(f *fixture) int { return f.guestUser }, http.StatusForbidden},
		{"author with edit-own permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusOK},
		{"author without subforum permission", func(f *fixture) int { return f.subB }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"someone else's topic without edit-others permission", func(f *fixture) int { return f.subA }, false, func(f *fixture) int { return f.admin }, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"author not listed on private subforum", func(f *fixture) int { return f.subPrivate }, false, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"author listed on private subforum", func(f *fixture) int { return f.subPrivate }, true, func(f *fixture) int { return f.member }, func(f *fixture) int { return f.member }, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			sub := tt.subforum(f)
			topicID := f.addTopic(t, sub, tt.author(f), Entities.GeneralTopic, "Old name")
			editor := tt.editor(f)
			if tt.listed {
				f.exec(t, "INSERT INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)", sub, editor)
			}

			name := "New name"
			w := f.do(t, "POST", fmt.Sprintf("/topic/update/%d", topicID), editor, Controllers.UpdateTopicRequest{Name: &name})
			expectStatus(t, w, tt.want)

			wantName := "Old name"
			if tt.want == http.StatusOK {
				wantName = "New name"
			}
			var got string
			if err := f.db.QueryRow("SELECT name FROM topics WHERE id = ?", topicID).Scan(&got); err != nil {
				t.Fatalf("reading topic: %v", err)
			}
			if got != wantName {
				t.Errorf("expected topic name %q, got %q", wantName, got)
			}
		})
	}
}

func TestUpdateTopic_Validation(t *testing.T) {
	t.Run("episode topics are rejected", func(t *testing.T) {
		f := newFixture(t)
		topicID := f.addTopic(t, f.subA, f.member, Entities.EpisodeTopic, "Episode")
		name := "New name"
		w := f.do(t, "POST", fmt.Sprintf("/topic/update/%d", topicID), f.member, Controllers.UpdateTopicRequest{Name: &name})
		expectStatus(t, w, http.StatusBadRequest)
	})

	t.Run("missing topic", func(t *testing.T) {
		f := newFixture(t)
		name := "New name"
		w := f.do(t, "POST", "/topic/update/999", f.member, Controllers.UpdateTopicRequest{Name: &name})
		expectStatus(t, w, http.StatusNotFound)
	})
}

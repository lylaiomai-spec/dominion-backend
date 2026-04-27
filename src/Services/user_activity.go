package Services

import (
	"cuento-backend/src/Events"
	"database/sql"
	"sync"
	"time"
)

type UserActivity struct {
	UserID          int       `json:"user_id"`
	Username        string    `json:"username"`
	CurrentPageType string    `json:"current_page_type"`
	CurrentPageId   string    `json:"current_page_id"`
	LastActive      time.Time `json:"last_active"`
	IsVisible       bool
	connections     int
}

type UserActivityStorage struct {
	users map[int]*UserActivity
	mu    sync.RWMutex
}

var ActivityStorage = UserActivityStorage{
	users: make(map[int]*UserActivity),
}

func (s *UserActivityStorage) AddUser(userID int, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, exists := s.users[userID]; !exists {
		s.users[userID] = &UserActivity{
			UserID:      userID,
			Username:    username,
			LastActive:  time.Now(),
			IsVisible:   true,
			connections: 1,
		}
	} else {
		existing.connections++
		existing.IsVisible = true
		existing.LastActive = time.Now()
		// CurrentPageType and CurrentPageId are intentionally preserved
	}
}

// RemoveUser decrements the connection count for the user and only removes
// them from active storage when their last connection is closed.
// Returns true if the user was fully removed (no more open tabs).
func (s *UserActivityStorage) RemoveUser(userID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[userID]
	if !exists {
		return false
	}
	user.connections--
	if user.connections <= 0 {
		user.connections = 0
		user.IsVisible = false
		return true
	}
	return false
}

// EvictInactiveUsers hides users from the activity list whose LastActive exceeds the timeout.
// Keeps them in the map so they continue receiving page-based notifications.
func (s *UserActivityStorage) EvictInactiveUsers(timeout time.Duration) []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-timeout)
	evicted := make([]int, 0)
	for userID, user := range s.users {
		if user.IsVisible && user.LastActive.Before(cutoff) {
			user.IsVisible = false
			evicted = append(evicted, userID)
		} else if !user.IsVisible && user.connections == 0 && user.LastActive.Before(cutoff) {
			delete(s.users, userID)
		}
	}
	return evicted
}

func (s *UserActivityStorage) UpdateUserLocation(db *sql.DB, userID int, pageType string, pageId string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[userID]
	if !exists {
		return
	}

	oldPageType := user.CurrentPageType
	oldPageId := user.CurrentPageId

	user.CurrentPageType = pageType
	user.CurrentPageId = pageId
	user.LastActive = time.Now()
	user.IsVisible = true

	if pageType == "topic" {
		Events.Publish(db, Events.UserReadingTopic, Events.UserReadingTopicEvent{TopicID: pageId})
	}
	if oldPageType == "topic" && (pageType != "topic" || pageId != oldPageId) {
		Events.Publish(db, Events.UserReadingTopic, Events.UserReadingTopicEvent{TopicID: oldPageId})
	}
}

func (s *UserActivityStorage) GetActiveUsers() []*UserActivity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeUsers := make([]*UserActivity, 0, len(s.users))
	for _, user := range s.users {
		if user.IsVisible {
			activeUsers = append(activeUsers, user)
		}
	}
	return activeUsers
}

func (s *UserActivityStorage) GetUserActivity(userID int) *UserActivity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users[userID]
}

func (s *UserActivityStorage) GetUsersOnPage(pageType string, pageId string) []*UserActivity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	usersOnPage := make([]*UserActivity, 0)
	for _, user := range s.users {
		if user.CurrentPageType == pageType && user.CurrentPageId == pageId {
			usersOnPage = append(usersOnPage, user)
		}
	}
	return usersOnPage
}

func (s *UserActivityStorage) GetUsersOnPageType(pageType string) []*UserActivity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	usersOnPage := make([]*UserActivity, 0)
	for _, user := range s.users {
		if user.IsVisible && user.CurrentPageType == pageType {
			usersOnPage = append(usersOnPage, user)
		}
	}
	return usersOnPage
}

// GetInactiveUserIDs returns IDs of users whose LastActive is older than the given timeout.
func (s *UserActivityStorage) GetInactiveUserIDs(timeout time.Duration) []int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-timeout)
	ids := make([]int, 0)
	for _, user := range s.users {
		if user.LastActive.Before(cutoff) {
			ids = append(ids, user.UserID)
		}
	}
	return ids
}

// UpdateTopicView updates the database table tracking the user's last read position in a topic.
func (s *UserActivityStorage) UpdateTopicView(db *sql.DB, userID int, topicID int64, postID *int64) error {
	query := `
		INSERT INTO user_topic_view (user_id, topic_id, post_id, view_date)
		VALUES (?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE 
			view_date = CASE WHEN VALUES(post_id) > post_id OR post_id IS NULL THEN NOW() ELSE view_date END,
			post_id = CASE WHEN VALUES(post_id) > post_id OR post_id IS NULL THEN VALUES(post_id) ELSE post_id END
	`
	_, err := db.Exec(query, userID, topicID, postID)
	return err
}

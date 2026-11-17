package test

import (
	"cuento-backend/src/Services"
	"testing"
)

func TestUserActivityStorage(t *testing.T) {
	// We need to initialize the internal map because it's private and not exported
	// but the global ActivityStorage is already initialized.
	// For testing purposes, let's use the global one but clear it if possible,
	// or better, if we can't initialize a new one, we might need to export the constructor.

	// Since I can't easily initialize a new UserActivityStorage due to private fields,
	// I'll use the global one for this test, but this is not ideal for parallel tests.
	s := &Services.ActivityStorage

	t.Run("Add and Remove User", func(t *testing.T) {
		s.AddUser(1, "user1")
		// Connected users stay hidden until they report a page.
		s.UpdateUserLocation(nil, 1, "forum", "1")
		active := s.GetActiveUsers()
		found := false
		for _, u := range active {
			if u.UserID == 1 && u.Username == "user1" {
				found = true
				break
			}
		}
		if !found {
			t.Error("User1 not found in active users after AddUser")
		}

		s.RemoveUser(1)
		active = s.GetActiveUsers()
		for _, u := range active {
			if u.UserID == 1 {
				t.Error("User1 still found in active users after RemoveUser")
			}
		}
	})

	t.Run("Update User Location and Get Users On Page", func(t *testing.T) {
		s.AddUser(2, "user2")
		s.AddUser(3, "user3")

		s.UpdateUserLocation(nil, 2, "topic", "10")
		s.UpdateUserLocation(nil, 3, "topic", "10")

		users := s.GetUsersOnPage("topic", "10")
		if len(users) != 2 {
			t.Errorf("Expected 2 users on topic 10, got %d", len(users))
		}

		s.UpdateUserLocation(nil, 2, "forum", "1")
		users = s.GetUsersOnPage("topic", "10")
		if len(users) != 1 {
			t.Errorf("Expected 1 user on topic 10 after user2 moved, got %d", len(users))
		}

		s.RemoveUser(2)
		s.RemoveUser(3)
	})
}

package test

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Services"
	"encoding/json"
	"net/http"
	"testing"
)

// archivingDays is the auto_archiving_days setting used by every test in this file.
const archivingDays = 30

func enableArchiving(t *testing.T, f *fixture) {
	t.Helper()
	f.exec(t, "UPDATE global_settings SET setting_value = 'y' WHERE setting_name = 'auto_archiving_enabled'")
	f.exec(t, "UPDATE global_settings SET setting_value = ? WHERE setting_name = 'auto_archiving_days'", archivingDays)
}

// addTimedCharacter creates an active character whose archiving countdown started daysAgo days ago.
func addTimedCharacter(t *testing.T, f *fixture, userID int, name string, daysAgo, extraDays int) int {
	t.Helper()
	id, _ := f.addCharacter(t, f.subA, userID, name)
	f.exec(t, "INSERT INTO absence_timer_start (character_id, start_date, extra_days) VALUES (?, CURDATE() - INTERVAL ? DAY, ?)", id, daysAgo, extraDays)
	return id
}

// addImmunity covers [today+fromDay 00:00, today+toDay 23:59:59].
func addImmunity(t *testing.T, f *fixture, characterID, fromDay, toDay int) {
	t.Helper()
	f.exec(t, `INSERT INTO auto_archiving_immunity (character_id, start_date, end_date, reason)
		VALUES (?, CURDATE() + INTERVAL ? DAY, CURDATE() + INTERVAL ? DAY + INTERVAL 86399 SECOND, 'Admin')`,
		characterID, fromDay, toDay)
}

func warningCount(t *testing.T, f *fixture, characterID int) int {
	t.Helper()
	return f.count(t, "SELECT COUNT(*) FROM archiving_warning_log WHERE character_id = ?", characterID)
}

func TestRunDailyArchivingJobs_RecalculatesEndedAbsences(t *testing.T) {
	tests := []struct {
		name          string
		absenceFrom   int // days ago
		absenceTo     int // days ago
		wantExtraDays int
	}{
		// A missed daily run must not leave the credit at the value saved when the absence was booked.
		{"ended 3 days ago", 15, 3, 13},
		{"ended 10 days ago is outside the lookback", 22, 10, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			id, _ := f.addCharacter(t, f.subA, f.member, "Wanderer")
			f.exec(t, "UPDATE character_base SET date_last_post = NOW() - INTERVAL 30 DAY WHERE id = ?", id)
			f.exec(t, "INSERT INTO absence_timer_start (character_id, start_date, extra_days) VALUES (?, CURDATE() - INTERVAL 30 DAY, 0)", id)
			f.exec(t, `INSERT INTO absent_users (user_id, absence_start_date, absence_end_date)
				VALUES (?, CURDATE() - INTERVAL ? DAY, CURDATE() - INTERVAL ? DAY + INTERVAL 86399 SECOND)`,
				f.member, tt.absenceFrom, tt.absenceTo)

			Services.RunDailyArchivingJobs(f.db)

			if got := f.count(t, "SELECT extra_days FROM absence_timer_start WHERE character_id = ?", id); got != tt.wantExtraDays {
				t.Errorf("expected extra_days %d, got %d", tt.wantExtraDays, got)
			}
		})
	}
}

func TestRunDailyArchivingJobs_RegularWarnings(t *testing.T) {
	tests := []struct {
		name     string
		immunity func(t *testing.T, f *fixture, id int)
		want     int
	}{
		{"no immunity", func(*testing.T, *fixture, int) {}, 1},
		// Admin and purchased immunity start on the archival day, which is 3 days away.
		{"immunity scheduled from the archival day", func(t *testing.T, f *fixture, id int) { addImmunity(t, f, id, 3, 10) }, 0},
		{"immunity starting after the archival day", func(t *testing.T, f *fixture, id int) { addImmunity(t, f, id, 5, 10) }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			enableArchiving(t, f)
			id := addTimedCharacter(t, f, f.member, "Sleeper", archivingDays-3, 0)
			tt.immunity(t, f, id)

			Services.RunDailyArchivingJobs(f.db)

			if got := f.count(t, "SELECT COUNT(*) FROM archiving_warning_log WHERE character_id = ? AND days_warning = 3", id); got != tt.want {
				t.Errorf("expected %d three-day warning(s), got %d", tt.want, got)
			}
		})
	}
}

func TestRunDailyArchivingJobs_ImmunityExpiryWarnings(t *testing.T) {
	tests := []struct {
		name      string
		extraDays int
		want      int
	}{
		// Countdown started 40 days ago; immunity ends in 3 days, at day 43.
		{"eligible when immunity ends", 0, 1},
		{"absence credit keeps it under the limit", 20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			enableArchiving(t, f)
			id := addTimedCharacter(t, f, f.member, "Guarded", 40, tt.extraDays)
			addImmunity(t, f, id, -5, 3)

			Services.RunDailyArchivingJobs(f.db)

			if got := warningCount(t, f, id); got != tt.want {
				t.Errorf("expected %d warning(s), got %d", tt.want, got)
			}
		})
	}
}

func TestRunDailyArchivingJobs_Archiving(t *testing.T) {
	tests := []struct {
		name       string
		immune     bool
		wantStatus Entities.CharacterStatus
	}{
		{"eligible character is archived", false, Entities.InactiveCharacter},
		{"active admin immunity prevents archiving", true, Entities.ActiveCharacter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			enableArchiving(t, f)
			id := addTimedCharacter(t, f, f.member, "Faded", archivingDays+1, 0)
			if tt.immune {
				addImmunity(t, f, id, -1, 5)
			}

			Services.RunDailyArchivingJobs(f.db)

			if got := Entities.CharacterStatus(f.count(t, "SELECT character_status FROM character_base WHERE id = ?", id)); got != tt.wantStatus {
				t.Errorf("expected character_status %d, got %d", tt.wantStatus, got)
			}
		})
	}
}

func TestGetArchivingWarnings_Access(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		f := newFixture(t)
		expectStatus(t, f.do(t, "GET", "/characters/archiving-warnings", 0, nil), http.StatusUnauthorized)
	})
	t.Run("user without any role", func(t *testing.T) {
		f := newFixture(t)
		expectStatus(t, f.do(t, "GET", "/characters/archiving-warnings", f.noRole, nil), http.StatusForbidden)
	})
}

func TestGetArchivingWarnings_Immunity(t *testing.T) {
	f := newFixture(t)
	enableArchiving(t, f)
	plain := addTimedCharacter(t, f, f.member, "Plain", archivingDays-3, 0)
	// Immunities end beyond the page's 10-day window so they don't show up as expiring.
	covered := addTimedCharacter(t, f, f.member, "Covered", archivingDays-3, 0)
	addImmunity(t, f, covered, 3, 20)
	gap := addTimedCharacter(t, f, f.member, "Gap", archivingDays-3, 0)
	addImmunity(t, f, gap, 5, 20)
	expiring := addTimedCharacter(t, f, f.member, "Expiring", 40, 0)
	addImmunity(t, f, expiring, -5, 3)
	credited := addTimedCharacter(t, f, f.member, "Credited", 40, 20)
	addImmunity(t, f, credited, -5, 3)

	w := f.do(t, "GET", "/characters/archiving-warnings", f.member, nil)
	expectStatus(t, w, http.StatusOK)

	var items []Controllers.ArchivingWarningItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	listed := map[int]bool{}
	for _, it := range items {
		listed[it.Id] = true
	}
	for _, c := range []struct {
		name string
		id   int
		want bool
	}{
		{"no immunity", plain, true},
		{"immunity scheduled from the archival day", covered, false},
		{"immunity starting after the archival day", gap, true},
		{"eligible when immunity ends", expiring, true},
		{"absence credit keeps it under the limit", credited, false},
	} {
		if listed[c.id] != c.want {
			t.Errorf("%s: expected listed=%v, got %v", c.name, c.want, listed[c.id])
		}
	}
}

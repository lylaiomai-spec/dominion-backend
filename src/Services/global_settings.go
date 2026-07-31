package Services

import (
	"database/sql"
	"strconv"
)

func GetGlobalSetting(name string, db *sql.DB) (string, error) {
	var value sql.NullString
	err := db.QueryRow("SELECT setting_value FROM global_settings WHERE setting_name = ?", name).Scan(&value)
	return value.String, err
}


func GetPostsPerPage(db *sql.DB) int {
	val, err := GetGlobalSetting("posts_per_page", db)
	if err != nil {
		return 20
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 20
	}
	return i
}

func GetAbsenceMaxDays(db *sql.DB) int {
	val, err := GetGlobalSetting("absence_max_days", db)
	if err != nil {
		return 30
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 30
	}
	return i
}

func GetAbsenceCooldownDays(db *sql.DB) int {
	val, err := GetGlobalSetting("absence_cooldown_days", db)
	if err != nil {
		return 7
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 7
	}
	return i
}

func GetAutoArchivingShowTable(db *sql.DB) bool {
	val, err := GetGlobalSetting("auto_archiving_show_table", db)
	if err != nil {
		return false
	}
	return val == "y"
}

func GetAutoArchivingEnabled(db *sql.DB) bool {
	val, err := GetGlobalSetting("auto_archiving_enabled", db)
	if err != nil {
		return false
	}
	return val == "y"
}

func GetAutoArchivingDays(db *sql.DB) int {
	val, err := GetGlobalSetting("auto_archiving_days", db)
	if err != nil {
		return 365
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return 365
	}
	return i
}

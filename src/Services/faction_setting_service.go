package Services

import (
	"cuento-backend/src/Entities"
	"database/sql"
)

// FillFactionSettingNames resolves and sets FactionSettingName on each faction.
// For each faction, it looks for a faction_setting where:
//   - level matches the faction's level, AND
//   - parent_faction_id matches the faction's parent_id (specific), OR parent_faction_id IS NULL (fallback)
//
// The specific match (non-null parent_faction_id) takes priority over the generic fallback.
func FillFactionSettingNames(factions []Entities.Faction, db *sql.DB) {
	if len(factions) == 0 {
		return
	}

	rows, err := db.Query("SELECT level, human_name, parent_faction_id FROM faction_settings")
	if err != nil {
		return
	}
	defer rows.Close()

	type setting struct {
		level           int
		humanName       string
		parentFactionId *int
	}

	var settings []setting
	for rows.Next() {
		var s setting
		var parentId sql.NullInt64
		if err := rows.Scan(&s.level, &s.humanName, &parentId); err == nil {
			if parentId.Valid {
				v := int(parentId.Int64)
				s.parentFactionId = &v
			}
			settings = append(settings, s)
		}
	}

	for i := range factions {
		f := &factions[i]
		var specific *string
		var fallback *string

		for j := range settings {
			s := &settings[j]
			if s.level != f.Level {
				continue
			}
			if s.parentFactionId == nil {
				name := s.humanName
				fallback = &name
			} else if f.ParentId != nil && *s.parentFactionId == *f.ParentId {
				name := s.humanName
				specific = &name
				break
			}
		}

		if specific != nil {
			f.FactionSettingName = specific
		} else {
			f.FactionSettingName = fallback
		}
	}
}

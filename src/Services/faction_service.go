package Services

import (
	"cuento-backend/src/Entities"
	"database/sql"
	"sort"
	"strings"
)

func GetFactionTreeByRoot(rootID int, db *sql.DB) ([]Entities.Faction, error) {
	query := `
		SELECT f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.show_on_profile, f.faction_status,
		       f.free_format_date_id
		FROM factions f
		WHERE f.root_id = ? OR f.id = ?
	`
	rows, err := db.Query(query, rootID, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allFactions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		var freeFormatDateId sql.NullInt64
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus,
			&freeFormatDateId); err != nil {
			return nil, err
		}
		if freeFormatDateId.Valid {
			v := int(freeFormatDateId.Int64)
			f.FreeFormatDateId = &v
		}
		allFactions = append(allFactions, f)
	}

	// Build adjacency list
	childrenMap := make(map[int][]Entities.Faction)
	var root Entities.Faction
	var rootFound bool

	for _, f := range allFactions {
		if f.Id == rootID {
			root = f
			rootFound = true
		}
		if f.ParentId != nil {
			childrenMap[*f.ParentId] = append(childrenMap[*f.ParentId], f)
		}
	}

	if !rootFound {
		return []Entities.Faction{}, nil
	}

	// Sort children by name to ensure deterministic order
	for parentID := range childrenMap {
		sort.Slice(childrenMap[parentID], func(i, j int) bool {
			return childrenMap[parentID][i].Name < childrenMap[parentID][j].Name
		})
	}

	// DFS to flatten the tree in pre-order traversal
	var result []Entities.Faction
	var dfs func(int)
	dfs = func(parentID int) {
		if children, ok := childrenMap[parentID]; ok {
			for _, child := range children {
				result = append(result, child)
				dfs(child.Id)
			}
		}
	}

	result = append(result, root)
	dfs(root.Id)

	return result, nil
}

func GetActiveFactionTree(db *sql.DB) ([]Entities.Faction, error) {
	return getFactionTree(db, &[]Entities.FactionStatus{Entities.FactionActive})
}

func GetWantedFactionTree(db *sql.DB) ([]Entities.Faction, error) {
	all, err := getFactionTree(db, &[]Entities.FactionStatus{Entities.FactionActive})
	if err != nil {
		return nil, err
	}

	// Build parent lookup from the flat list.
	parentOf := make(map[int]*int, len(all))
	for _, f := range all {
		parentOf[f.Id] = f.ParentId
	}

	// Collect faction IDs that directly have active wanted characters.
	rows, err := db.Query(`
		SELECT DISTINCT ccf.faction_id
		FROM character_claim_faction ccf
		JOIN wanted_character_base wc ON wc.character_claim_id = ccf.character_claim_id
		WHERE wc.is_claimed = false
		  AND (wc.is_deleted IS NULL OR wc.is_deleted = false)
		  AND wc.wanted_character_status = 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keep := make(map[int]bool)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		// Mark this faction and all its ancestors.
		for cur := &id; cur != nil; {
			if keep[*cur] {
				break
			}
			keep[*cur] = true
			cur = parentOf[*cur]
		}
	}

	result := make([]Entities.Faction, 0)
	for _, f := range all {
		if keep[f.Id] {
			result = append(result, f)
		}
	}
	return result, nil
}

func GetFactionTree(db *sql.DB) ([]Entities.Faction, error) {
	return getFactionTree(db, &[]Entities.FactionStatus{Entities.FactionActive, Entities.FactionInactive})
}

func GetFullFactionTree(db *sql.DB) ([]Entities.Faction, error) {
	return getFactionTree(db, nil)
}

func getFactionTree(db *sql.DB, statusFilter *[]Entities.FactionStatus) ([]Entities.Faction, error) {
	query := `
		SELECT f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.show_on_profile, f.faction_status,
		       f.free_format_date_id
		FROM factions f
	`
	var rows *sql.Rows
	var err error
	if statusFilter != nil && len(*statusFilter) > 0 {
		placeholders := strings.Repeat("?,", len(*statusFilter)-1) + "?"
		query += " WHERE f.faction_status IN (" + placeholders + ")"
		args := make([]interface{}, len(*statusFilter))
		for i, s := range *statusFilter {
			args[i] = s
		}
		rows, err = db.Query(query, args...)
	} else {
		rows, err = db.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allFactions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		var freeFormatDateId sql.NullInt64
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus,
			&freeFormatDateId); err != nil {
			return nil, err
		}
		if freeFormatDateId.Valid {
			v := int(freeFormatDateId.Int64)
			f.FreeFormatDateId = &v
		}
		allFactions = append(allFactions, f)
	}

	// Build adjacency list and identify roots
	childrenMap := make(map[int][]Entities.Faction)
	var roots []Entities.Faction

	for _, f := range allFactions {
		if f.ParentId == nil || *f.ParentId == 0 {
			roots = append(roots, f)
		} else {
			childrenMap[*f.ParentId] = append(childrenMap[*f.ParentId], f)
		}
	}

	// Sort roots by name
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Name < roots[j].Name
	})

	// Sort children by name
	for parentID := range childrenMap {
		sort.Slice(childrenMap[parentID], func(i, j int) bool {
			return childrenMap[parentID][i].Name < childrenMap[parentID][j].Name
		})
	}

	// DFS to flatten the tree
	var result []Entities.Faction
	var dfs func(int)
	dfs = func(parentID int) {
		if children, ok := childrenMap[parentID]; ok {
			for _, child := range children {
				result = append(result, child)
				dfs(child.Id)
			}
		}
	}

	for _, root := range roots {
		result = append(result, root)
		dfs(root.Id)
	}

	return result, nil
}

func CreateFaction(faction Entities.Faction, db DBExecutor) (int64, error) {
	if faction.ParentId != nil && *faction.ParentId == 0 {
		faction.ParentId = nil
	}
	query := `
		INSERT INTO factions (name, parent_id, level, description, icon, show_on_profile)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	res, err := db.Exec(query, faction.Name, faction.ParentId, faction.Level, faction.Description, faction.Icon, faction.ShowOnProfile)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return id, err
}

func AddFactionCharacter(factionID int, characterID int, db DBExecutor) error {
	query := `
		INSERT INTO character_faction (faction_id, character_id) VALUES (?, ?)
	`
	_, err := db.Exec(query, factionID, characterID)
	return err
}

func RemoveFactionCharacter(factionID int, characterID int, db DBExecutor) error {
	query := `
		DELETE FROM character_faction WHERE faction_id = ? AND character_id = ?
	`
	_, err := db.Exec(query, factionID, characterID)
	return err
}

func AddFactionWantedCharacter(factionID int, wantedCharacterID int, db DBExecutor) error {
	_, err := db.Exec("INSERT INTO wanted_character_faction (faction_id, wanted_character_id) VALUES (?, ?)", factionID, wantedCharacterID)
	return err
}

func GetFactionTreeByWantedCharacter(wantedCharacterID int, db *sql.DB) ([]Entities.Faction, error) {
	query := `
		SELECT f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.show_on_profile, f.faction_status
		FROM factions f
		JOIN wanted_character_faction wcf ON f.id = wcf.faction_id
		WHERE wcf.wanted_character_id = ? ORDER BY f.level, f.name
	`
	rows, err := db.Query(query, wantedCharacterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var factions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus); err != nil {
			return nil, err
		}
		factions = append(factions, f)
	}

	if len(factions) == 0 {
		return []Entities.Faction{}, nil
	}

	sort.Slice(factions, func(i, j int) bool {
		if factions[i].Level != factions[j].Level {
			return factions[i].Level < factions[j].Level
		}
		return factions[i].Name < factions[j].Name
	})

	var trees [][]Entities.Faction
	factionToTreeIndex := make(map[int]int)

	for _, f := range factions {
		if f.Level == 0 {
			trees = append(trees, []Entities.Faction{f})
			factionToTreeIndex[f.Id] = len(trees) - 1
		} else {
			if f.ParentId != nil {
				if treeIdx, ok := factionToTreeIndex[*f.ParentId]; ok {
					trees[treeIdx] = append(trees[treeIdx], f)
					factionToTreeIndex[f.Id] = treeIdx
				}
			}
		}
	}

	var result []Entities.Faction
	for _, tree := range trees {
		result = append(result, tree...)
	}

	return result, nil
}

func GetFactionTreeByCharacterClaim(claimID int, db *sql.DB) ([]Entities.Faction, error) {
	query := `
		SELECT f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.show_on_profile, f.faction_status
		FROM factions f
		JOIN character_claim_faction ccf ON f.id = ccf.faction_id
		WHERE ccf.character_claim_id = ? ORDER BY f.level, f.name
	`
	rows, err := db.Query(query, claimID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var factions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus); err != nil {
			return nil, err
		}
		factions = append(factions, f)
	}

	if len(factions) == 0 {
		return []Entities.Faction{}, nil
	}

	sort.Slice(factions, func(i, j int) bool {
		if factions[i].Level != factions[j].Level {
			return factions[i].Level < factions[j].Level
		}
		return factions[i].Name < factions[j].Name
	})

	var trees [][]Entities.Faction
	factionToTreeIndex := make(map[int]int)

	for _, f := range factions {
		if f.Level == 0 {
			trees = append(trees, []Entities.Faction{f})
			factionToTreeIndex[f.Id] = len(trees) - 1
		} else {
			if f.ParentId != nil {
				if treeIdx, ok := factionToTreeIndex[*f.ParentId]; ok {
					trees[treeIdx] = append(trees[treeIdx], f)
					factionToTreeIndex[f.Id] = treeIdx
				}
			}
		}
	}

	var result []Entities.Faction
	for _, tree := range trees {
		result = append(result, tree...)
	}

	FillFactionSettingNames(result, db)

	return result, nil
}

func GetFactionTreeByCharacter(characterID int, db *sql.DB) ([]Entities.Faction, error) {
	query := `
		SELECT f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.show_on_profile, f.faction_status
		FROM factions f
		JOIN character_faction cf ON f.id = cf.faction_id
		WHERE cf.character_id = ? ORDER BY f.level, f.name
	`
	rows, err := db.Query(query, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var factions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus); err != nil {
			return nil, err
		}
		factions = append(factions, f)
	}

	if len(factions) == 0 {
		return []Entities.Faction{}, nil
	}

	sort.Slice(factions, func(i, j int) bool {
		if factions[i].Level != factions[j].Level {
			return factions[i].Level < factions[j].Level
		}
		return factions[i].Name < factions[j].Name
	})

	var trees [][]Entities.Faction
	factionToTreeIndex := make(map[int]int)

	for _, f := range factions {
		if f.Level == 0 {
			trees = append(trees, []Entities.Faction{f})
			factionToTreeIndex[f.Id] = len(trees) - 1
		} else {
			if f.ParentId != nil {
				if treeIdx, ok := factionToTreeIndex[*f.ParentId]; ok {
					trees[treeIdx] = append(trees[treeIdx], f)
					factionToTreeIndex[f.Id] = treeIdx
				}
			}
		}
	}

	var result []Entities.Faction
	for _, tree := range trees {
		result = append(result, tree...)
	}

	FillFactionSettingNames(result, db)

	return result, nil
}

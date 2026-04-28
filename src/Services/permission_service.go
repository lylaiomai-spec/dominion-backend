package Services

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Router"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

type PermissionType int

const (
	EndpointPermission PermissionType = 0
	SubforumPermission PermissionType = 1
)

var SubforumPermissions = map[string]string{
	"subforum_read":                          "permission.subforum_read",
	"subforum_create_general_topic":          "permission.subforum_create_general_topic",
	"subforum_create_episode_topic":          "permission.subforum_create_episode_topic",
	"subforum_create_character_topic":        "permission.subforum_create_character_topic",
	"subforum_create_wanted_character_topic": "permission.subforum_create_wanted_character_topic",
	"subforum_create_lore_topic":             "permission.subforum_create_lore_topic",
	"subforum_post":                          "permission.subforum_post",
	"subforum_delete_topic":                  "permission.subforum_delete_topic",
	"subforum_delete_others_topic":           "permission.subforum_delete_others_topic",
	"subforum_edit_others_post":              "permission.subforum_edit_others_post",
	"subforum_edit_own_post":                 "permission.subforum_edit_own_post",
	"subforum_edit_others_topic":             "permission.subforum_edit_others_topic",
	"subforum_edit_own_topic":                "permission.subforum_edit_own_topic",
}

type PermissionMatrixObject struct {
	Roles           map[int]string          `json:"roles"`
	Permissions     map[string]string       `json:"permissions"`
	Matrix          map[string]map[int]bool `json:"matrix"`
	PermissionOrder []string                `json:"permission_order"`
}

func GetEndpointPermissionMatrix(db *sql.DB, lang string) (PermissionMatrixObject, error) {
	// 1. Fetch all roles
	roleRows, err := db.Query("SELECT id, name FROM roles")
	if err != nil {
		return PermissionMatrixObject{}, err
	}
	defer roleRows.Close()

	roleMap := make(map[int]string)
	for roleRows.Next() {
		var role Entities.Role
		if err := roleRows.Scan(&role.Id, &role.Name); err != nil {
			return PermissionMatrixObject{}, err
		}
		roleMap[role.Id] = role.Name
	}

	// 2. Fetch all existing role-permission relationships
	permRows, err := db.Query("SELECT role_id, permission FROM role_permission WHERE type = 0")
	if err != nil {
		return PermissionMatrixObject{}, err
	}
	defer permRows.Close()

	existingPerms := make(map[string]map[int]bool) // permission -> roleID -> true
	for permRows.Next() {
		var roleID int
		var permission string
		if err := permRows.Scan(&roleID, &permission); err != nil {
			continue
		}
		if _, ok := roleMap[roleID]; ok {
			if _, ok := existingPerms[permission]; !ok {
				existingPerms[permission] = make(map[int]bool)
			}
			existingPerms[permission][roleID] = true
		}
	}

	// 3. Build the full matrix, permissions map, and ordered list of keys
	permissionMatrix := make(map[string]map[int]bool)
	permissionsMap := make(map[string]string)
	permissionOrder := make([]string, len(Router.ProtectedRoutes))

	localizer := NewLocalizer(lang)
	for i, route := range Router.ProtectedRoutes {
		permission := route.Path
		permissionOrder[i] = permission
		permissionsMap[permission] = T(localizer, route.Definition)
		permissionMatrix[permission] = make(map[int]bool)
		for roleID := range roleMap {
			if rolesWithPerm, ok := existingPerms[permission]; ok {
				permissionMatrix[permission][roleID] = rolesWithPerm[roleID]
			} else {
				permissionMatrix[permission][roleID] = false
			}
		}
	}

	return PermissionMatrixObject{
		Roles:           roleMap,
		Permissions:     permissionsMap,
		Matrix:          permissionMatrix,
		PermissionOrder: permissionOrder,
	}, nil
}

func GetSubforumPermissionMatrix(db *sql.DB, lang string) (PermissionMatrixObject, error) {
	// 1. Fetch all roles
	roleRows, err := db.Query("SELECT id, name FROM roles")
	if err != nil {
		return PermissionMatrixObject{}, err
	}
	defer roleRows.Close()

	roleMap := make(map[int]string)
	for roleRows.Next() {
		var role Entities.Role
		if err := roleRows.Scan(&role.Id, &role.Name); err != nil {
			return PermissionMatrixObject{}, err
		}
		roleMap[role.Id] = role.Name
	}

	// 2. Fetch all subforums
	subforumRows, err := db.Query("SELECT id, name FROM subforums")
	if err != nil {
		return PermissionMatrixObject{}, err
	}
	defer subforumRows.Close()

	type SubforumInfo struct {
		ID   int
		Name string
	}
	var subforums []SubforumInfo
	for subforumRows.Next() {
		var sub SubforumInfo
		if err := subforumRows.Scan(&sub.ID, &sub.Name); err != nil {
			return PermissionMatrixObject{}, err
		}
		subforums = append(subforums, sub)
	}

	// 3. Fetch all existing subforum role-permission relationships
	permRows, err := db.Query("SELECT role_id, permission FROM role_permission WHERE type = 1")
	if err != nil {
		return PermissionMatrixObject{}, err
	}
	defer permRows.Close()

	existingPerms := make(map[string]map[int]bool) // permission -> roleID -> true
	for permRows.Next() {
		var roleID int
		var permission string
		if err := permRows.Scan(&roleID, &permission); err != nil {
			continue
		}
		if _, ok := roleMap[roleID]; ok {
			if _, ok := existingPerms[permission]; !ok {
				existingPerms[permission] = make(map[int]bool)
			}
			existingPerms[permission][roleID] = true
		}
	}

	// 4. Build the matrix, permissions map, and ordered list of keys
	permissionMatrix := make(map[string]map[int]bool)
	allPossiblePerms := make(map[string]string)
	permissionOrder := make([]string, 0)

	localizer := NewLocalizer(lang)
	for _, sub := range subforums {
		for permKey, permDef := range SubforumPermissions {
			permissionString := fmt.Sprintf("%s:%d", permKey, sub.ID)
			humanReadableDef := fmt.Sprintf("Subforum '%s' (ID %d): %s", sub.Name, sub.ID, T(localizer, permDef))

			permissionOrder = append(permissionOrder, permissionString)
			allPossiblePerms[permissionString] = humanReadableDef

			permissionMatrix[permissionString] = make(map[int]bool)
			for roleID := range roleMap {
				if rolesWithPerm, ok := existingPerms[permissionString]; ok {
					permissionMatrix[permissionString][roleID] = rolesWithPerm[roleID]
				} else {
					permissionMatrix[permissionString][roleID] = false
				}
			}
		}
	}

	return PermissionMatrixObject{
		Roles:           roleMap,
		Permissions:     allPossiblePerms,
		Matrix:          permissionMatrix,
		PermissionOrder: permissionOrder,
	}, nil
}

func UpdatePermissionMatrix(permissions []string, db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback() // Rollback on error

	// 1. Get all roles to map names to IDs
	roleRows, err := tx.Query("SELECT id, name FROM roles")
	if err != nil {
		return fmt.Errorf("failed to fetch roles: %w", err)
	}
	defer roleRows.Close()

	roleNameToID := make(map[string]int)
	for roleRows.Next() {
		var role Entities.Role
		if err := roleRows.Scan(&role.Id, &role.Name); err != nil {
			return fmt.Errorf("failed to scan role: %w", err)
		}
		roleNameToID[role.Name] = role.Id
	}

	// 2. Wipe all old permissions
	if _, err := tx.Exec("DELETE FROM role_permission"); err != nil {
		return fmt.Errorf("failed to delete old permissions: %w", err)
	}

	// 3. Prepare for bulk insert
	stmt, err := tx.Prepare("INSERT INTO role_permission (type, role_id, permission) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// 4. Parse and insert new permissions
	for _, p := range permissions {
		parts := strings.SplitN(p, ".", 3)
		if len(parts) != 3 {
			continue // Skip malformed strings
		}

		permType, err := strconv.Atoi(parts[0])
		if err != nil {
			continue // Skip if type is not a number
		}

		roleName := parts[1]
		roleID, ok := roleNameToID[roleName]
		if !ok {
			continue // Skip if role name is invalid
		}

		permission := parts[2]

		if _, err := stmt.Exec(permType, roleID, permission); err != nil {
			return fmt.Errorf("failed to insert permission '%s': %w", p, err)
		}
	}

	// 5. Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func HasPermission(userID int, permission string, db *sql.DB) (bool, error) {
	if userID > 0 {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM role_permission rp
			JOIN user_role ur ON rp.role_id = ur.role_id
			WHERE ur.user_id = ? AND rp.permission = ?
		`, userID, permission).Scan(&count)
		if err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}

	// Fall back to guest role, mirroring GetVisibleSubforums behaviour
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM role_permission rp
		JOIN roles r ON rp.role_id = r.id
		WHERE r.name = 'guest' AND rp.permission = ?
	`, permission).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func GetVisibleSubforums(userID int, permission string, db *sql.DB) ([]int, error) {
	var roleIDs []int
	if userID > 0 {
		rows, err := db.Query("SELECT role_id FROM user_role WHERE user_id = ?", userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var rID int
			if err := rows.Scan(&rID); err == nil {
				roleIDs = append(roleIDs, rID)
			}
		}
	}

	if len(roleIDs) == 0 {
		var guestID int
		err := db.QueryRow("SELECT id FROM roles WHERE name = 'guest'").Scan(&guestID)
		if err == nil {
			roleIDs = append(roleIDs, guestID)
		}
	}

	if len(roleIDs) == 0 {
		return []int{}, nil
	}

	placeholders := strings.Repeat("?,", len(roleIDs)-1) + "?"
	query := fmt.Sprintf("SELECT permission FROM role_permission WHERE type = 1 AND role_id IN (%s) AND permission LIKE ?", placeholders)

	args := []interface{}{}
	for _, id := range roleIDs {
		args = append(args, id)
	}
	args = append(args, permission+":%")

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seenSubforums := make(map[int]bool)
	var subforumIDs []int
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			parts := strings.Split(p, ":")
			if len(parts) == 2 {
				if id, err := strconv.Atoi(parts[1]); err == nil {
					if !seenSubforums[id] {
						subforumIDs = append(subforumIDs, id)
						seenSubforums[id] = true
					}
				}
			}
		}
	}

	return subforumIDs, nil
}

func GetSubforumPermissions(userID int, subforumID int, db *sql.DB) (*Entities.SubforumPermissions, error) {
	var roleIDs []int
	if userID > 0 {
		rows, err := db.Query("SELECT role_id FROM user_role WHERE user_id = ?", userID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rID int
				if err := rows.Scan(&rID); err == nil {
					roleIDs = append(roleIDs, rID)
				}
			}
		}
	}

	if len(roleIDs) == 0 {
		var guestID int
		err := db.QueryRow("SELECT id FROM roles WHERE name = 'guest'").Scan(&guestID)
		if err == nil {
			roleIDs = append(roleIDs, guestID)
		}
	}

	permissions := &Entities.SubforumPermissions{}
	if len(roleIDs) == 0 {
		return permissions, nil
	}

	permMap := map[string]*bool{
		fmt.Sprintf("subforum_create_general_topic:%d", subforumID):          &permissions.SubforumCreateGeneralTopic,
		fmt.Sprintf("subforum_create_episode_topic:%d", subforumID):          &permissions.SubforumCreateEpisodeTopic,
		fmt.Sprintf("subforum_create_character_topic:%d", subforumID):        &permissions.SubforumCreateCharacterTopic,
		fmt.Sprintf("subforum_create_wanted_character_topic:%d", subforumID): &permissions.SubforumCreateWantedCharacterTopic,
		fmt.Sprintf("subforum_create_lore_topic:%d", subforumID):             &permissions.SubforumCreateLoreTopic,
		fmt.Sprintf("subforum_post:%d", subforumID):                          &permissions.SubforumPost,
		fmt.Sprintf("subforum_delete_topic:%d", subforumID):                  &permissions.SubforumDeleteOwnTopic,
		fmt.Sprintf("subforum_delete_others_topic:%d", subforumID):           &permissions.SubforumDeleteOthersTopic,
		fmt.Sprintf("subforum_edit_others_post:%d", subforumID):              &permissions.SubforumEditOthersPost,
		fmt.Sprintf("subforum_edit_own_post:%d", subforumID):                 &permissions.SubforumEditOwnPost,
	}

	var permStrings []string
	var args []interface{}
	for p := range permMap {
		permStrings = append(permStrings, p)
	}

	placeholders := func(n int) string {
		if n <= 0 {
			return ""
		}
		return strings.Repeat("?,", n-1) + "?"
	}

	query := fmt.Sprintf("SELECT permission FROM role_permission WHERE type = 1 AND role_id IN (%s) AND permission IN (%s)",
		placeholders(len(roleIDs)),
		placeholders(len(permStrings)))

	for _, rID := range roleIDs {
		args = append(args, rID)
	}
	for _, p := range permStrings {
		args = append(args, p)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return permissions, err
	}
	defer rows.Close()

	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			if val, ok := permMap[p]; ok {
				*val = true
			}
		}
	}

	return permissions, nil
}

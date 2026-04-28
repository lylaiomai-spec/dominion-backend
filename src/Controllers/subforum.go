package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func scanCategoryRows(rows *sql.Rows, userTimezone *string) ([]Entities.Category, error) {
	var categories []Entities.Category
	for rows.Next() {
		var sub Entities.Subform
		var cat Entities.Category
		var dateLastPost *time.Time
		var subID sql.NullInt64
		var subName, subDescription sql.NullString
		var subPosition, topicNumber, postNumber sql.NullInt64
		if err := rows.Scan(
			&subID,
			&subName,
			&subDescription,
			&subPosition,
			&topicNumber,
			&postNumber,
			&sub.LastPostTopicId,
			&sub.LastPostTopicName,
			&sub.LastPostId,
			&dateLastPost,
			&sub.LastPostAuthorName,
			&sub.ShowLastTopic,
			&cat.Id,
			&cat.Name,
			&cat.Position,
		); err != nil {
			return nil, err
		}

		if len(categories) == 0 || categories[len(categories)-1].Id != cat.Id {
			cat.Subforums = []Entities.Subform{}
			categories = append(categories, cat)
		}

		if !subID.Valid {
			continue
		}

		sub.Id = int(subID.Int64)
		sub.Name = subName.String
		sub.Description = subDescription.String
		sub.Position = int(subPosition.Int64)
		sub.TopicNumber = int(topicNumber.Int64)
		sub.PostNumber = int(postNumber.Int64)
		if dateLastPost != nil {
			localized := Services.LocalizeTime(*dateLastPost, userTimezone)
			sub.DateLastPostLocalized = &localized
		}
		sub.DescriptionHtml = Services.ParseBBCode(sub.Description)

		categories[len(categories)-1].Subforums = append(categories[len(categories)-1].Subforums, sub)
	}
	if categories == nil {
		categories = []Entities.Category{}
	}
	return categories, nil
}

func GetHomeCategories(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	visibleSubforumIDs, err := Services.GetVisibleSubforums(userID, "subforum_read", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine visible subforums: " + err.Error()})
		c.Abort()
		return
	}

	if len(visibleSubforumIDs) == 0 {
		c.JSON(http.StatusOK, []Entities.Category{})
		return
	}

	placeholders := strings.Repeat("?,", len(visibleSubforumIDs)-1) + "?"
	query := fmt.Sprintf(`
		SELECT
			subforums.id,
			subforums.name,
			subforums.description,
			subforums.position,
			subforums.topic_number,
			subforums.post_number,
			subforums.last_post_topic_id,
			subforums.last_post_topic_name,
			subforums.last_post_id,
			subforums.date_last_post,
			subforums.last_post_author_user_name,
			subforums.show_last_topic,
			categories.id,
			categories.name,
			categories.position
		FROM categories
		JOIN subforums ON subforums.category_id = categories.id AND subforums.id IN (%s)
		ORDER BY categories.position, subforums.position`, placeholders)

	args := make([]interface{}, len(visibleSubforumIDs))
	for i, id := range visibleSubforumIDs {
		args[i] = id
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get categories: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	categories, err := scanCategoryRows(rows, Services.GetUserTimezone(userID, db))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan category data: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, categories)
}

func GetAdminHomeCategories(c *gin.Context, db *sql.DB) {
	query := `
		SELECT
			subforums.id,
			subforums.name,
			subforums.description,
			subforums.position,
			subforums.topic_number,
			subforums.post_number,
			subforums.last_post_topic_id,
			subforums.last_post_topic_name,
			subforums.last_post_id,
			subforums.date_last_post,
			subforums.last_post_author_user_name,
			subforums.show_last_topic,
			categories.id,
			categories.name,
			categories.position
		FROM categories
		LEFT JOIN subforums ON subforums.category_id = categories.id
		ORDER BY categories.position, subforums.position`

	rows, err := db.Query(query)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get categories: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	userID := Services.GetUserIdFromContext(c)
	categories, err := scanCategoryRows(rows, Services.GetUserTimezone(userID, db))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan category data: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, categories)
}

func GetShortSubforumList(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	visibleIDs, err := Services.GetVisibleSubforums(userID, "subforum_read", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine visible subforums: " + err.Error()})
		c.Abort()
		return
	}

	if len(visibleIDs) == 0 {
		c.JSON(http.StatusOK, []Entities.ShortSubform{})
		return
	}

	placeholders := strings.Repeat("?,", len(visibleIDs)-1) + "?"
	query := fmt.Sprintf("SELECT id, name FROM subforums WHERE id IN (%s) ORDER BY position", placeholders)
	args := make([]interface{}, len(visibleIDs))
	for i, id := range visibleIDs {
		args[i] = id
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get subforums: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	subforums := []Entities.ShortSubform{}
	for rows.Next() {
		var s Entities.ShortSubform
		if err := rows.Scan(&s.Id, &s.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan subforums: " + err.Error()})
			c.Abort()
			return
		}
		subforums = append(subforums, s)
	}

	c.JSON(http.StatusOK, subforums)
}

func CreateCategory(c *gin.Context, db *sql.DB) {
	var input struct {
		Name     string `json:"name" binding:"required"`
		Position int    `json:"position"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid input: " + err.Error()})
		c.Abort()
		return
	}

	result, err := db.Exec("INSERT INTO categories (name, position) VALUES (?, ?)", input.Name, input.Position)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create category: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := result.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func UpdateCategory(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var input struct {
		Name     string `json:"name" binding:"required"`
		Position int    `json:"position"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid input: " + err.Error()})
		c.Abort()
		return
	}

	result, err := db.Exec("UPDATE categories SET name = ?, position = ? WHERE id = ?", input.Name, input.Position, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update category: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Category not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated"})
}

func CreateSubforum(c *gin.Context, db *sql.DB) {
	var input struct {
		CategoryId    int    `json:"category_id" binding:"required"`
		Name          string `json:"name" binding:"required"`
		Description   string `json:"description"`
		Position      int    `json:"position"`
		ShowLastTopic *bool  `json:"show_last_topic"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid input: " + err.Error()})
		c.Abort()
		return
	}

	result, err := db.Exec(
		"INSERT INTO subforums (category_id, name, description, position, show_last_topic) VALUES (?, ?, ?, ?, ?)",
		input.CategoryId, input.Name, input.Description, input.Position, input.ShowLastTopic,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create subforum: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := result.LastInsertId()

	var adminRoleID int
	if err := db.QueryRow("SELECT id FROM roles WHERE name = 'admin'").Scan(&adminRoleID); err == nil {
		excluded := map[string]bool{
			"subforum_create_episode_topic":          true,
			"subforum_create_character_topic":        true,
			"subforum_create_wanted_character_topic": true,
		}
		for permKey := range Services.SubforumPermissions {
			if excluded[permKey] {
				continue
			}
			perm := fmt.Sprintf("%s:%d", permKey, id)
			_, _ = db.Exec("INSERT INTO role_permission (type, role_id, permission) VALUES (1, ?, ?)", adminRoleID, perm)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func UpdateSubforum(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var input struct {
		CategoryId  int    `json:"category_id" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Position    int    `json:"position"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid input: " + err.Error()})
		c.Abort()
		return
	}

	result, err := db.Exec(
		"UPDATE subforums SET category_id = ?, name = ?, description = ?, position = ? WHERE id = ?",
		input.CategoryId, input.Name, input.Description, input.Position, id,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update subforum: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Subforum not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subforum updated"})
}

func DeleteCategory(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var subforumCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM subforums WHERE category_id = ?", id).Scan(&subforumCount); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check subforums: " + err.Error()})
		c.Abort()
		return
	}
	if subforumCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Cannot delete category: it still has subforums"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM categories WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete category: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Category not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted"})
}

func DeleteSubforum(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var topicCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM topics WHERE subforum_id = ?", id).Scan(&topicCount); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check topics: " + err.Error()})
		c.Abort()
		return
	}
	if topicCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Cannot delete subforum: it still has topics"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM subforums WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete subforum: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Subforum not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Subforum deleted"})
}

func GetSubforum(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var subforum Entities.Subform
	var dateLastPost *time.Time
	var topicNumber, postNumber sql.NullInt64
	query := "SELECT id, category_id, name, description, position, topic_number, post_number, last_post_topic_id, last_post_topic_name, last_post_id, date_last_post, last_post_author_user_name, show_last_topic FROM subforums WHERE id = ?"
	err = db.QueryRow(query, id).Scan(
		&subforum.Id,
		&subforum.CategoryId,
		&subforum.Name,
		&subforum.Description,
		&subforum.Position,
		&topicNumber,
		&postNumber,
		&subforum.LastPostTopicId,
		&subforum.LastPostTopicName,
		&subforum.LastPostId,
		&dateLastPost,
		&subforum.LastPostAuthorName,
		&subforum.ShowLastTopic,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Subforum not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get subforum: " + err.Error()})
		}
		c.Abort()
		return
	}

	subforum.TopicNumber = int(topicNumber.Int64)
	subforum.PostNumber = int(postNumber.Int64)

	// Determine User Roles
	var roleIDs []int
	userID := Services.GetUserIdFromContext(c)
	if dateLastPost != nil {
		localized := Services.LocalizeTime(*dateLastPost, Services.GetUserTimezone(userID, db))
		subforum.DateLastPostLocalized = &localized
	}
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
		err := db.QueryRow("SELECT id FROM roles WHERE name = 'Guest'").Scan(&guestID)
		if err == nil {
			roleIDs = append(roleIDs, guestID)
		}
	}

	// Check Permissions
	permissions := &Entities.SubforumPermissions{}
	subforum.Permissions = permissions

	if len(roleIDs) > 0 {
		permMap := map[string]*bool{
			fmt.Sprintf("subforum_create_general_topic:%d", id):          &permissions.SubforumCreateGeneralTopic,
			fmt.Sprintf("subforum_create_episode_topic:%d", id):          &permissions.SubforumCreateEpisodeTopic,
			fmt.Sprintf("subforum_create_character_topic:%d", id):        &permissions.SubforumCreateCharacterTopic,
			fmt.Sprintf("subforum_create_wanted_character_topic:%d", id): &permissions.SubforumCreateWantedCharacterTopic,
			fmt.Sprintf("subforum_create_lore_topic:%d", id):             &permissions.SubforumCreateLoreTopic,
			fmt.Sprintf("subforum_post:%d", id):                          &permissions.SubforumPost,
			fmt.Sprintf("subforum_delete_topic:%d", id):                  &permissions.SubforumDeleteOwnTopic,
			fmt.Sprintf("subforum_delete_others_topic:%d", id):           &permissions.SubforumDeleteOthersTopic,
			fmt.Sprintf("subforum_edit_others_post:%d", id):              &permissions.SubforumEditOthersPost,
			fmt.Sprintf("subforum_edit_own_post:%d", id):                 &permissions.SubforumEditOwnPost,
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
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var p string
				if err := rows.Scan(&p); err == nil {
					if val, ok := permMap[p]; ok {
						*val = true
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, subforum)
}

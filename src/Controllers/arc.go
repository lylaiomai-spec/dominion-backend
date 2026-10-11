package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type Arc struct {
	ID           int                     `json:"id"`
	Title        string                  `json:"title"`
	Description  *string                 `json:"description"`
	IsPublic     bool                    `json:"is_public"`
	Status       Entities.ArcStatus      `json:"status"`
	ImageURL     *string                 `json:"image_url"`
	ThumbnailURL *string                 `json:"thumbnail_url"`
	CreatorID    *int                    `json:"creator_id"`
	Factions     []Entities.FactionShort `json:"factions"`
	GameMasters  []Entities.ShortUser    `json:"game_masters"`
	EpisodeCount int                     `json:"episode_count"`
	CanEdit      bool                    `json:"can_edit"`
	SubforumID   *int                    `json:"subforum_id"`
}

type GetArcListRequest struct {
	Statuses   []Entities.ArcStatus `json:"statuses"`
	FactionIDs []int                `json:"faction_ids"`
	Search     string               `json:"search"`
}

type CreateArcRequest struct {
	Title          string              `json:"title" binding:"required"`
	Description    *string             `json:"description"`
	IsPublic       bool                `json:"is_public"`
	Status         Entities.ArcStatus  `json:"status"`
	ImageURL       *string             `json:"image_url"`
	FactionIDs     []int               `json:"faction_ids"`
	GameMasterIDs  []int               `json:"game_master_ids"`
}

type UpdateArcRequest struct {
	Title          *string             `json:"title"`
	Description    *string             `json:"description"`
	IsPublic       *bool               `json:"is_public"`
	Status         *Entities.ArcStatus `json:"status"`
	ImageURL       *string             `json:"image_url"`
	FactionIDs     *[]int              `json:"faction_ids"`
	GameMasterIDs  *[]int              `json:"game_master_ids"`
}

func GetArcList(c *gin.Context, db *sql.DB) {
	var req GetArcListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	where := []string{}
	args := []interface{}{}

	if userID == 0 {
		where = append(where, "a.is_public = 1")
	}

	if len(req.Statuses) > 0 {
		placeholders := make([]string, len(req.Statuses))
		for i, s := range req.Statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		where = append(where, "a.status IN ("+strings.Join(placeholders, ",")+")")
	}

	if req.Search != "" {
		where = append(where, "a.title LIKE ?")
		args = append(args, "%"+req.Search+"%")
	}

	if len(req.FactionIDs) > 0 {
		placeholders := make([]string, len(req.FactionIDs))
		for i, id := range req.FactionIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where = append(where, "a.id IN (SELECT arc_id FROM arc_factions WHERE faction_id IN ("+strings.Join(placeholders, ",")+")"+")")
	}

	query := "SELECT a.id, a.title, a.is_public, a.status, a.image_url, a.creator_id FROM arcs a"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY a.id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch arcs: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	list := []Arc{}
	arcIndex := map[int]int{} // arc ID → index in list
	for rows.Next() {
		var a Arc
		var creatorID sql.NullInt64
		var imageURL sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.IsPublic, &a.Status, &imageURL, &creatorID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan arc: " + err.Error()})
			c.Abort()
			return
		}
		if imageURL.Valid {
			a.ImageURL = &imageURL.String
			if thumb, err := Services.GetResizedImageURL(imageURL.String, 0, 60, db); err == nil {
				a.ThumbnailURL = &thumb
			}
		}
		if creatorID.Valid {
			id := int(creatorID.Int64)
			a.CreatorID = &id
		}
		a.Factions = []Entities.FactionShort{}
		a.GameMasters = []Entities.ShortUser{}
		arcIndex[a.ID] = len(list)
		list = append(list, a)
	}

	if len(list) == 0 {
		c.JSON(http.StatusOK, list)
		return
	}

	arcIDs := make([]interface{}, len(list))
	placeholders := make([]string, len(list))
	for i, a := range list {
		arcIDs[i] = a.ID
		placeholders[i] = "?"
	}
	inClause := strings.Join(placeholders, ",")

	// Factions
	factionRows, err := db.Query(
		"SELECT af.arc_id, f.id, f.name FROM arc_factions af JOIN factions f ON f.id = af.faction_id WHERE af.arc_id IN ("+inClause+")",
		arcIDs...,
	)
	if err == nil {
		defer factionRows.Close()
		for factionRows.Next() {
			var arcID int
			var f Entities.FactionShort
			if err := factionRows.Scan(&arcID, &f.Id, &f.Name); err == nil {
				idx := arcIndex[arcID]
				list[idx].Factions = append(list[idx].Factions, f)
			}
		}
	}

	// Game masters
	gmRows, err := db.Query(
		"SELECT agm.arc_id, u.id, u.username FROM arc_game_masters agm JOIN users u ON u.id = agm.user_id WHERE agm.arc_id IN ("+inClause+")",
		arcIDs...,
	)
	if err == nil {
		defer gmRows.Close()
		for gmRows.Next() {
			var arcID int
			var gm Entities.ShortUser
			if err := gmRows.Scan(&arcID, &gm.Id, &gm.Username); err == nil {
				idx := arcIndex[arcID]
				list[idx].GameMasters = append(list[idx].GameMasters, gm)
			}
		}
	}

	// Episode counts
	countRows, err := db.Query(
		"SELECT arc_id, COUNT(*) FROM arc_episodes WHERE arc_id IN ("+inClause+") GROUP BY arc_id",
		arcIDs...,
	)
	if err == nil {
		defer countRows.Close()
		for countRows.Next() {
			var arcID, count int
			if err := countRows.Scan(&arcID, &count); err == nil {
				list[arcIndex[arcID]].EpisodeCount = count
			}
		}
	}

	c.JSON(http.StatusOK, list)
}

func GetArc(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid arc ID"})
		c.Abort()
		return
	}

	var a Arc
	var creatorID sql.NullInt64
	var description sql.NullString
	var imageURL sql.NullString
	var subforumID sql.NullInt64
	err = db.QueryRow("SELECT id, title, description, is_public, status, image_url, creator_id, subforum_id FROM arcs WHERE id = ?", id).
		Scan(&a.ID, &a.Title, &description, &a.IsPublic, &a.Status, &imageURL, &creatorID, &subforumID)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Arc not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch arc: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if !a.IsPublic && userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "This arc is not public"})
		c.Abort()
		return
	}

	if description.Valid {
		a.Description = &description.String
	}
	if imageURL.Valid {
		a.ImageURL = &imageURL.String
	}
	if creatorID.Valid {
		cid := int(creatorID.Int64)
		a.CreatorID = &cid
	}

	if userID != 0 {
		isCreator := a.CreatorID != nil && *a.CreatorID == userID
		if !isCreator {
			var gmCount int
			_ = db.QueryRow("SELECT COUNT(*) FROM arc_game_masters WHERE arc_id = ? AND user_id = ?", a.ID, userID).Scan(&gmCount)
			if gmCount > 0 {
				a.CanEdit = true
			} else {
				a.CanEdit, _ = Services.HasPermission(userID, "/arc/update/:id", db)
			}
		} else {
			a.CanEdit = true
		}
	}

	a.Factions = []Entities.FactionShort{}
	a.GameMasters = []Entities.ShortUser{}

	factionRows, err := db.Query(
		"SELECT f.id, f.name FROM arc_factions af JOIN factions f ON af.faction_id = f.id WHERE af.arc_id = ?", id,
	)
	if err == nil {
		defer factionRows.Close()
		for factionRows.Next() {
			var f Entities.FactionShort
			if factionRows.Scan(&f.Id, &f.Name) == nil {
				a.Factions = append(a.Factions, f)
			}
		}
	}

	gmRows, err := db.Query(
		"SELECT u.id, u.username FROM arc_game_masters agm JOIN users u ON agm.user_id = u.id WHERE agm.arc_id = ?", id,
	)
	if err == nil {
		defer gmRows.Close()
		for gmRows.Next() {
			var u Entities.ShortUser
			if gmRows.Scan(&u.Id, &u.Username) == nil {
				a.GameMasters = append(a.GameMasters, u)
			}
		}
	}

	if subforumID.Valid {
		sfID := int(subforumID.Int64)
		if canAccess, _ := Services.HasPermission(userID, fmt.Sprintf("subforum_read:%d", sfID), db); canAccess {
			a.SubforumID = &sfID
		}
	}

	db.QueryRow("SELECT COUNT(*) FROM arc_episodes WHERE arc_id = ?", id).Scan(&a.EpisodeCount)

	c.JSON(http.StatusOK, a)
}

func CreateArc(c *gin.Context, db *sql.DB) {
	var req CreateArcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	canCreate, _ := Services.HasPermission(userID, "/arc/create", db)
	if !canCreate {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to create arcs"})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO arcs (title, description, is_public, status, image_url, creator_id) VALUES (?, ?, ?, ?, ?, ?)",
		req.Title, req.Description, req.IsPublic, req.Status, req.ImageURL, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create arc: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()
	arcID := int(newID)

	if len(req.FactionIDs) > 0 {
		factionVals := make([]string, len(req.FactionIDs))
		factionArgs := make([]interface{}, len(req.FactionIDs)*2)
		for i, fid := range req.FactionIDs {
			factionVals[i] = "(?, ?)"
			factionArgs[i*2] = arcID
			factionArgs[i*2+1] = fid
		}
		db.Exec("INSERT IGNORE INTO arc_factions (arc_id, faction_id) VALUES "+strings.Join(factionVals, ","), factionArgs...)
	}

	if len(req.GameMasterIDs) > 0 {
		gmVals := make([]string, len(req.GameMasterIDs))
		gmArgs := make([]interface{}, len(req.GameMasterIDs)*2)
		for i, uid := range req.GameMasterIDs {
			gmVals[i] = "(?, ?)"
			gmArgs[i*2] = arcID
			gmArgs[i*2+1] = uid
		}
		db.Exec("INSERT IGNORE INTO arc_game_masters (arc_id, user_id) VALUES "+strings.Join(gmVals, ","), gmArgs...)
	}

	Events.Publish(db, Events.ArcCreated, Events.ArcCreatedEvent{
		ArcID:     arcID,
		Title:     req.Title,
		GMUserIDs: req.GameMasterIDs,
	})

	c.JSON(http.StatusCreated, gin.H{"id": arcID})
}

func UpdateArc(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid arc ID"})
		c.Abort()
		return
	}

	var req UpdateArcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.Title == nil && req.Description == nil && req.IsPublic == nil && req.Status == nil && req.ImageURL == nil && req.FactionIDs == nil && req.GameMasterIDs == nil {
		c.JSON(http.StatusOK, gin.H{"message": "Arc updated successfully"})
		return
	}

	userID := Services.GetUserIdFromContext(c)

	var creatorID sql.NullInt64
	if err := db.QueryRow("SELECT creator_id FROM arcs WHERE id = ?", id).Scan(&creatorID); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Arc not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch arc: " + err.Error()})
		}
		c.Abort()
		return
	}

	isCreator := creatorID.Valid && int(creatorID.Int64) == userID

	isGM := false
	if !isCreator {
		var gmCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM arc_game_masters WHERE arc_id = ? AND user_id = ?", id, userID).Scan(&gmCount); err == nil {
			isGM = gmCount > 0
		}
	}

	if !isCreator && !isGM {
		hasPerm, _ := Services.HasPermission(userID, "/arc/update/:id", db)
		if !hasPerm {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this arc"})
			c.Abort()
			return
		}
	}

	setClauses := []string{}
	args := []interface{}{}
	if req.Title != nil {
		setClauses = append(setClauses, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *req.Description)
	}
	if req.IsPublic != nil {
		setClauses = append(setClauses, "is_public = ?")
		args = append(args, *req.IsPublic)
	}
	if req.Status != nil {
		setClauses = append(setClauses, "status = ?")
		args = append(args, *req.Status)
	}
	if req.ImageURL != nil {
		setClauses = append(setClauses, "image_url = ?")
		args = append(args, *req.ImageURL)
	}

	if len(setClauses) > 0 {
		query := "UPDATE arcs SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
		args = append(args, id)
		if _, err := db.Exec(query, args...); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update arc: " + err.Error()})
			c.Abort()
			return
		}
	}

	if req.FactionIDs != nil {
		db.Exec("DELETE FROM arc_factions WHERE arc_id = ?", id)
		if len(*req.FactionIDs) > 0 {
			factionVals := make([]string, len(*req.FactionIDs))
			factionArgs := make([]interface{}, len(*req.FactionIDs)*2)
			for i, fid := range *req.FactionIDs {
				factionVals[i] = "(?, ?)"
				factionArgs[i*2] = id
				factionArgs[i*2+1] = fid
			}
			db.Exec("INSERT IGNORE INTO arc_factions (arc_id, faction_id) VALUES "+strings.Join(factionVals, ","), factionArgs...)
		}
	}

	if req.GameMasterIDs != nil {
		var prevGMIDs []int
		if gmRows, err := db.Query("SELECT user_id FROM arc_game_masters WHERE arc_id = ?", id); err == nil {
			defer gmRows.Close()
			for gmRows.Next() {
				var uid int
				if gmRows.Scan(&uid) == nil {
					prevGMIDs = append(prevGMIDs, uid)
				}
			}
		}

		db.Exec("DELETE FROM arc_game_masters WHERE arc_id = ?", id)
		if len(*req.GameMasterIDs) > 0 {
			gmVals := make([]string, len(*req.GameMasterIDs))
			gmArgs := make([]interface{}, len(*req.GameMasterIDs)*2)
			for i, uid := range *req.GameMasterIDs {
				gmVals[i] = "(?, ?)"
				gmArgs[i*2] = id
				gmArgs[i*2+1] = uid
			}
			db.Exec("INSERT IGNORE INTO arc_game_masters (arc_id, user_id) VALUES "+strings.Join(gmVals, ","), gmArgs...)
		}

		Events.Publish(db, Events.ArcGMsUpdated, Events.ArcGMsUpdatedEvent{
			ArcID:         id,
			PrevGMUserIDs: prevGMIDs,
			NewGMUserIDs:  *req.GameMasterIDs,
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": "Arc updated successfully"})
}

func DeleteArc(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid arc ID"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	hasPerm, _ := Services.HasPermission(userID, "/arc/delete/:id", db)
	if !hasPerm {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to delete arcs"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM arcs WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete arc: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Arc not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Arc deleted successfully"})
}

func canEditArc(userID, arcID int, db *sql.DB) bool {
	var creatorID sql.NullInt64
	if err := db.QueryRow("SELECT creator_id FROM arcs WHERE id = ?", arcID).Scan(&creatorID); err != nil {
		return false
	}
	if creatorID.Valid && int(creatorID.Int64) == userID {
		return true
	}
	var gmCount int
	db.QueryRow("SELECT COUNT(*) FROM arc_game_masters WHERE arc_id = ? AND user_id = ?", arcID, userID).Scan(&gmCount)
	if gmCount > 0 {
		return true
	}
	hasPerm, _ := Services.HasPermission(userID, "/arc/update/:id", db)
	return hasPerm
}

type ArcEpisodeListItem struct {
	ID             int                       `json:"id"`
	TopicID        int                       `json:"topic_id"`
	Title          string                    `json:"title"`
	Status         int                       `json:"status"`
	LastPostDate   string                    `json:"last_post_date"`
	LastPostAuthor *Entities.ShortUser       `json:"last_post_author"`
	Characters     []Entities.ShortCharacter `json:"characters"`
	Factions       []Entities.FactionShort   `json:"factions"`
}

func GetArcEpisodes(c *gin.Context, db *sql.DB) {
	arcID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid arc ID"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	// Check arc visibility for guests
	var isPublic bool
	if err := db.QueryRow("SELECT is_public FROM arcs WHERE id = ?", arcID).Scan(&isPublic); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Arc not found"})
		c.Abort()
		return
	}
	if userID == 0 && !isPublic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "This arc is not public"})
		c.Abort()
		return
	}

	visibleSubforumIDs, err := Services.GetVisibleSubforums(userID, "subforum_read", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine visible subforums: " + err.Error()})
		c.Abort()
		return
	}
	if len(visibleSubforumIDs) == 0 {
		c.JSON(http.StatusOK, []ArcEpisodeListItem{})
		return
	}

	subforumPH := make([]string, len(visibleSubforumIDs))
	subforumArgs := make([]interface{}, len(visibleSubforumIDs)+2)
	subforumArgs[0] = arcID
	subforumArgs[1] = Entities.DeletedTopic
	for i, id := range visibleSubforumIDs {
		subforumPH[i] = "?"
		subforumArgs[i+2] = id
	}

	rows, err := db.Query(`
		SELECT e.id, e.topic_id, e.name, e.episode_status, t.date_last_post,
		       u.id, u.username
		FROM arc_episodes ae
		JOIN episode_base e ON ae.episode_id = e.id
		JOIN topics t ON e.topic_id = t.id
		LEFT JOIN users u ON t.last_post_author_user_id = u.id
		WHERE ae.arc_id = ? AND t.status != ? AND t.subforum_id IN (`+strings.Join(subforumPH, ",")+`)
		ORDER BY t.date_last_post DESC`,
		subforumArgs...,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch episodes: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	list := []ArcEpisodeListItem{}
	epIndex := map[int]int{}
	for rows.Next() {
		var ep ArcEpisodeListItem
		var lastPostDate sql.NullString
		var authorID sql.NullInt64
		var authorUsername sql.NullString
		if err := rows.Scan(&ep.ID, &ep.TopicID, &ep.Title, &ep.Status, &lastPostDate, &authorID, &authorUsername); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan episode: " + err.Error()})
			c.Abort()
			return
		}
		if lastPostDate.Valid {
			ep.LastPostDate = lastPostDate.String
		}
		if authorID.Valid {
			ep.LastPostAuthor = &Entities.ShortUser{Id: int(authorID.Int64), Username: authorUsername.String}
		}
		ep.Characters = []Entities.ShortCharacter{}
		ep.Factions = []Entities.FactionShort{}
		epIndex[ep.ID] = len(list)
		list = append(list, ep)
	}

	if len(list) == 0 {
		c.JSON(http.StatusOK, list)
		return
	}

	epIDs := make([]interface{}, len(list))
	ph := make([]string, len(list))
	for i, ep := range list {
		epIDs[i] = ep.ID
		ph[i] = "?"
	}
	inClause := strings.Join(ph, ",")

	// Characters
	charRows, err := db.Query(
		"SELECT ec.episode_id, cb.id, cb.name, cb.avatar FROM episode_character ec JOIN character_base cb ON ec.character_id = cb.id WHERE ec.episode_id IN ("+inClause+") ORDER BY cb.name ASC",
		epIDs...,
	)
	if err == nil {
		defer charRows.Close()
		for charRows.Next() {
			var epID int
			var ch Entities.ShortCharacter
			var avatar sql.NullString
			if charRows.Scan(&epID, &ch.Id, &ch.Name, &avatar) == nil {
				if avatar.Valid {
					ch.Avatar = &avatar.String
				}
				list[epIndex[epID]].Characters = append(list[epIndex[epID]].Characters, ch)
			}
		}
	}

	// Factions (distinct per episode, via characters)
	factionRows, err := db.Query(
		`SELECT DISTINCT ec.episode_id, f.id, f.name
		 FROM episode_character ec
		 JOIN character_faction cf ON ec.character_id = cf.character_id
		 JOIN factions f ON cf.faction_id = f.id
		 WHERE ec.episode_id IN (`+inClause+`)`,
		epIDs...,
	)
	if err == nil {
		defer factionRows.Close()
		for factionRows.Next() {
			var epID int
			var f Entities.FactionShort
			if factionRows.Scan(&epID, &f.Id, &f.Name) == nil {
				list[epIndex[epID]].Factions = append(list[epIndex[epID]].Factions, f)
			}
		}
	}

	c.JSON(http.StatusOK, list)
}

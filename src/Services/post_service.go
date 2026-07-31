package Services

import (
	"cuento-backend/src/Entities"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func GetPostById(id int, db *sql.DB, currencyActive bool) (*Entities.Post, error) {
	// 1. Get custom field columns from the config table
	var configJSON string
	err := db.QueryRow("SELECT config FROM custom_field_config WHERE entity_type = 'character_profile'").Scan(&configJSON)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get character profile config: %w", err)
	}

	var customConfig []Entities.CustomFieldConfig
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &customConfig); err != nil {
			return nil, fmt.Errorf("failed to parse character profile config: %w", err)
		}
	}

	var flattenedCols []string
	for _, field := range customConfig {
		flattenedCols = append(flattenedCols, "cpf."+field.MachineFieldName)
	}

	colsSelect := ""
	if len(flattenedCols) > 0 {
		colsSelect = ", " + strings.Join(flattenedCols, ", ")
	}

	currencyJoin := ""
	if currencyActive {
		colsSelect += ", COALESCE(cua.amount, 0) as currency_amount"
		currencyJoin = "LEFT JOIN currency_user_account cua ON p.author_user_id = cua.user_id"
	}

	// 2. Construct the main query
	query := fmt.Sprintf(`
		SELECT
			p.id, p.topic_id, p.author_user_id, p.date_created, p.content, p.use_character_profile,
			u.username, u.avatar, u.total_posts, u.total_general_posts,
			cp.id as character_profile_id, cp.character_id, cb.name as character_name, cp.avatar as character_avatar, cp.mask_name, cp.is_mask, cp.signature as character_signature,
			u.signature as user_signature
			%s
		FROM posts p
		LEFT JOIN users u ON p.author_user_id = u.id
		LEFT JOIN character_profile_base cp ON p.character_profile_id = cp.id
		LEFT JOIN character_base cb ON cp.character_id = cb.id
		LEFT JOIN character_profile_flattened cpf ON cp.id = cpf.entity_id
		%s
		WHERE p.id = ?
	`, colsSelect, currencyJoin)

	// 3. Scan and process results
	rows, err := db.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get post: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, sql.ErrNoRows
	}

	cols, _ := rows.Columns()

	// Find date_created column index to scan it directly into time.Time
	dateCreatedIdx := -1
	for i, col := range cols {
		if col == "date_created" {
			dateCreatedIdx = i
			break
		}
	}

	values := make([]interface{}, len(cols))
	var dateCreated time.Time
	for i := range values {
		if i == dateCreatedIdx {
			values[i] = &dateCreated
		} else {
			values[i] = new(sql.RawBytes)
		}
	}

	if err := rows.Scan(values...); err != nil {
		return nil, fmt.Errorf("failed to scan post data: %w", err)
	}

	rowMap := make(map[string]interface{})
	for i, colName := range cols {
		if i == dateCreatedIdx {
			continue
		}
		val := *(values[i].(*sql.RawBytes))
		if val != nil {
			rowMap[colName] = string(val)
		}
	}

	useProxy := GetUseImageProxy(db)
	domain, _ := GetGlobalSetting("domain", db)

	var post Entities.Post
	if val, ok := rowMap["id"]; ok {
		post.Id, _ = strconv.Atoi(val.(string))
	}
	if val, ok := rowMap["topic_id"]; ok {
		post.TopicId, _ = strconv.Atoi(val.(string))
	}
	if val, ok := rowMap["author_user_id"]; ok {
		post.AuthorUserId, _ = strconv.Atoi(val.(string))
	}
	if val, ok := rowMap["username"]; ok {
		post.AuthorUserName = val.(string)
	}
	post.DateCreated = dateCreated
	if val, ok := rowMap["content"]; ok {
		post.Content = val.(string)
		post.ContentHtml = ApplyImageProxyToHTML(LinkifyURLs(ParseBBCode(post.Content), domain, db), useProxy)
	}
	if val, ok := rowMap["use_character_profile"]; ok {
		post.UseCharacterProfile, _ = strconv.ParseBool(val.(string))
	}

	if post.UseCharacterProfile {
		var charProfile Entities.CharacterProfile
		if id, ok := rowMap["character_profile_id"]; ok {
			charProfile.Id, _ = strconv.Atoi(id.(string))
		}
		if id, ok := rowMap["character_id"]; ok {
			charID, _ := strconv.Atoi(id.(string))
			charProfile.CharacterId = &charID
		}
		if name, ok := rowMap["character_name"]; ok {
			charProfile.CharacterName = name.(string)
		}
		if avatar, ok := rowMap["character_avatar"]; ok {
			avatarStr := avatar.(string)
			charProfile.Avatar = WrapImageURLPtr(&avatarStr, useProxy)
		}
		if maskName, ok := rowMap["mask_name"]; ok {
			maskNameStr := maskName.(string)
			charProfile.MaskName = &maskNameStr
		}
		if isMask, ok := rowMap["is_mask"]; ok {
			isMaskBool, _ := strconv.ParseBool(isMask.(string))
			charProfile.IsMask = &isMaskBool
		}
		if sig, ok := rowMap["character_signature"]; ok {
			sigStr := sig.(string)
			sigHtml := ApplyImageProxyToHTML(LinkifyURLs(ParseBBCode(sigStr), domain, db), useProxy)
			charProfile.Signature = &sigStr
			charProfile.SignatureHtml = &sigHtml
		}

		customFields := make(map[string]Entities.CustomFieldValue)
		for _, field := range customConfig {
			if val, ok := rowMap[field.MachineFieldName]; ok {
				cfValue := Entities.CustomFieldValue{Content: val}
				if field.FieldType == "text" {
					if s, ok := val.(string); ok {
						cfValue.ContentHtml = ApplyImageProxyToHTML(ParseBBCode(s), useProxy)
					}
				} else if useProxy && (field.ContentFieldType == "image" || field.ContentFieldType == "cropped_image") {
					if s, ok := val.(string); ok {
						cfValue.Content = WrapImageURL(s)
					}
				}
				customFields[field.MachineFieldName] = cfValue
			}
		}
		charProfile.CustomFields.CustomFields = customFields
		charProfile.CustomFields.FieldConfig = customConfig
		charProfile.Factions = []Entities.Faction{}
		if charProfile.CharacterId != nil {
			factionRows, err := db.Query(`
				SELECT f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.faction_status
				FROM character_faction cf
				JOIN factions f ON f.id = cf.faction_id
				WHERE f.show_on_profile = true AND cf.character_id = ?
				ORDER BY f.level ASC
			`, *charProfile.CharacterId)
			if err == nil {
				defer factionRows.Close()
				for factionRows.Next() {
					var f Entities.Faction
					if err := factionRows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.FactionStatus); err == nil {
						charProfile.Factions = append(charProfile.Factions, f)
					}
				}
				FillFactionSettingNames(charProfile.Factions, db)
			}
		}
		post.CharacterProfile = &charProfile
	} else {
		var userProfile Entities.UserProfile
		userProfile.UserId = post.AuthorUserId
		if username, ok := rowMap["username"]; ok {
			userProfile.UserName = username.(string)
		}
		if avatar, ok := rowMap["avatar"]; ok {
			avatarStr := avatar.(string)
			if useProxy && avatarStr != "" {
				avatarStr = WrapImageURL(avatarStr)
			}
			userProfile.Avatar = avatarStr
		}
		if v, ok := rowMap["total_posts"]; ok {
			userProfile.TotalPosts, _ = strconv.Atoi(v.(string))
		}
		if v, ok := rowMap["total_general_posts"]; ok {
			userProfile.TotalGeneralPosts, _ = strconv.Atoi(v.(string))
		}
		if v, ok := rowMap["currency_amount"]; ok {
			amount, _ := strconv.Atoi(v.(string))
			userProfile.CurrencyAmount = &amount
		}
		if sig, ok := rowMap["user_signature"]; ok {
			sigStr := sig.(string)
			sigHtml := ApplyImageProxyToHTML(LinkifyURLs(ParseBBCode(sigStr), domain, db), useProxy)
			userProfile.Signature = &sigStr
			userProfile.SignatureHtml = &sigHtml
		}
		post.UserProfile = &userProfile
	}

	return &post, nil
}

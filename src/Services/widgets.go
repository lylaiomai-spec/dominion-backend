package Services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type WidgetProvider struct {
	Name string
	Fn   func(map[string]interface{}, *sql.DB) (string, error)
}

type conditionalWidget struct {
	WidgetProvider
	featureKey string
}

var builtinWidgets = []WidgetProvider{
	{"WidgetLastPost", WidgetLastPost},
	{"WidgetRandomEntities", WidgetRandomEntities},
}

var featureWidgets []conditionalWidget

func RegisterFeatureWidget(featureKey string, name string, fn func(map[string]interface{}, *sql.DB) (string, error)) {
	featureWidgets = append(featureWidgets, conditionalWidget{
		WidgetProvider: WidgetProvider{name, fn},
		featureKey:     featureKey,
	})
}

func GetWidgets(db *sql.DB) []WidgetProvider {
	result := make([]WidgetProvider, len(builtinWidgets))
	copy(result, builtinWidgets)
	for _, w := range featureWidgets {
		var isActive bool
		if err := db.QueryRow("SELECT is_active FROM features WHERE `key` = ?", w.featureKey).Scan(&isActive); err == nil && isActive {
			result = append(result, w.WidgetProvider)
		}
	}
	return result
}

func WidgetLastPost(config map[string]interface{}, db *sql.DB) (string, error) {
	topicID, err := extractIntValue(config, "topic_id")
	if err != nil {
		return "", err
	}

	var content string
	err = db.QueryRow(
		"SELECT content FROM posts WHERE topic_id = ? AND (is_deleted IS NULL OR is_deleted <> 1) ORDER BY date_created DESC LIMIT 1",
		topicID,
	).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to get last post: %w", err)
	}

	return ParseBBCode(content), nil
}

// RenderWidget fetches a widget by ID, resolves its type, and calls the registered function.
func RenderWidget(id int, db *sql.DB) (string, error) {
	var templateID int
	var configJSON sql.NullString
	err := db.QueryRow("SELECT template_id, config FROM widgets WHERE id = ?", id).Scan(&templateID, &configJSON)
	if err != nil {
		return "", fmt.Errorf("widget %d not found: %w", id, err)
	}

	var funcName string
	var configTemplateJSON sql.NullString
	err = db.QueryRow("SELECT func, config_template FROM widget_types WHERE id = ?", templateID).Scan(&funcName, &configTemplateJSON)
	if err != nil {
		return "", fmt.Errorf("widget type %d not found: %w", templateID, err)
	}

	var fn func(map[string]interface{}, *sql.DB) (string, error)
	for _, w := range GetWidgets(db) {
		if w.Name == funcName {
			fn = w.Fn
			break
		}
	}
	if fn == nil {
		return "", fmt.Errorf("unknown widget function: %s", funcName)
	}

	config := make(map[string]interface{})
	if configJSON.Valid && configJSON.String != "" {
		if err := json.Unmarshal([]byte(configJSON.String), &config); err != nil {
			return "", fmt.Errorf("invalid widget config: %w", err)
		}
	}
	config["_widget_id"] = id

	// For fields marked can_empty in the template, inject a zero default if missing from config.
	if configTemplateJSON.Valid && configTemplateJSON.String != "" {
		var rawTmpl map[string]interface{}
		if err := json.Unmarshal([]byte(configTemplateJSON.String), &rawTmpl); err == nil {
			for fieldName, rawMeta := range rawTmpl {
				if strings.HasPrefix(fieldName, "_") {
					continue
				}
				fieldMeta, ok := rawMeta.(map[string]interface{})
				if !ok {
					continue
				}
				if canEmpty, ok := fieldMeta["can_empty"].(bool); ok && canEmpty {
					if _, exists := config[fieldName]; !exists {
						switch fieldMeta["type"] {
						case "int":
							config[fieldName] = 0
						default:
							config[fieldName] = ""
						}
					}
				}
			}
		}
	}

	return fn(config, db)
}

var widgetTagRe = regexp.MustCompile(`\[widget=(\d+)\]`)

// RenderPanelContent parses BB code first, then replaces [widget=N] tags with rendered widget HTML.
func RenderPanelContent(content string, db *sql.DB) string {
	html := ParseBBCode(content)

	return widgetTagRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := widgetTagRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		id, _ := strconv.Atoi(sub[1])
		rendered, err := RenderWidget(id, db)
		if err != nil {
			return fmt.Sprintf("<!-- widget %d error: %s -->", id, err.Error())
		}
		return rendered
	})
}

var safeFieldName = regexp.MustCompile(`^[a-z0-9_]+$`)

func WidgetRandomEntities(config map[string]interface{}, db *sql.DB) (string, error) {
	widgetID, _ := config["_widget_id"].(int)

	number, err := extractIntValue(config, "number")
	if err != nil {
		return "", err
	}

	interval := 0
	if v, err := extractIntValue(config, "interval"); err == nil {
		interval = v
	}

	entityType, err := extractStringValue(config, "entity_type")
	if err != nil {
		return "", err
	}
	if entityType != "character" && entityType != "wanted_character" {
		return "", fmt.Errorf("unsupported entity_type: %s", entityType)
	}

	type fieldSize struct{ width, height int }
	fieldSizes := make(map[string]fieldSize)

	var configFields []string
	for i, key := range []string{"entity_field_1", "entity_field_2"} {
		if v, err := extractStringValue(config, key); err == nil && v != "" && safeFieldName.MatchString(v) {
			configFields = append(configFields, v)
			w, _ := extractIntValue(config, fmt.Sprintf("entity_field_%d_width", i+1))
			h, _ := extractIntValue(config, fmt.Sprintf("entity_field_%d_height", i+1))
			if w > 0 || h > 0 {
				fieldSizes[v] = fieldSize{width: w, height: h}
			}
		}
	}

	knownColumns := func(table string) map[string]bool {
		m := make(map[string]bool)
		r, err := db.Query("SELECT column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?", table)
		if err != nil {
			return m
		}
		defer r.Close()
		for r.Next() {
			var col string
			if r.Scan(&col) == nil {
				m[col] = true
			}
		}
		return m
	}
	baseColumns := knownColumns(entityType + "_base")
	flattenedColumns := knownColumns(entityType + "_flattened")

	var filterClauses []string
	var filterArgs []interface{}
	needsFlattened := false
	if rawFilters, ok := config["filters"]; ok && rawFilters != nil {
		filtersMap, ok := rawFilters.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("invalid filters format")
		}
		for fieldName, rawVals := range filtersMap {
			if !safeFieldName.MatchString(fieldName) {
				return "", fmt.Errorf("invalid filter field name: %s", fieldName)
			}
			var vals []interface{}
			switch v := rawVals.(type) {
			case []interface{}:
				vals = v
			case string:
				vals = []interface{}{v}
			case float64:
				vals = []interface{}{v}
			}
			if len(vals) == 0 {
				continue
			}
			placeholders := strings.Repeat("?,", len(vals))
			placeholders = placeholders[:len(placeholders)-1]
			var clause string
			if fieldName == "status_active" {
				clause = fmt.Sprintf("b.%s_status IN (%s)", entityType, placeholders)
			} else if baseColumns[fieldName] {
				clause = fmt.Sprintf("b.%s IN (%s)", fieldName, placeholders)
			} else if flattenedColumns[fieldName] {
				clause = fmt.Sprintf("f.%s IN (%s)", fieldName, placeholders)
				needsFlattened = true
			} else {
				continue // field doesn't exist in either table — skip
			}
			filterClauses = append(filterClauses, clause)
			filterArgs = append(filterArgs, vals...)
		}
	}

	whereClause := "1=1"
	if len(filterClauses) > 0 {
		whereClause += " AND " + strings.Join(filterClauses, " AND ")
	}

	fromClause := fmt.Sprintf("%s_base b", entityType)
	if needsFlattened {
		fromClause += fmt.Sprintf(" JOIN %s_flattened f ON b.id = f.entity_id", entityType)
	}

	query := fmt.Sprintf(
		"SELECT b.id, b.topic_id, b.name FROM %s WHERE %s ORDER BY RAND()",
		fromClause, whereClause,
	)

	rows, err := db.Query(query, filterArgs...)
	if err != nil {
		return "", fmt.Errorf("failed to query random entities: %w", err)
	}
	defer rows.Close()

	useTopicId := entityType == "wanted_character"

	type customField struct {
		FieldName  string      `json:"field_name"`
		Value      interface{} `json:"value"`
		RenderType string      `json:"render_type"`
	}
	type entityItem struct {
		Id           int           `json:"id"`
		Type         string        `json:"type"`
		Name         string        `json:"name"`
		CustomFields []customField `json:"custom_fields,omitempty"`
	}
	type entityRaw struct {
		rawId int // base table id for custom field lookup
		entityItem
	}

	var allRaw []entityRaw
	for rows.Next() {
		var id int
		var topicId sql.NullInt64
		var name string
		if err := rows.Scan(&id, &topicId, &name); err != nil {
			continue
		}
		displayId := id
		if useTopicId && topicId.Valid {
			displayId = int(topicId.Int64)
		}
		allRaw = append(allRaw, entityRaw{rawId: id, entityItem: entityItem{Id: displayId, Type: entityType, Name: name}})
	}

	// Fetch custom fields for all entities in one query
	if len(allRaw) > 0 && len(configFields) > 0 {
		useProxy := GetUseImageProxy(db)

		fieldRenderType := make(map[string]string)
		if fieldConfigs, err := GetFieldConfig(entityType, db); err == nil {
			for _, fc := range fieldConfigs {
				fieldRenderType[fc.MachineFieldName] = fc.ContentFieldType
			}
		}

		idPlaceholders := strings.Repeat("?,", len(allRaw))
		idPlaceholders = idPlaceholders[:len(idPlaceholders)-1]
		idArgs := make([]interface{}, len(allRaw))
		for i, r := range allRaw {
			idArgs[i] = r.rawId
		}

		fieldPlaceholders := strings.Repeat("?,", len(configFields))
		fieldPlaceholders = fieldPlaceholders[:len(fieldPlaceholders)-1]
		fieldArgs := make([]interface{}, len(configFields))
		for i, f := range configFields {
			fieldArgs[i] = f
		}

		cfRows, cfErr := db.Query(
			fmt.Sprintf("SELECT entity_id, field_machine_name, field_type, value_int, value_string, value_text FROM %s_main WHERE entity_id IN (%s) AND field_machine_name IN (%s)", entityType, idPlaceholders, fieldPlaceholders),
			append(idArgs, fieldArgs...)...,
		)
		if cfErr == nil {
			defer cfRows.Close()
			fieldsByEntity := make(map[int][]customField)
			for cfRows.Next() {
				var entityID int
				var fieldName, fieldType string
				var valueInt sql.NullInt64
				var valueString, valueText sql.NullString
				if err := cfRows.Scan(&entityID, &fieldName, &fieldType, &valueInt, &valueString, &valueText); err != nil {
					continue
				}
				var value interface{}
				switch fieldType {
				case "int", "select":
					if valueInt.Valid {
						value = valueInt.Int64
					}
				case "text":
					if valueText.Valid {
						value = valueText.String
					}
				default:
					if valueString.Valid {
						value = valueString.String
					}
				}
				if value == nil {
					continue
				}
				renderType := fieldRenderType[fieldName]
				if useProxy && (renderType == "image" || renderType == "cropped_image") {
					if s, ok := value.(string); ok {
						proxyURL := WrapImageURL(s)
						if dims, ok := fieldSizes[fieldName]; ok {
							proxyURL += fmt.Sprintf("&size=%dx%d", dims.width, dims.height)
						}
						value = proxyURL
					}
				}
				fieldsByEntity[entityID] = append(fieldsByEntity[entityID], customField{
					FieldName:  fieldName,
					Value:      value,
					RenderType: renderType,
				})
			}
			for i := range allRaw {
				if fields, ok := fieldsByEntity[allRaw[i].rawId]; ok {
					allRaw[i].CustomFields = fields
				}
			}
		}
	}

	allItems := make([]entityItem, len(allRaw))
	for i, r := range allRaw {
		allItems[i] = r.entityItem
	}

	var sets [][]entityItem
	if number > 0 {
		for i := 0; i < len(allItems); i += number {
			end := i + number
			if end > len(allItems) {
				end = len(allItems)
			}
			sets = append(sets, allItems[i:end])
		}
	}
	if sets == nil {
		sets = [][]entityItem{}
	}

	type widgetData struct {
		Interval int            `json:"interval"`
		Sets     [][]entityItem `json:"sets"`
	}
	dataJSON, err := json.Marshal(widgetData{Interval: interval, Sets: sets})
	if err != nil {
		return "", fmt.Errorf("failed to marshal widget data: %w", err)
	}

	comment := fmt.Sprintf("<!-- widget:%d:%s -->", widgetID, string(dataJSON))
	placeholder := fmt.Sprintf(`<div data-widget-id="%d" widget-type="random_entity"></div>`, widgetID)

	return comment + "\n" + placeholder, nil
}

func extractIntValue(config map[string]interface{}, key string) (int, error) {
	raw, ok := config[key]
	if !ok {
		return 0, fmt.Errorf("missing %s in config", key)
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	}
	return 0, fmt.Errorf("missing or invalid %s value in config", key)
}

func extractStringValue(config map[string]interface{}, key string) (string, error) {
	raw, ok := config[key]
	if !ok {
		return "", fmt.Errorf("missing %s in config", key)
	}
	if v, ok := raw.(string); ok {
		return v, nil
	}
	return "", fmt.Errorf("missing or invalid %s value in config", key)
}

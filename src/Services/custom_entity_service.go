package Services

import (
	"cuento-backend/src/Entities"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func ResolveSelectField(val interface{}, options map[string]string) map[string]interface{} {
	var id int
	switch v := val.(type) {
	case float64:
		id = int(v)
	case int:
		id = v
	}
	return map[string]interface{}{"id": id, "value": options[strconv.Itoa(id)]}
}

type BaseEntity interface {
	GetBaseFields() []string
}

type DBExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Prepare(query string) (*sql.Stmt, error)
}

func IdentifyBaseEntity(className string) (interface{}, error) {
	var entity interface{}
	switch className {
	case "character":
		entity = &Entities.Character{}
	case "character_profile":
		entity = &Entities.CharacterProfile{}
	case "episode":
		entity = &Entities.Episode{}
	case "wanted_character":
		entity = &Entities.WantedCharacter{}
	default:
		return nil, fmt.Errorf("unknown entity class: %s", className)
	}
	return entity, nil
}

func ToSnakeCase(str string) string {
	var res strings.Builder
	for i, r := range str {
		if i > 0 && unicode.IsUpper(r) {
			if str[i-1] != '_' {
				res.WriteByte('_')
			}
		}
		res.WriteRune(unicode.ToLower(r))
	}
	return res.String()
}

func GetEntity(id int64, className string, db DBExecutor) (interface{}, error) {
	// Basic validation
	for _, r := range className {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return nil, fmt.Errorf("invalid class name")
		}
	}

	// Fetch Config
	var configBytes []byte
	err := db.QueryRow(fmt.Sprintf("SELECT config FROM custom_field_config WHERE entity_type = '%s'", className)).Scan(&configBytes)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no configuration found for entity type %s", className)
		}
		return nil, err
	}

	config := make([]Entities.CustomFieldConfig, 0)
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &config); err != nil {
			return nil, err
		}
	}

	// 1. Fetch data as map
	query := fmt.Sprintf("SELECT * FROM %s_base LEFT JOIN %s_flattened ON %s_base.id = %s_flattened.entity_id WHERE %s_base.id = ?", className, className, className, className, className)

	rows, err := db.Query(query, id)
	if err != nil {
		return nil, err
	}

	if !rows.Next() {
		rows.Close()
		return nil, sql.ErrNoRows
	}

	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}

	vals := make([]interface{}, len(cols))
	for i := range vals {
		vals[i] = new(sql.RawBytes)
	}

	if err := rows.Scan(vals...); err != nil {
		rows.Close()
		return nil, err
	}

	data := make(map[string]interface{})
	for i, colName := range cols {
		val := vals[i].(*sql.RawBytes)
		if *val == nil {
			continue
		}
		var v interface{}
		if err := json.Unmarshal(*val, &v); err == nil {
			data[colName] = v
		} else {
			data[colName] = string(*val)
		}
	}
	rows.Close() // close before issuing further queries on the same connection

	// 2. Instantiate struct
	var entity, er = IdentifyBaseEntity(className)
	if er != nil {
		return nil, er
	}

	useProxy := GetUseImageProxy(db)

	// 3. Fill struct
	if err := fillEntity(entity, data, config, useProxy); err != nil {
		return nil, err
	}

	return entity, nil
}

func fillEntity(entity interface{}, data map[string]interface{}, config []Entities.CustomFieldConfig, useProxy bool) error {
	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	usedKeys := make(map[string]bool)

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		fieldName := fieldType.Name

		// Use ToSnakeCase for mapping: struct field "TopicId" -> db column "topic_id"
		dbKey := ToSnakeCase(fieldName)

		if val, ok := data[dbKey]; ok {
			usedKeys[dbKey] = true
			if err := setField(field, val); err != nil {
				return fmt.Errorf("failed to set field %s: %w", fieldName, err)
			}
		}
	}

	// Handle CustomFields
	// Look for a field of type CustomFieldEntity
	cfField := v.FieldByName("CustomFields")
	if cfField.IsValid() && cfField.Type() == reflect.TypeOf(Entities.CustomFieldEntity{}) {
		cfMap := make(map[string]Entities.CustomFieldValue)

		configMap := make(map[string]Entities.CustomFieldConfig)
		sortColumns := make(map[string]bool)
		for _, c := range config {
			configMap[c.MachineFieldName] = c
			if c.FieldType == "free_format_date" {
				sortColumns[c.MachineFieldName+"_sort"] = true
			}
		}

		for key, val := range data {
			if !usedKeys[key] && key != "entity_id" && !sortColumns[key] {
				cfValue := Entities.CustomFieldValue{Content: val}
				if conf, ok := configMap[key]; ok {
					if conf.FieldType == "text" {
						if s, ok := val.(string); ok {
							cfValue.ContentHtml = ApplyImageProxyToHTML(ParseBBCode(s), useProxy)
						}
					} else if useProxy && (conf.ContentFieldType == "image" || conf.ContentFieldType == "cropped_image") {
						if s, ok := val.(string); ok {
							cfValue.Content = WrapImageURL(s)
						}
					} else if conf.FieldType == "select" {
						if conf.Options != nil {
							cfValue.Content = ResolveSelectField(val, conf.Options)
						}
					} else if conf.FieldType == "free_format_date" {
						if m, ok := val.(map[string]interface{}); ok {
							cfValue.Data = m
							if fs, ok := m["formatted_string"].(string); fs != "" && ok {
								cfValue.Content = fs
							} else if fs, ok := m["format_string"].(string); ok {
								rendered := fs
								if placeholders, ok := m["placeholders"].(map[string]interface{}); ok {
									for k, v := range placeholders {
										rendered = strings.ReplaceAll(rendered, "{"+k+"}", fmt.Sprintf("%v", v))
									}
								}
								cfValue.Content = rendered
							}
						}
					}
				}
				cfMap[key] = cfValue
			}
		}

		// Set the CustomFields map in the CustomFieldEntity struct
		cfMapField := cfField.FieldByName("CustomFields")
		if cfMapField.IsValid() && cfMapField.CanSet() {
			cfMapField.Set(reflect.ValueOf(cfMap))
		}

		cfConfigField := cfField.FieldByName("FieldConfig")
		if cfConfigField.IsValid() && cfConfigField.CanSet() {
			cfConfigField.Set(reflect.ValueOf(config))
		}
	}

	return nil
}

func setField(field reflect.Value, val interface{}) error {
	if !field.CanSet() {
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		if s, ok := val.(string); ok {
			field.SetString(s)
		} else {
			field.SetString(fmt.Sprintf("%v", val))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var i int64
		switch v := val.(type) {
		case float64:
			i = int64(v)
		case string:
			n, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				i = n
			}
		case int:
			i = int64(v)
		}
		field.SetInt(i)
	case reflect.Ptr:
		if val == nil {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		elem := reflect.New(field.Type().Elem())
		if err := setField(elem.Elem(), val); err != nil {
			return err
		}
		field.Set(elem)
	case reflect.Float32, reflect.Float64:
		var f float64
		switch v := val.(type) {
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err == nil {
				f = n
			}
		case float64:
			f = v
		case int:
			f = float64(v)
		}
		field.SetFloat(f)
	case reflect.Bool:
		var b bool
		switch v := val.(type) {
		case string:
			b, _ = strconv.ParseBool(v)
		case bool:
			b = v
		case int:
			b = v != 0
		case float64:
			b = v != 0
		}
		field.SetBool(b)
	default:
		if reflect.TypeOf(val).AssignableTo(field.Type()) {
			field.Set(reflect.ValueOf(val))
		}
	}
	return nil
}

func getColumnTypes(className string, db DBExecutor) (map[string]string, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT * FROM %s_flattened WHERE 1=0", className))
	if err != nil {
		return nil, fmt.Errorf("failed to query custom fields metadata: %w", err)
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	colTypeMap := make(map[string]string)
	for _, ct := range colTypes {
		colTypeMap[ct.Name()] = ct.DatabaseTypeName()
	}
	return colTypeMap, nil
}

func nextPowerOf10(n int64) int64 {
	p := int64(1)
	for p < n {
		p *= 10
	}
	return p
}

// computeFreeFormatDateSort fetches the free_format_date template and computes
// a sortable integer. Each placeholder occupies a fixed power-of-10 "slot" wide enough
// to hold all its possible values, so the most-significant position always dominates.
func computeFreeFormatDateSort(freeFormatDateId *int, placeholders map[string]interface{}, db DBExecutor) int64 {
	if freeFormatDateId == nil || len(placeholders) == 0 {
		return 0
	}

	var ffdJSON string
	err := db.QueryRow(`SELECT free_format_date FROM free_format_date_settings WHERE id = ?`, *freeFormatDateId).Scan(&ffdJSON)
	if err != nil || ffdJSON == "" {
		return 0
	}

	var template Entities.FreeFormatDate
	if err := json.Unmarshal([]byte(ffdJSON), &template); err != nil {
		return 0
	}

	sorted := make([]Entities.FreeFormatDatePlaceholder, len(template.Placeholders))
	copy(sorted, template.Placeholders)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Position < sorted[j].Position
	})

	// Compute the cardinality of each placeholder, then round up to the next power of 10
	// so each position occupies a clean digit group in the final integer.
	slotSizes := make([]int64, len(sorted))
	for i, p := range sorted {
		var cardinality int64
		if p.Type == Entities.FreeFormatDatePlaceholderTypeList {
			cardinality = int64(len(p.ValueList))
		} else {
			min := 0
			if p.MinValue != nil {
				min = *p.MinValue
			}
			max := min + 999
			if p.MaxValue != nil {
				max = *p.MaxValue
			}
			cardinality = int64(max-min) + 1
		}
		slotSizes[i] = nextPowerOf10(cardinality)
	}

	var sortValue int64
	for i, p := range sorted {
		var val int64
		raw := placeholders[p.Name]
		if p.Type == Entities.FreeFormatDatePlaceholderTypeList {
			if s, ok := raw.(string); ok {
				for idx, v := range p.ValueList {
					if v == s {
						val = int64(idx) + 1
						break
					}
				}
			}
		} else if p.IsHiddenNegative {
			// User inputs positive values [min,max] but sorting treats the range as [-max,-min].
			// Mapping: input v → normalized = v - min (null → max, placing it after all valid values).
			max := int64(0)
			if p.MaxValue != nil {
				max = int64(*p.MaxValue)
			}
			min := int64(0)
			if p.MinValue != nil {
				min = int64(*p.MinValue)
			}
			if f, ok := raw.(float64); ok {
				val = int64(f) - min
			} else if raw == nil {
				val = max
			}
		} else {
			min := int64(0)
			if p.MinValue != nil {
				min = int64(*p.MinValue)
			}
			if f, ok := raw.(float64); ok {
				if min < 0 {
					val = int64(f) - min
				} else {
					val = int64(f)
				}
			} else if raw == nil && min < 0 {
				val = -min
			}
		}
		// Weight = product of slot sizes for all less-significant positions
		weight := int64(1)
		for j := i + 1; j < len(slotSizes); j++ {
			weight *= slotSizes[j]
		}
		sortValue += val * weight
	}

	return sortValue
}

// buildFreeFormatDateStoredValue converts the incoming {faction_id, format_string, placeholders}
// payload into a FreeFormatDateFieldValue and computes its sort value.
func buildFreeFormatDateStoredValue(raw map[string]interface{}, entityId int64, entityType string, db DBExecutor) (jsonStr string, sortVal int64, err error) {
	formatString, _ := raw["format_string"].(string)

	placeholders, _ := raw["placeholders"].(map[string]interface{})

	var freeFormatDateId *int
	if fid := raw["free_format_date_id"]; fid != nil {
		if f, ok := fid.(float64); ok {
			i := int(f)
			freeFormatDateId = &i
		}
	}

	sortVal = computeFreeFormatDateSort(freeFormatDateId, placeholders, db)

	formattedString := formatString
	if placeholders != nil {
		for k, v := range placeholders {
			formattedString = strings.ReplaceAll(formattedString, "{"+k+"}", fmt.Sprintf("%v", v))
		}
	}

	stored := Entities.FreeFormatDateFieldValue{
		EntityId:         int(entityId),
		EntityType:       entityType,
		FreeFormatDateId: freeFormatDateId,
		FormatString:     formatString,
		FormattedString:  formattedString,
		Placeholders:     placeholders,
		SortValue:        sortVal,
	}

	jsonBytes, err := json.Marshal(stored)
	if err != nil {
		return "", 0, err
	}
	return string(jsonBytes), sortVal, nil
}

func CreateEntity(className string, entity interface{}, db DBExecutor) (interface{}, int64, error) {
	// Basic validation
	for _, r := range className {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return nil, 0, fmt.Errorf("invalid class name")
		}
	}

	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()

	// Determine allowed fields from BaseEntity interface
	var allowedFields map[string]bool
	if baseEntity, ok := entity.(BaseEntity); ok {
		allowedFields = make(map[string]bool)
		for _, f := range baseEntity.GetBaseFields() {
			allowedFields[strings.ToLower(f)] = true
		}
	}

	// 1. Insert into the base table
	var cols []string
	var vals []interface{}
	var placeholders []string

	if allowedFields == nil {
		return nil, 0, fmt.Errorf("entity does not implement BaseEntity interface")
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		fieldName := fieldType.Name

		snakeName := ToSnakeCase(fieldName)
		if !allowedFields[snakeName] {
			continue
		}

		cols = append(cols, snakeName)
		fieldVal := field.Interface()
		if fv := reflect.ValueOf(fieldVal); fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				fieldVal = nil
			} else {
				fieldVal = fv.Elem().Interface()
			}
		}
		vals = append(vals, fieldVal)
		placeholders = append(placeholders, "?")
	}

	var id int64
	if len(cols) > 0 {
		query := fmt.Sprintf("INSERT INTO %s_base (%s) VALUES (%s)", className, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
		res, err := db.Exec(query, vals...)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to insert base entity: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get insert id: %w", err)
		}

		// Set Id back to struct
		idField := v.FieldByName("Id")
		if idField.IsValid() && idField.CanSet() {
			idField.SetInt(id)
		}
	} else {
		return nil, 0, fmt.Errorf("no base fields to insert")
	}

	// 2. Insert custom fields
	cfField := v.FieldByName("CustomFields")
	if cfField.IsValid() {
		cfMapField := cfField.FieldByName("CustomFields")
		if cfMapField.IsValid() && cfMapField.Kind() == reflect.Map && cfMapField.Len() > 0 {
			colTypeMap, err := getColumnTypes(className, db)
			if err != nil {
				return nil, 0, err
			}

			fieldConfigs, _ := GetFieldConfig(className, db)
			fieldConfigMap := make(map[string]Entities.CustomFieldConfig)
			for _, fc := range fieldConfigs {
				fieldConfigMap[fc.MachineFieldName] = fc
			}

			insertQuery := fmt.Sprintf("INSERT INTO %s_main (entity_id, field_machine_name, field_type, value_int, value_decimal, value_string, value_text, value_date, value_free_formatted_date, sort_free_formatted_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", className)

			iter := cfMapField.MapRange()
			for iter.Next() {
				fieldName := iter.Key().String()
				fieldValueRaw := iter.Value().Interface()

				var fieldValue interface{}
				var sortValue interface{}
				if cfVal, ok := fieldValueRaw.(Entities.CustomFieldValue); ok {
					fieldValue = cfVal.Content
					if cfVal.Sort != nil {
						sortValue = float64(*cfVal.Sort)
					}
					if contentMap, isMap := fieldValue.(map[string]interface{}); isMap {
						if sv, hasSort := contentMap["sort"]; hasSort && sortValue == nil {
							sortValue = sv
						}
						if content, hasContent := contentMap["content"]; hasContent {
							fieldValue = content
						}
					}
				} else {
					fieldValue = fieldValueRaw
				}

				var fieldType string
				var valInt *int
				var valDecimal *float64
				var valString *string
				var valText *string
				var valDate *string
				var valFreeFormattedDate *string
				var valSortFreeFormattedDate *int64

				if fc, isFreeFormat := fieldConfigMap[fieldName]; isFreeFormat && fc.FieldType == "free_format_date" {
					fieldType = "free_format_date"
					if m, ok := fieldValue.(map[string]interface{}); ok {
						if s, sv, buildErr := buildFreeFormatDateStoredValue(m, id, className, db); buildErr == nil {
							valFreeFormattedDate = &s
							valSortFreeFormattedDate = &sv
						}
					}
				} else {
					dbType, ok := colTypeMap[fieldName]
					if !ok {
						continue
					}
					switch dbType {
					case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT":
						fieldType = "int"
						if v, ok := fieldValue.(float64); ok {
							i := int(v)
							valInt = &i
						} else if v, ok := fieldValue.(int); ok {
							valInt = &v
						} else if v, ok := fieldValue.(string); ok {
							if i, err := strconv.Atoi(v); err == nil {
								valInt = &i
							}
						}
					case "DECIMAL", "FLOAT", "DOUBLE":
						fieldType = "decimal"
						if v, ok := fieldValue.(float64); ok {
							valDecimal = &v
						}
					case "VARCHAR", "CHAR":
						fieldType = "string"
						if v, ok := fieldValue.(string); ok {
							valString = &v
						}
					case "TEXT", "BLOB":
						fieldType = "text"
						if v, ok := fieldValue.(string); ok {
							valText = &v
						}
					case "DATETIME", "DATE", "TIMESTAMP":
						fieldType = "date"
						if v, ok := fieldValue.(string); ok {
							valDate = &v
						}
					case "JSON":
						fieldType = "free_format_date"
						if m, ok := fieldValue.(map[string]interface{}); ok {
							jsonBytes, _ := json.Marshal(m)
							s := string(jsonBytes)
							valFreeFormattedDate = &s
							if sv, ok := sortValue.(float64); ok {
								i := int64(sv)
								valSortFreeFormattedDate = &i
							}
						}
					default:
						fieldType = "string"
						if v, ok := fieldValue.(string); ok {
							valString = &v
						}
					}
				}

				_, err := db.Exec(insertQuery, id, fieldName, fieldType, valInt, valDecimal, valString, valText, valDate, valFreeFormattedDate, valSortFreeFormattedDate)
				if err != nil {
					return nil, 0, fmt.Errorf("failed to insert custom field %s: %w", fieldName, err)
				}
			}
		}
	}

	createdEntity, err := GetEntity(id, className, db)
	return createdEntity, id, err
}

func GetFieldConfig(entityType string, db DBExecutor) ([]Entities.CustomFieldConfig, error) {
	var configBytes []byte
	err := db.QueryRow("SELECT config FROM custom_field_config WHERE entity_type = ?", entityType).Scan(&configBytes)
	if err != nil {
		if err == sql.ErrNoRows {
			return []Entities.CustomFieldConfig{}, nil
		}
		return nil, err
	}
	config := make([]Entities.CustomFieldConfig, 0)
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &config); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func PatchEntity(id int64, className string, updates map[string]interface{}, db DBExecutor) (interface{}, error) {
	// Basic validation
	for _, r := range className {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return nil, fmt.Errorf("invalid class name")
		}
	}

	// 1. Identify base fields
	var entity, err = IdentifyBaseEntity(className)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(entity).Elem()
	t := v.Type()

	baseFieldNames := make(map[string]bool)
	for i := 0; i < v.NumField(); i++ {
		fieldName := t.Field(i).Name
		if fieldName != "Id" && fieldName != "CustomFields" {
			baseFieldNames[ToSnakeCase(fieldName)] = true
		}
	}

	// 2. Prepare base update
	var baseUpdates []string
	var baseArgs []interface{}

	for key, val := range updates {
		lowerKey := strings.ToLower(key)
		if baseFieldNames[lowerKey] {
			baseUpdates = append(baseUpdates, fmt.Sprintf("%s = ?", lowerKey))
			baseArgs = append(baseArgs, val)
		}
	}

	if len(baseUpdates) > 0 {
		query := fmt.Sprintf("UPDATE %s_base SET %s WHERE id = ?", className, strings.Join(baseUpdates, ", "))
		baseArgs = append(baseArgs, id)
		if _, err := db.Exec(query, baseArgs...); err != nil {
			return nil, fmt.Errorf("failed to update base entity: %w", err)
		}
	}

	// 3. Update custom fields
	if cfVal, ok := updates["custom_fields"]; ok {
		var fieldsMap map[string]interface{}

		// The incoming custom_fields payload is expected to be a map[string]interface{}
		// where each value is either a primitive type or a map[string]interface{} with a "content" key.
		if fMap, isMap := cfVal.(map[string]interface{}); isMap {
			fieldsMap = fMap
		} else {
			// If cfVal is not a map, it's an unexpected format for custom_fields.
			// This might indicate an error in the request payload or a misunderstanding of the format.
			return nil, fmt.Errorf("custom_fields payload is not a map: %T", cfVal)
		}

		if len(fieldsMap) > 0 {
			colTypeMap, err := getColumnTypes(className, db)
			if err != nil {
				return nil, err
			}

			fieldConfigs, _ := GetFieldConfig(className, db)
			fieldConfigMap := make(map[string]Entities.CustomFieldConfig)
			for _, fc := range fieldConfigs {
				fieldConfigMap[fc.MachineFieldName] = fc
			}

			for fieldName, fieldValueRaw := range fieldsMap {
				if fieldName == "" {
					continue
				}

				var actualFieldValue interface{}
				var actualSortValue interface{}
				if contentMap, isContentMap := fieldValueRaw.(map[string]interface{}); isContentMap {
					if sv, hasSort := contentMap["sort"]; hasSort {
						actualSortValue = sv
					}
					if content, hasContent := contentMap["content"]; hasContent {
						actualFieldValue = content
					} else {
						actualFieldValue = fieldValueRaw
					}
				} else {
					actualFieldValue = fieldValueRaw
				}

				var fieldType string
				var valInt *int
				var valDecimal *float64
				var valString *string
				var valText *string
				var valDate *string
				var valFreeFormattedDate *string
				var valSortFreeFormattedDate *int64

				if fc, isFreeFormat := fieldConfigMap[fieldName]; isFreeFormat && fc.FieldType == "free_format_date" {
					fieldType = "free_format_date"
					if m, ok := actualFieldValue.(map[string]interface{}); ok {
						if s, sv, buildErr := buildFreeFormatDateStoredValue(m, id, className, db); buildErr == nil {
							valFreeFormattedDate = &s
							valSortFreeFormattedDate = &sv
						}
					}
				} else {
					dbType, ok := colTypeMap[fieldName]
					if !ok {
						continue
					}
					switch dbType {
					case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT":
						fieldType = "int"
						if v, ok := actualFieldValue.(float64); ok {
							i := int(v)
							valInt = &i
						} else if v, ok := actualFieldValue.(int); ok {
							valInt = &v
						} else if v, ok := actualFieldValue.(string); ok {
							if i, err := strconv.Atoi(v); err == nil {
								valInt = &i
							}
						}
					case "DECIMAL", "FLOAT", "DOUBLE":
						fieldType = "decimal"
						if v, ok := actualFieldValue.(float64); ok {
							valDecimal = &v
						}
					case "VARCHAR", "CHAR":
						fieldType = "string"
						if v, ok := actualFieldValue.(string); ok {
							valString = &v
						}
					case "TEXT", "BLOB":
						fieldType = "text"
						if v, ok := actualFieldValue.(string); ok {
							valText = &v
						}
					case "DATETIME", "DATE", "TIMESTAMP":
						fieldType = "date"
						if v, ok := actualFieldValue.(string); ok {
							valDate = &v
						}
					case "JSON":
						fieldType = "free_format_date"
						if m, ok := actualFieldValue.(map[string]interface{}); ok {
							jsonBytes, _ := json.Marshal(m)
							s := string(jsonBytes)
							valFreeFormattedDate = &s
							if sv, ok := actualSortValue.(float64); ok {
								i := int64(sv)
								valSortFreeFormattedDate = &i
							}
						}
					default:
						fieldType = "string"
						if v, ok := actualFieldValue.(string); ok {
							valString = &v
						}
					}
				}

				var exists int
				err := db.QueryRow(fmt.Sprintf("SELECT 1 FROM %s_main WHERE entity_id = ? AND field_machine_name = ?", className), id, fieldName).Scan(&exists)
				if err != nil && err != sql.ErrNoRows {
					return nil, fmt.Errorf("failed to check custom field existence: %w", err)
				}

				if err == sql.ErrNoRows {
					insertQuery := fmt.Sprintf("INSERT INTO %s_main (entity_id, field_machine_name, field_type, value_int, value_decimal, value_string, value_text, value_date, value_free_formatted_date, sort_free_formatted_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", className)
					_, err = db.Exec(insertQuery, id, fieldName, fieldType, valInt, valDecimal, valString, valText, valDate, valFreeFormattedDate, valSortFreeFormattedDate)
				} else {
					updateQuery := fmt.Sprintf("UPDATE %s_main SET field_type = ?, value_int = ?, value_decimal = ?, value_string = ?, value_text = ?, value_date = ?, value_free_formatted_date = ?, sort_free_formatted_date = ? WHERE entity_id = ? AND field_machine_name = ?", className)
					_, err = db.Exec(updateQuery, fieldType, valInt, valDecimal, valString, valText, valDate, valFreeFormattedDate, valSortFreeFormattedDate, id, fieldName)
				}

				if err != nil {
					return nil, fmt.Errorf("failed to save custom field %s: %w", fieldName, err)
				}
			}
		}
	}

	return GetEntity(id, className, db)
}

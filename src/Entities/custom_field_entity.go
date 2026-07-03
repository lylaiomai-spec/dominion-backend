package Entities

import (
	"database/sql"
	"fmt"
)

type FieldSchema struct {
	MachineName string `json:"machine_name"`
	FieldType   string `json:"field_type"`
}

type CustomField struct {
	FieldName  string      `json:"field_name"`
	FieldValue interface{} `json:"field_value"`
}

type CustomFieldConfig struct {
	MachineFieldName string `json:"machine_field_name"`
	HumanFieldName   string `json:"human_field_name"`
	FieldType        string `json:"field_type"`
	ContentFieldType string `json:"content_field_type"`
	Order            int    `json:"order"`
}

type CustomFieldData struct {
	HumanFieldName string `json:"human_field_name"`
	FieldValue     string `json:"field_value"`
	FormattedValue string `json:"formatted_value,omitempty"` // Optional: Only populated for text fields
}

type CustomFieldValue struct {
	Content     interface{} `json:"content"`
	ContentHtml string      `json:"content_html,omitempty"`
}

type CustomFieldEntity struct {
	CustomFields map[string]CustomFieldValue `json:"custom_fields"`
	FieldConfig  []CustomFieldConfig         `json:"field_config"`
}

// Compile-time check or global compiler initialization

func GenerateEntityTables(entity CustomFieldEntity, entityName string, db *sql.DB) error {
	customFieldMainTableSQL := "CREATE TABLE IF NOT EXISTS " + entityName + "_main (" +
		"entity_id INT," +
		"field_machine_name VARCHAR(255)," +
		"field_type VARCHAR(10)," +
		"value_int INT," +
		"value_decimal DECIMAL(10,2)," +
		"value_string VARCHAR(255)," +
		"value_text TEXT," +
		"value_date DATETIME)"

	customFieldFlattenedTableSQL := "CREATE TABLE IF NOT EXISTS " + entityName + "_flattened (" +
		"entity_id INT PRIMARY KEY"

	fieldTypeMap := map[string]string{
		"int":     "INT",
		"decimal": "DECIMAL(10,2)",
		"string":  "VARCHAR(255)",
		"text":    "TEXT",
		"date":    "DATETIME",
	}

	valueColumnMap := map[string]string{
		"int":     "value_int",
		"decimal": "value_decimal",
		"string":  "value_string",
		"text":    "value_text",
		"date":    "value_date",
	}

	for _, config := range entity.FieldConfig {
		valCol := valueColumnMap[config.FieldType]
		if valCol == "" {
			valCol = "value_string"
		}
		customFieldFlattenedTableSQL += ", " + config.MachineFieldName + " " + fieldTypeMap[config.FieldType]
	}
	customFieldFlattenedTableSQL += ")"

	if _, err := db.Exec(customFieldMainTableSQL); err != nil {
		return fmt.Errorf("error creating main table: %w", err)
	}
	if _, err := db.Exec(customFieldFlattenedTableSQL); err != nil {
		return fmt.Errorf("error creating flattened table: %w", err)
	}
	return UpdateTriggers(entity, entityName, db)
}

func UpdateFlattenedTable(entity CustomFieldEntity, entityName string, db *sql.DB) error {
	tableName := entityName + "_flattened"

	// 1. Get existing columns from the database to avoid trying to add duplicates.
	// This query works for MySQL/MariaDB, which matches the syntax used in GenerateEntityTables.
	rows, err := db.Query("SELECT column_name FROM information_schema.columns WHERE table_name = ? AND table_schema = DATABASE()", tableName)
	if err != nil {
		return fmt.Errorf("failed to query existing columns: %w", err)
	}
	defer rows.Close()

	existingColumns := make(map[string]bool)
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return fmt.Errorf("failed to scan column name: %w", err)
		}
		existingColumns[colName] = true
	}

	fieldTypeMap := map[string]string{
		"int":     "INT",
		"decimal": "DECIMAL(10,2)",
		"string":  "VARCHAR(255)",
		"text":    "TEXT",
		"date":    "DATETIME",
	}

	valueColumnMap := map[string]string{
		"int":     "value_int",
		"decimal": "value_decimal",
		"string":  "value_string",
		"text":    "value_text",
		"date":    "value_date",
	}

	// Track fields present in the current configuration
	configFieldNames := make(map[string]bool)

	// 2. Iterate over config and add missing columns
	for _, config := range entity.FieldConfig {
		configFieldNames[config.MachineFieldName] = true
		if !existingColumns[config.MachineFieldName] {
			sqlType := fieldTypeMap[config.FieldType]
			if sqlType == "" {
				sqlType = "VARCHAR(255)" // Default fallback
			}

			valCol := valueColumnMap[config.FieldType]
			if valCol == "" {
				valCol = "value_string"
			}

			// Note: Table and column names cannot be parameterized in SQL.
			// Ensure MachineFieldName is sanitized in production to prevent SQL injection.
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, config.MachineFieldName, sqlType)

			if _, err := db.Exec(alterSQL); err != nil {
				return fmt.Errorf("failed to add column %s: %w", config.MachineFieldName, err)
			}
		}
	}

	// 3. Remove columns that are no longer in the config
	for colName := range existingColumns {
		// Skip the primary identifier column so we don't delete the Id
		if colName == "entity_id" {
			continue
		}

		if !configFieldNames[colName] {
			alterSQL := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableName, colName)
			if _, err := db.Exec(alterSQL); err != nil {
				return fmt.Errorf("failed to drop column %s: %w", colName, err)
			}
		}
	}

	return UpdateTriggers(entity, entityName, db)
}

func UpdateTriggers(entity CustomFieldEntity, entityName string, db *sql.DB) error {
	valueColumnMap := map[string]string{
		"int":     "value_int",
		"decimal": "value_decimal",
		"string":  "value_string",
		"text":    "value_text",
		"date":    "value_date",
	}

	triggerBody := ""
	deleteTriggerBody := ""

	for _, config := range entity.FieldConfig {
		valCol := valueColumnMap[config.FieldType]
		if valCol == "" {
			valCol = "value_string"
		}
		// Update the specific column in the flattened table when the main table row matches the field name
		triggerBody += fmt.Sprintf("IF NEW.field_machine_name = '%s' THEN UPDATE %s_flattened SET %s = NEW.%s WHERE entity_id = NEW.entity_id; END IF; ", config.MachineFieldName, entityName, config.MachineFieldName, valCol)
		deleteTriggerBody += fmt.Sprintf("IF OLD.field_machine_name = '%s' THEN UPDATE %s_flattened SET %s = NULL WHERE entity_id = OLD.entity_id; END IF; ", config.MachineFieldName, entityName, config.MachineFieldName)
	}

	// Drop existing triggers to ensure we update them with new fields
	triggers := []string{"insert", "update", "delete"}
	for _, t := range triggers {
		if _, err := db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s_main_after_%s", entityName, t)); err != nil {
			return fmt.Errorf("failed to drop trigger %s: %w", t, err)
		}
	}

	// Create INSERT Trigger
	// We use INSERT IGNORE to ensure the row exists in flattened table (requires entity_id to be PRIMARY KEY or UNIQUE)
	insertSQL := fmt.Sprintf("CREATE TRIGGER %s_main_after_insert AFTER INSERT ON %s_main FOR EACH ROW BEGIN INSERT IGNORE INTO %s_flattened (entity_id) VALUES (NEW.entity_id); %s END", entityName, entityName, entityName, triggerBody)
	if _, err := db.Exec(insertSQL); err != nil {
		return fmt.Errorf("failed to create insert trigger: %w", err)
	}

	// Create UPDATE Trigger
	updateSQL := fmt.Sprintf("CREATE TRIGGER %s_main_after_update AFTER UPDATE ON %s_main FOR EACH ROW BEGIN INSERT IGNORE INTO %s_flattened (entity_id) VALUES (NEW.entity_id); %s END", entityName, entityName, entityName, triggerBody)
	if _, err := db.Exec(updateSQL); err != nil {
		return fmt.Errorf("failed to create update trigger: %w", err)
	}

	// Create DELETE Trigger
	deleteSQL := fmt.Sprintf("CREATE TRIGGER %s_main_after_delete AFTER DELETE ON %s_main FOR EACH ROW BEGIN %s END", entityName, entityName, deleteTriggerBody)
	if _, err := db.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to create delete trigger: %w", err)
	}

	return nil
}

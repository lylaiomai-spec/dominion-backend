// Standalone script: generates migration SQL by diffing the local MySQL database
// against the local default_tables.sql schema file.
//
// Usage:
//   go run local_migration.go -project <name>
//   go run local_migration.go -db <dbname> [-pass <password>] [-schema <path>]
//
// Loads saved credentials from ~/.config/cuento/config.json when -project is given.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	defaultSchemaPath = "src/Install/default_tables.sql"
	defaultDBUser     = "user"
	localDBHost       = "localhost"
	localDBPort       = "3306"
)

// ─── Config ───────────────────────────────────────────────────────────────────

type ProjectConfig struct {
	DBName string `json:"db_name"`
	DBPass string `json:"db_pass"`
}

type AppConfig struct {
	Projects map[string]*ProjectConfig `json:"projects"`
}

func loadProjectConfig(projectName string) (*ProjectConfig, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "cuento", "config.json"))
	if err != nil {
		return nil, err
	}
	var app AppConfig
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, err
	}
	p, ok := app.Projects[projectName]
	if !ok {
		return nil, fmt.Errorf("project %q not found in config", projectName)
	}
	return p, nil
}

// ─── Local MySQL ──────────────────────────────────────────────────────────────

func fetchLocalSchema(dbUser, dbName, dbPass, container, mysqlBin string) (string, error) {
	mysqlArgs := func(query string) *exec.Cmd {
		mysqlInner := []string{
			"-u", dbUser,
			"-h", localDBHost,
			"-P", localDBPort,
			"--skip-column-names",
			dbName,
		}
		if dbPass != "" {
			mysqlInner = append([]string{"-p" + dbPass}, mysqlInner...)
		}
		var cmd *exec.Cmd
		if container != "" {
			args := append([]string{"exec", "-i", container, mysqlBin}, mysqlInner...)
			cmd = exec.Command("docker", args...)
		} else {
			cmd = exec.Command(mysqlBin, mysqlInner...)
		}
		cmd.Stdin = strings.NewReader(query)
		return cmd
	}

	tablesOut, err := mysqlArgs("SHOW TABLES;").Output()
	if err != nil {
		return "", fmt.Errorf("SHOW TABLES: %w", err)
	}

	var sb strings.Builder
	for _, tableName := range strings.Split(strings.TrimSpace(string(tablesOut)), "\n") {
		tableName = strings.TrimSpace(tableName)
		if tableName == "" {
			continue
		}
		createOut, err := mysqlArgs(fmt.Sprintf("SHOW CREATE TABLE `%s`;", tableName)).Output()
		if err != nil {
			return "", fmt.Errorf("SHOW CREATE TABLE %s: %w", tableName, err)
		}
		// Output: tablename\tCREATE TABLE ...
		parts := strings.SplitN(string(createOut), "\t", 2)
		if len(parts) == 2 {
			stmt := strings.ReplaceAll(parts[1], `\n`, "\n")
			stmt = strings.ReplaceAll(stmt, `\t`, "\t")
			stmt = strings.TrimSpace(stmt)
			if !strings.HasSuffix(stmt, ";") {
				stmt += ";"
			}
			sb.WriteString(stmt)
			sb.WriteString("\n\n")
		}
	}
	return sb.String(), nil
}

// ─── SQL parsing ──────────────────────────────────────────────────────────────

var reCreateTable = regexp.MustCompile(
	`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
		"(?:`?(\\w+)`?)" +
		`\s*\(([\s\S]+?)\)\s*(?:ENGINE\b|;)`,
)

var reColumnName = regexp.MustCompile("^\\s*`?(\\w+)`?\\s+")
var rePKCols = regexp.MustCompile(`(?i)primary\s+key\s*\(([^)]+)\)`)
var reFKRef = regexp.MustCompile(`(?i)REFERENCES\s+` + "`?(\\w+)`?")

var (
	reNormWS            = regexp.MustCompile(`\s+`)
	reDefaultBeforeNull = regexp.MustCompile(`(?i)\bdefault\s+(\S+)\s+not\s+null\b`)
	reCharsetSQL        = regexp.MustCompile(`(?i)\s+(character\s+set|charset|collate)\s+\S+`)
	reIntWidth          = regexp.MustCompile(`\b(tinyint|smallint|mediumint|int|integer|bigint|year)\(\d+\)`)
	reInlineKey         = regexp.MustCompile(`\s+(primary\s+key|unique\s+key|unique)\b`)
	reNullClause        = regexp.MustCompile(`\s+(default\s+)?null\b`)
	reBoolean           = regexp.MustCompile(`\b(boolean|bool)\b`)
	reDecimal           = regexp.MustCompile(`\bdecimal(\s*\([^)]*\))?`)
	reJSON              = regexp.MustCompile(`\bjson\b`)
	reCheckExpr         = regexp.MustCompile(`\s+check\s*\((?:[^()]*|\([^()]*\))*\)`)
	reInsertTable       = regexp.MustCompile(`(?i)INSERT\s+(?:IGNORE\s+)?INTO\s+` + "`?(\\w+)`?")
)

func normalizeColDef(def string) string {
	def = strings.ReplaceAll(def, "`", "")
	def = reCharsetSQL.ReplaceAllString(def, "")
	def = strings.ToLower(strings.TrimSpace(def))
	def = strings.ReplaceAll(def, "current_timestamp()", "current_timestamp")
	def = strings.ReplaceAll(def, "default false", "default 0")
	def = strings.ReplaceAll(def, "default true", "default 1")
	def = reBoolean.ReplaceAllString(def, "tinyint(1)")
	def = reIntWidth.ReplaceAllString(def, "$1")
	def = reDecimal.ReplaceAllStringFunc(def, func(s string) string {
		if strings.Contains(s, "(") {
			return s
		}
		return "decimal(10,0)"
	})
	def = reJSON.ReplaceAllString(def, "longtext")
	def = reCheckExpr.ReplaceAllString(def, "")
	if strings.Contains(def, "auto_increment") && !strings.Contains(def, "not null") {
		def = strings.Replace(def, "auto_increment", "not null auto_increment", 1)
	}
	def = strings.ReplaceAll(def, "auto_increment not null", "not null auto_increment")
	if strings.Contains(def, "primary key") && !strings.Contains(def, "not null") {
		def = strings.Replace(def, "primary key", "not null primary key", 1)
	}
	def = reInlineKey.ReplaceAllString(def, "")
	def = reDefaultBeforeNull.ReplaceAllString(def, "not null default $1")
	def = strings.ReplaceAll(def, "not null", "\x01")
	def = reNullClause.ReplaceAllString(def, "")
	def = strings.ReplaceAll(def, "\x01", "not null")
	def = reNormWS.ReplaceAllString(def, " ")
	def = strings.ReplaceAll(def, ", ", ",")
	def = strings.ReplaceAll(def, "enum (", "enum(")
	def = strings.ReplaceAll(def, "set (", "set(")
	return strings.TrimSpace(def)
}

func parseTables(sql string) map[string]map[string]string {
	tables := make(map[string]map[string]string)
	for _, m := range reCreateTable.FindAllStringSubmatch(sql, -1) {
		name := strings.ToLower(m[1])
		cols := make(map[string]string)
		body := m[2]
		var pkCols []string
		var lastCol string
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(strings.TrimRight(line, ","))
			if line == "" {
				continue
			}
			upper := strings.ToUpper(line)
			if upper == "PRIMARY KEY" {
				if lastCol != "" {
					pkCols = append(pkCols, lastCol)
				}
				continue
			}
			if strings.HasPrefix(upper, "PRIMARY ") ||
				strings.HasPrefix(upper, "UNIQUE ") || strings.HasPrefix(upper, "UNIQUE\t") ||
				upper == "KEY" || strings.HasPrefix(upper, "KEY ") || strings.HasPrefix(upper, "KEY\t") || strings.HasPrefix(upper, "KEY`") ||
				strings.HasPrefix(upper, "INDEX ") || strings.HasPrefix(upper, "INDEX\t") ||
				strings.HasPrefix(upper, "CONSTRAINT ") ||
				strings.HasPrefix(upper, "FOREIGN ") {
				continue
			}
			if cm := reColumnName.FindStringSubmatch(line); len(cm) > 1 {
				lastCol = strings.ToLower(cm[1])
				cols[lastCol] = line
			}
		}
		if pm := rePKCols.FindStringSubmatch(strings.ReplaceAll(body, "\n", " ")); pm != nil {
			for _, pkCol := range strings.Split(pm[1], ",") {
				pkCols = append(pkCols, strings.ToLower(strings.Trim(strings.TrimSpace(pkCol), "`")))
			}
		}
		for _, pk := range pkCols {
			if def, ok := cols[pk]; ok {
				upper := strings.ToUpper(def)
				if !strings.Contains(upper, "NOT NULL") {
					if strings.Contains(upper, " NULL") {
						def = regexp.MustCompile(`(?i)\bnull\b`).ReplaceAllString(def, "NOT NULL")
					} else {
						def += " NOT NULL"
					}
					cols[pk] = def
				}
			}
		}
		tables[name] = cols
	}
	return tables
}

func extractInserts(sql string) []string {
	var inserts []string
	var current strings.Builder
	inInsert := false
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inInsert {
			if strings.HasPrefix(strings.ToUpper(trimmed), "INSERT") {
				inInsert = true
				current.Reset()
			} else {
				continue
			}
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(trimmed)
		if strings.HasSuffix(trimmed, ";") {
			inserts = append(inserts, current.String())
			inInsert = false
		}
	}
	return inserts
}

func fkDeps(tableDDL string) map[string]bool {
	deps := map[string]bool{}
	for _, m := range reFKRef.FindAllStringSubmatch(tableDDL, -1) {
		deps[strings.ToLower(m[1])] = true
	}
	return deps
}

func topoSort(newTableNames []string, ddlOf map[string]string) []string {
	deps := make(map[string]map[string]bool, len(newTableNames))
	newSet := make(map[string]bool, len(newTableNames))
	for _, t := range newTableNames {
		newSet[t] = true
	}
	for _, t := range newTableNames {
		d := fkDeps(ddlOf[t])
		filtered := map[string]bool{}
		for ref := range d {
			if newSet[ref] && ref != t {
				filtered[ref] = true
			}
		}
		deps[t] = filtered
	}
	var sorted []string
	visited := map[string]bool{}
	var visit func(t string)
	visit = func(t string) {
		if visited[t] {
			return
		}
		visited[t] = true
		refs := make([]string, 0, len(deps[t]))
		for r := range deps[t] {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		for _, r := range refs {
			visit(r)
		}
		sorted = append(sorted, t)
	}
	alpha := make([]string, len(newTableNames))
	copy(alpha, newTableNames)
	sort.Strings(alpha)
	for _, t := range alpha {
		visit(t)
	}
	return sorted
}

func generateMigration(oldSQL, newSQL string) string {
	oldTables := parseTables(oldSQL)
	newTables := parseTables(newSQL)

	newTableDDL := map[string]string{}
	for _, mm := range reCreateTable.FindAllStringSubmatch(newSQL, -1) {
		newTableDDL[strings.ToLower(mm[1])] = mm[0]
	}
	var newTableNames []string
	for tableName := range newTables {
		if _, exists := oldTables[tableName]; !exists {
			newTableNames = append(newTableNames, tableName)
		}
	}
	orderedNew := topoSort(newTableNames, newTableDDL)

	var stmts []string
	for _, tableName := range orderedNew {
		if ddl, ok := newTableDDL[tableName]; ok {
			stmts = append(stmts, ddl+";\n")
		}
	}
	for tableName, newCols := range newTables {
		oldCols, exists := oldTables[tableName]
		if !exists {
			continue
		}
		for colName, colDef := range newCols {
			cleanDef := strings.Join(strings.Fields(colDef), " ")
			if oldDef, known := oldCols[colName]; !known {
				stmts = append(stmts,
					fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN %s;\n", tableName, cleanDef))
			} else if normalizeColDef(oldDef) != normalizeColDef(colDef) {
				stmts = append(stmts,
					fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN %s;\n", tableName, cleanDef))
			}
		}
	}
	newTableSet := make(map[string]bool, len(newTableNames))
	for _, t := range newTableNames {
		newTableSet[t] = true
	}
	for _, ins := range extractInserts(newSQL) {
		if m := reInsertTable.FindStringSubmatch(ins); m != nil {
			if newTableSet[strings.ToLower(m[1])] {
				stmts = append(stmts, ins+"\n")
			}
		}
	}
	return strings.Join(stmts, "\n")
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	var (
		projectFlag   = flag.String("project", "", "project name (loads DB name and password from ~/.config/cuento/config.json)")
		dbFlag        = flag.String("db", "", "database name (alternative to -project)")
		userFlag      = flag.String("user", defaultDBUser, "database user")
		passFlag      = flag.String("pass", "", "database password (leave empty if none)")
		schemaFlag    = flag.String("schema", defaultSchemaPath, "path to default_tables.sql (relative to working directory)")
		containerFlag = flag.String("container", "", "docker container name to run mysql inside (avoids local client auth issues)")
		mysqlBinFlag  = flag.String("mysql-bin", "mysql", "mysql binary name (use 'mariadb' for MariaDB containers)")
	)
	flag.Parse()

	var dbName, dbPass, dbUser string

	if *projectFlag != "" {
		cfg, err := loadProjectConfig(*projectFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
			os.Exit(1)
		}
		dbName = cfg.DBName
		dbPass = cfg.DBPass
		dbUser = *userFlag
	} else if *dbFlag != "" {
		dbName = *dbFlag
		dbPass = *passFlag
		dbUser = *userFlag
	} else {
		fmt.Fprintln(os.Stderr, "usage: go run local_migration.go -project <name>")
		fmt.Fprintln(os.Stderr, "       go run local_migration.go -db <dbname> [-user <user>] [-pass <password>] [-schema <path>]")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "▶  Reading schema from %s...\n", *schemaFlag)
	schemaBytes, err := os.ReadFile(*schemaFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading schema file: %v\n", err)
		os.Exit(1)
	}
	latestSQL := string(schemaBytes)
	fmt.Fprintln(os.Stderr, "   ✓ Done")

	if *containerFlag != "" {
		fmt.Fprintf(os.Stderr, "▶  Reading live schema via docker container %q (%s@%s/%s)...\n", *containerFlag, dbUser, localDBHost, dbName)
	} else {
		fmt.Fprintf(os.Stderr, "▶  Reading live schema from local DB (%s@%s/%s)...\n", dbUser, localDBHost, dbName)
	}
	liveSQL, err := fetchLocalSchema(dbUser, dbName, dbPass, *containerFlag, *mysqlBinFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading local schema: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "   ✓ Done")

	fmt.Fprintln(os.Stderr, "▶  Generating migration SQL...")
	migration := generateMigration(liveSQL, latestSQL)

	if migration == "" {
		fmt.Fprintln(os.Stderr, "   ✓ Schema is already up to date — no migration needed.")
		return
	}

	fmt.Fprintln(os.Stderr, "   ✓ Done")
	fmt.Fprintln(os.Stderr)
	fmt.Print(migration)
}

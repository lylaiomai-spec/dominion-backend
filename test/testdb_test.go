package test

import (
	"bytes"
	"cuento-backend/config"
	"cuento-backend/src/Controllers"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// testDBName is fixed on purpose: there is no override that could point the
// tests at the development database.
const testDBName = "cuento_test"

const (
	roleGuest = 1
	roleUser  = 2
	roleAdmin = 3
)

var (
	testDB      *sql.DB // pool used by handlers, same DSN options as production
	schemaDB    *sql.DB // multiStatements pool used only to rebuild the schema
	testDBError error
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	if err := Services.InitI18n("../locales"); err != nil {
		log.Fatalf("loading locales: %v", err)
	}
	testDB, schemaDB, testDBError = openTestDB()
	if testDBError != nil {
		log.Printf("WARNING: database tests will be skipped: %v", testDBError)
	}
	Services.DB = testDB

	code := m.Run()

	if testDB != nil {
		testDB.Close()
		schemaDB.Close()
	}
	os.Exit(code)
}

// openTestDB connects with the DB_HOST/DB_PORT/DB_USER/DB_PASSWORD settings but
// always to cuento_test. See developers/local-setup.md for the one-time setup.
func openTestDB() (*sql.DB, *sql.DB, error) {
	cfg := config.LoadDBConfig()
	cfg.Name = testDBName

	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", testDBName, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("connecting to %s on %s:%s: %w", testDBName, cfg.Host, cfg.Port, err)
	}
	schema, err := sql.Open("mysql", cfg.DSN()+"&multiStatements=true")
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("opening %s schema connection: %w", testDBName, err)
	}
	return db, schema, nil
}

// resetDB rebuilds cuento_test from default_tables.sql and permissions.csv, so
// every test starts from the same state a fresh install has. Tests that use it
// share one database and must not call t.Parallel().
func resetDB(t *testing.T) *sql.DB {
	t.Helper()
	if testDB == nil {
		t.Skipf("test database unavailable, skipping: %v", testDBError)
	}
	if err := dropAllTables(); err != nil {
		t.Fatalf("dropping tables: %v", err)
	}
	schema, err := os.ReadFile("../src/Install/default_tables.sql")
	if err != nil {
		t.Fatalf("reading schema: %v", err)
	}
	if _, err := schemaDB.Exec(string(schema)); err != nil {
		t.Fatalf("loading schema: %v", err)
	}
	if err := importPermissions("../src/Install/permissions.csv"); err != nil {
		t.Fatalf("importing permissions: %v", err)
	}
	return testDB
}

func dropAllTables() error {
	rows, err := schemaDB.Query("SELECT table_name FROM information_schema.tables WHERE table_schema = ?", testDBName)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scanning table name: %w", err)
		}
		tables = append(tables, "`"+name+"`")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}
	_, err = schemaDB.Exec("SET FOREIGN_KEY_CHECKS = 0; DROP TABLE " + strings.Join(tables, ", ") + "; SET FOREIGN_KEY_CHECKS = 1;")
	return err
}

func importPermissions(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(records) < 2 {
		return fmt.Errorf("%s has no permission rows", path)
	}
	placeholders := make([]string, 0, len(records)-1)
	args := make([]interface{}, 0, 3*(len(records)-1))
	for _, rec := range records[1:] { // first row is the header
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, rec[0], rec[1], rec[2])
	}
	_, err = testDB.Exec("INSERT IGNORE INTO role_permission (role_id, type, permission) VALUES "+strings.Join(placeholders, ", "), args...)
	return err
}

// fixture is the shared starting point for handler tests: one user per role,
// a user without any role, and three subforums with different access rules.
type fixture struct {
	db *sql.DB

	member, guestUser, admin, noRole int

	// subA: roleUser holds every subforum permission (and roleGuest holds them
	// too, so the endpoint-role layer can be tested on its own).
	// subB: only roleAdmin holds subforum permissions.
	// subPrivate: private; access comes only from private_subforum_users.
	subA, subB, subPrivate int
}

var allSubforumPermissions = []string{
	"subforum_read", "subforum_post",
	"subforum_create_general_topic", "subforum_create_episode_topic", "subforum_create_character_topic",
	"subforum_create_wanted_character_topic",
	"subforum_edit_own_post", "subforum_edit_own_topic",
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{db: resetDB(t)}

	f.member = f.addUser(t, "member", roleUser)
	f.guestUser = f.addUser(t, "guest-role", roleGuest)
	f.admin = f.addUser(t, "admin", roleAdmin, roleUser)
	f.noRole = f.addUser(t, "no-role")

	category := f.insert(t, "INSERT INTO categories (name, position) VALUES ('Game', 1)")
	f.subA = f.insert(t, "INSERT INTO subforums (category_id, name, position) VALUES (?, 'Subforum A', 1)", category)
	f.subB = f.insert(t, "INSERT INTO subforums (category_id, name, position) VALUES (?, 'Subforum B', 2)", category)
	f.subPrivate = f.insert(t, "INSERT INTO subforums (category_id, name, position, is_private) VALUES (?, 'Private', 3, 1)", category)

	f.grantSubforum(t, roleUser, f.subA, allSubforumPermissions...)
	f.grantSubforum(t, roleGuest, f.subA, allSubforumPermissions...)
	f.grantSubforum(t, roleAdmin, f.subB, allSubforumPermissions...)
	f.grantSubforum(t, roleUser, f.subPrivate, allSubforumPermissions...)
	return f
}

const fixturePassword = "correct horse"

// bcrypt runs at cost 14 (about a second), so the fixture password is hashed once per run.
var fixturePasswordHash = sync.OnceValues(func() (string, error) {
	var u Entities.User
	err := u.HashPassword(fixturePassword)
	return u.Password, err
})

func (f *fixture) addUser(t *testing.T, username string, roles ...int) int {
	t.Helper()
	hash, err := fixturePasswordHash()
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}
	id := f.insert(t, "INSERT INTO users (username, password, date_registered) VALUES (?, ?, NOW())", username, hash)
	for _, role := range roles {
		f.exec(t, "INSERT INTO user_role (user_id, role_id) VALUES (?, ?)", id, role)
	}
	return id
}

func (f *fixture) grantSubforum(t *testing.T, roleID, subforumID int, perms ...string) {
	t.Helper()
	for _, p := range perms {
		f.exec(t, "INSERT INTO role_permission (role_id, type, permission) VALUES (?, 1, ?)", roleID, fmt.Sprintf("%s:%d", p, subforumID))
	}
}

func (f *fixture) addTopic(t *testing.T, subforumID, authorID int, topicType Entities.TopicType, name string) int {
	t.Helper()
	return f.insert(t, `INSERT INTO topics (subforum_id, name, author_user_id, date_created, date_last_post, status, type, post_number, last_post_author_user_id)
		VALUES (?, ?, ?, NOW(), NOW(), 0, ?, 0, ?)`, subforumID, name, authorID, topicType, authorID)
}

func (f *fixture) addPost(t *testing.T, topicID, authorID int, content string) int {
	t.Helper()
	return f.insert(t, "INSERT INTO posts (topic_id, author_user_id, content) VALUES (?, ?, ?)", topicID, authorID, content)
}

// addCharacter creates an active character with its sheet topic and main profile.
func (f *fixture) addCharacter(t *testing.T, subforumID, userID int, name string) (characterID, topicID int) {
	t.Helper()
	topicID = f.addTopic(t, subforumID, userID, Entities.CharacterSheetTopic, name)
	characterID = f.insert(t, "INSERT INTO character_base (user_id, name, topic_id, character_status) VALUES (?, ?, ?, ?)",
		userID, name, topicID, Entities.ActiveCharacter)
	f.exec(t, "INSERT INTO character_profile_base (character_id, user_id) VALUES (?, ?)", characterID, userID)
	return characterID, topicID
}

func (f *fixture) addNotification(t *testing.T, userID int) int {
	t.Helper()
	return f.insert(t, `INSERT INTO notifications (user_id, type, title, message, data) VALUES (?, 'mention', 'Mention', 'You were mentioned', ?)`,
		userID, `{"user_id": 1, "user_name": "author", "post_id": 1, "topic_id": 1, "topic_name": "Topic"}`)
}

func (f *fixture) insert(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	res, err := f.db.Exec(query, args...)
	if err != nil {
		t.Fatalf("fixture insert %q: %v", query, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("fixture insert id %q: %v", query, err)
	}
	return int(id)
}

func (f *fixture) exec(t *testing.T, query string, args ...interface{}) {
	t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		t.Fatalf("fixture exec %q: %v", query, err)
	}
}

func (f *fixture) count(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// do sends a request through a router wired like main.go. userID 0 sends no token.
func (f *fixture) do(t *testing.T, method, path string, userID int, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if userID != 0 {
		req.Header.Set("Authorization", "Bearer "+signToken(t, userID))
	}
	w := httptest.NewRecorder()
	newTestRouter(f.db).ServeHTTP(w, req)
	return w
}

func signToken(t *testing.T, userID int) string {
	t.Helper()
	claims := &Middlewares.Claims{
		UserID:           userID,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(Middlewares.JwtKey)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return token
}

// newTestRouter mirrors the middleware chain and paths that main.go uses for
// the endpoints under test.
func newTestRouter(db *sql.DB) *gin.Engine {
	r := gin.New()
	r.Use(Middlewares.ErrorMiddleware())
	r.Use(Middlewares.FeatureFlagsMiddleware(db))

	public := r.Group("/")
	public.POST("/register", func(c *gin.Context) { Controllers.Register(c, db) })
	public.POST("/login", func(c *gin.Context) { Controllers.Login(c, db) })

	optional := r.Group("/")
	optional.Use(Middlewares.OptionalAuthMiddleware(), Middlewares.UserLocaleMiddleware(db))
	optional.GET("/topic/get/:id", func(c *gin.Context) { Controllers.GetTopic(c, db) })
	optional.GET("/character/get/:id", func(c *gin.Context) { Controllers.GetCharacter(c, db) })
	optional.POST("/post/create", func(c *gin.Context) { Controllers.CreatePost(c, db) })

	protected := r.Group("/")
	protected.Use(Middlewares.AuthMiddleware(), Middlewares.PermissionsMiddleware(db), Middlewares.UserLocaleMiddleware(db))
	protected.POST("/character/create", func(c *gin.Context) { Controllers.CreateCharacter(c, db) })
	protected.POST("/episode/create", func(c *gin.Context) { Controllers.CreateEpisode(c, db) })
	protected.POST("/wanted-character/create", func(c *gin.Context) { Controllers.CreateWantedCharacter(c, db) })
	protected.POST("/post/update/:id", func(c *gin.Context) { Controllers.UpdatePost(c, db) })
	protected.POST("/topic/update/:id", func(c *gin.Context) { Controllers.UpdateTopic(c, db) })
	protected.GET("/notifications/unread", func(c *gin.Context) { Controllers.GetUnreadNotifications(c, db) })
	protected.POST("/notifications/dismiss/:id", func(c *gin.Context) { Controllers.DismissNotification(c, db) })
	return r
}

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("expected status %d, got %d. Body: %s", want, w.Code, w.Body.String())
	}
}

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type Store struct {
	database     *sql.DB
	databasePath string
}

func Open(databasePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	database, err := sql.Open("sqlite", "file:"+databasePath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	database.SetConnMaxLifetime(0)
	store := &Store{database: database, databasePath: databasePath}
	if err := store.Ping(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return store, nil
}

func (store *Store) Close() error {
	return store.database.Close()
}

// Reopen replaces the SQLite handle after an administrator restores a
// validated database file. The caller must serialize this operation with
// other administrative writes.
func (store *Store) Reopen(ctx context.Context) error {
	if store.database != nil {
		if err := store.database.Close(); err != nil {
			return err
		}
	}
	database, err := sql.Open("sqlite", "file:"+store.databasePath+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return fmt.Errorf("reopen sqlite: %w", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	database.SetConnMaxLifetime(0)
	store.database = database
	if err := store.Ping(ctx); err != nil {
		_ = database.Close()
		return err
	}
	if err := store.Migrate(ctx); err != nil {
		_ = database.Close()
		return fmt.Errorf("migrate restored sqlite: %w", err)
	}
	return nil
}

// ValidateDatabaseFile checks the minimum schema required by a Review Hub
// backup without changing the imported file.
func ValidateDatabaseFile(path string) error {
	database, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open imported sqlite: %w", err)
	}
	defer database.Close()
	var value int
	if err := database.QueryRow("SELECT 1").Scan(&value); err != nil {
		return fmt.Errorf("probe imported sqlite: %w", err)
	}
	rows, err := database.Query("SELECT name FROM sqlite_master WHERE type='table' AND name IN ('users','projects','pdf_snapshots','review_comments')")
	if err != nil {
		return fmt.Errorf("inspect imported sqlite: %w", err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, required := range []string{"users", "projects", "pdf_snapshots", "review_comments"} {
		if !found[required] {
			return fmt.Errorf("imported sqlite is missing table %s", required)
		}
	}
	return nil
}

func (store *Store) Ping(ctx context.Context) error {
	var value int
	if err := store.database.QueryRowContext(ctx, "SELECT 1").Scan(&value); err != nil {
		return fmt.Errorf("sqlite probe: %w", err)
	}
	if value != 1 {
		return errors.New("sqlite probe returned an unexpected value")
	}
	return nil
}

func (store *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := store.database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (store *Store) Checkpoint(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)")
	return err
}

type PlatformDefaults struct {
	MaxPDFBytes        int
	MaxImportBytes     int
	MaxProjectsPerUser int
	MaxUsers           int
	AllowedRoots       []string
}

// SeedPlatformDefaults imports bootstrap configuration only for keys that an
// administrator has not already changed through the web console.
func (store *Store) SeedPlatformDefaults(ctx context.Context, defaults PlatformDefaults) error {
	allowedRoots, err := json.Marshal(defaults.AllowedRoots)
	if err != nil {
		return err
	}
	values := map[string]string{
		"max_pdf_bytes":         strconv.Itoa(defaults.MaxPDFBytes),
		"max_import_bytes":      strconv.Itoa(defaults.MaxImportBytes),
		"max_projects_per_user": strconv.Itoa(defaults.MaxProjectsPerUser),
		"max_users":             strconv.Itoa(defaults.MaxUsers),
		"allowed_roots":         string(allowedRoots),
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(ctx,
		"INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING",
	)
	if err != nil {
		return err
	}
	defer statement.Close()
	for key, value := range values {
		if _, err := statement.ExecContext(ctx, key, value, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (store *Store) UserByID(ctx context.Context, id string) (domain.User, error) {
	return store.userBy(ctx, "id", id)
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (store *Store) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return store.userBy(ctx, "email", email)
}

func (store *Store) userBy(ctx context.Context, column, value string) (domain.User, error) {
	if column != "id" && column != "email" {
		return domain.User{}, errors.New("unsupported user lookup")
	}
	var user domain.User
	var projectLimit sql.NullInt64
	err := store.database.QueryRowContext(ctx,
		"SELECT id,email,display_name,password_hash,role,is_active,project_limit FROM users WHERE "+column+"=?", value,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &user.IsActive, &projectLimit)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.NewAPIError(401, "AUTH_REQUIRED", "请先登录")
	}
	if err != nil {
		return domain.User{}, err
	}
	if projectLimit.Valid {
		user.ProjectLimit = &projectLimit.Int64
	}
	return user, nil
}

func (store *Store) RegistrationOpen(ctx context.Context) (bool, error) {
	var value string
	err := store.database.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key='registration_open'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return value != "false", nil
}

func (store *Store) SetupRequired(ctx context.Context) (bool, error) {
	var count int
	if err := store.database.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (store *Store) UserCapacityAvailable(ctx context.Context, maxUsers int) (bool, error) {
	var count int
	err := store.database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE email NOT LIKE 'share-%@local.invalid'",
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count < maxUsers, nil
}

func (store *Store) CreateUser(ctx context.Context, user domain.User) error {
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		"INSERT INTO users(id,email,display_name,password_hash,role,is_active,project_limit,created_at,updated_at,last_login_at) VALUES(?,?,?,?,?,?,?,?,?,NULL)",
		user.ID, user.Email, user.DisplayName, user.PasswordHash, user.Role, user.IsActive, user.ProjectLimit, now, now,
	)
	return err
}

func (store *Store) UpdateLastLogin(ctx context.Context, userID string) error {
	_, err := store.database.ExecContext(ctx, "UPDATE users SET last_login_at=? WHERE id=?", time.Now().UnixMilli(), userID)
	return err
}

func (store *Store) UpdateDisplayName(ctx context.Context, userID, displayName string) error {
	_, err := store.database.ExecContext(ctx, "UPDATE users SET display_name=?,updated_at=? WHERE id=?", displayName, time.Now().UnixMilli(), userID)
	return err
}

func (store *Store) SaveListenerSettings(ctx context.Context, host string, port int) error {
	value, err := json.Marshal(map[string]any{"host": host, "port": port})
	if err != nil {
		return err
	}
	_, err = store.database.ExecContext(ctx,
		"INSERT INTO app_settings(key,value,updated_at) VALUES('listener_settings',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at",
		string(value), time.Now().UnixMilli(),
	)
	return err
}

func (store *Store) Audit(ctx context.Context, projectID *string, actorID *string, entityType, entityID, action string, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte(`{"truncated":true,"reason":"unserializable"}`)
	}
	if len(encoded) > 16_384 {
		encoded = []byte(fmt.Sprintf(`{"truncated":true,"originalBytes":%d}`, len(encoded)))
	}
	level := "info"
	value := strings.ToLower(entityType + ":" + action)
	if strings.Contains(value, "failed") || strings.Contains(value, "denied") || strings.Contains(value, "deleted") || strings.Contains(value, "revoked") {
		level = "warning"
	}
	_, _ = store.database.ExecContext(ctx,
		"INSERT INTO audit_events(id,project_id,actor_user_id,entity_type,entity_id,action,before_json,after_json,created_at,level) VALUES(?,?,?,?,?,?,?,?,?,?)",
		uuid.NewString(), projectID, actorID, entityType, entityID, action, nil, string(encoded), time.Now().UnixMilli(), level,
	)
}

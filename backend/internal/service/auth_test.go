package service

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

func TestLoginCreatesNodeCompatibleSessionCookie(t *testing.T) {
	store := testStore(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password-with-12"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	user := domain.User{
		ID:           "user-1",
		Email:        "reviewer@example.test",
		DisplayName:  "reviewer",
		PasswordHash: string(hash),
		Role:         "reviewer",
		IsActive:     1,
	}
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(store, "shared-session-secret", false, 1000)
	_, cookie, err := service.Login(context.Background(), user.Email, "password-with-12", true)
	if err != nil {
		t.Fatal(err)
	}
	if cookie.Name != sessionCookieName || cookie.MaxAge == 0 || !cookie.HttpOnly {
		t.Fatalf("unexpected session cookie: %#v", cookie)
	}
	request := httptest.NewRequest("GET", "/api/auth/me", nil)
	request.AddCookie(cookie)
	current, err := service.CurrentUser(request)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != user.ID || current.Email != user.Email {
		t.Fatalf("current user = %#v", current)
	}
}

func TestCurrentUserRejectsModifiedSignature(t *testing.T) {
	store := testStore(t)
	service := NewAuthService(store, "shared-session-secret", false, 1000)
	request := httptest.NewRequest("GET", "/api/auth/me", nil)
	request.Header.Set("Cookie", sessionCookieName+"=user-1.invalid")
	if _, err := service.CurrentUser(request); err == nil {
		t.Fatal("modified signature was accepted")
	}
}

func TestSetupRejectsUnsupportedListenerWithoutCreatingAdmin(t *testing.T) {
	store := testStore(t)
	service := NewAuthService(store, "shared-session-secret", false, 1000)

	_, _, err := service.Setup(
		context.Background(),
		"admin@example.test",
		"Administrator",
		"password-with-12",
		"password-with-12",
		"not/a-host",
		3000,
	)
	if err == nil {
		t.Fatal("setup accepted an unsupported listener")
	}
	required, err := store.SetupRequired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !required {
		t.Fatal("invalid setup created an administrator")
	}
}

func testStore(t *testing.T) *repository.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review-hub.db")
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`
      CREATE TABLE users(
        id TEXT PRIMARY KEY,email TEXT UNIQUE NOT NULL,display_name TEXT NOT NULL,
        password_hash TEXT NOT NULL,role TEXT NOT NULL,is_active INTEGER NOT NULL,
        project_limit INTEGER,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,last_login_at INTEGER
      );
      CREATE TABLE app_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL,updated_at INTEGER NOT NULL);
      CREATE TABLE audit_events(
        id TEXT PRIMARY KEY,project_id TEXT,actor_user_id TEXT,entity_type TEXT NOT NULL,
        entity_id TEXT NOT NULL,action TEXT NOT NULL,before_json TEXT,after_json TEXT,
        created_at INTEGER NOT NULL,level TEXT NOT NULL
      );`)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

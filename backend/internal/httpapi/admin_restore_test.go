package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

func TestAdminRestoreValidatesAndReopensRestoredSQLite(t *testing.T) {
	dataDirectory := t.TempDir()
	store, err := repository.Open(filepath.Join(dataDirectory, "paper-review.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{MaxPDFBytes: 2_000_000, MaxImportBytes: 3_000_000, MaxProjectsPerUser: 20, MaxUsers: 100, AllowedRoots: []string{dataDirectory}}); err != nil {
		t.Fatal(err)
	}
	password := "restore-password-12"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	admin := domain.User{ID: "admin", Email: "admin@example.test", DisplayName: "Admin", PasswordHash: string(hash), Role: "admin", IsActive: 1}
	if err := store.CreateUser(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDirectory, "keep.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "restore-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetDataDirectory(dataDirectory)

	importedPath := filepath.Join(t.TempDir(), "imported.db")
	imported, err := repository.Open(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := imported.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{MaxPDFBytes: 2_000_000, MaxImportBytes: 3_000_000, MaxProjectsPerUser: 20, MaxUsers: 100, AllowedRoots: []string{dataDirectory}}); err != nil {
		t.Fatal(err)
	}
	if err := imported.CreateUser(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	if err := imported.Close(); err != nil {
		t.Fatal(err)
	}
	databaseBytes, err := os.ReadFile(importedPath)
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := makeRestoreArchive(t, databaseBytes, []string{"paper-review.db"})
	result, err := api.restoreBackupArchive(context.Background(), archiveBytes, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Format != "review-hub-backup" || result.PreRestoreBackup == "" {
		t.Fatalf("unexpected restore result: %#v", result)
	}
	if _, err := store.UserByEmail(context.Background(), admin.Email); err != nil {
		t.Fatalf("reopened store cannot read restored database: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "backups", result.PreRestoreBackup)); err != nil {
		t.Fatalf("pre-restore backup missing: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(dataDirectory, "keep.txt")); err != nil || string(content) != "before" {
		t.Fatalf("root data unexpectedly changed: content=%q err=%v", content, err)
	}

	unsafe := makeRestoreArchive(t, databaseBytes, []string{"paper-review.db", "../escape.txt"})
	if _, err := api.restoreBackupArchive(context.Background(), unsafe, admin.ID); err == nil {
		t.Fatal("unsafe restore archive was accepted")
	}
}

func makeRestoreArchive(t *testing.T, database []byte, files []string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	databaseEntry, err := writer.Create("paper-review.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := databaseEntry.Write(database); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if name == "paper-review.db" {
			continue
		}
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("test")); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := writer.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(map[string]any{"format": "review-hub-backup", "formatVersion": 1, "schemaVersion": currentSchemaVersion, "files": files})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write(encoded); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

func TestAdminOverviewIsOwnedByGoAPI(t *testing.T) {
	store, err := repository.Open(filepath.Join(t.TempDir(), "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{
		MaxPDFBytes: 2_000_000, MaxImportBytes: 3_000_000,
		MaxProjectsPerUser: 20, MaxUsers: 100, AllowedRoots: []string{"/safe"},
	}); err != nil {
		t.Fatal(err)
	}
	password := "test-password-12"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	admin := domain.User{ID: "admin", Email: "admin@example.test", DisplayName: "Admin", PasswordHash: string(hash), Role: "admin", IsActive: 1}
	viewer := domain.User{ID: "viewer", Email: "viewer@example.test", DisplayName: "Viewer", PasswordHash: string(hash), Role: "viewer", IsActive: 1}
	for _, user := range []domain.User{admin, viewer} {
		if err := store.CreateUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{
		ID: "project", Name: "Project", Slug: "project", RepositoryPath: "/tmp/project",
		SourcePDFPath: "/tmp/project/paper.pdf", CreatedByUserID: admin.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSnapshot(context.Background(), repository.NewSnapshot{
		ID: "snapshot", ProjectID: "project", VersionNumber: 1,
		OriginalPDFPath: "/tmp/project/paper.pdf", ArchivedPDFPath: "snapshots/project/paper.pdf",
		OriginalFileName: "paper.pdf", FileSizeBytes: 42, SHA256: "hash", PageCount: 1, ArchivedByUserID: admin.ID,
	}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	login := func(email string) *http.Cookie {
		_, cookie, err := auth.Login(context.Background(), email, password, false)
		if err != nil {
			t.Fatal(err)
		}
		return cookie
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	adminRequest.AddCookie(login(admin.Email))
	adminResponse := httptest.NewRecorder()
	api.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body = %s", adminResponse.Code, adminResponse.Body.String())
	}
	if !containsAll(adminResponse.Body.String(), `"users":2`, `"projects":1`, `"snapshots":1`, `"configuredRootCount":1`) {
		t.Fatalf("unexpected admin overview: %s", adminResponse.Body.String())
	}

	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	viewerRequest.AddCookie(login(viewer.Email))
	viewerResponse := httptest.NewRecorder()
	api.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, body = %s", viewerResponse.Code, viewerResponse.Body.String())
	}
}

func containsAll(value string, expected ...string) bool {
	for _, item := range expected {
		if !strings.Contains(value, item) {
			return false
		}
	}
	return true
}

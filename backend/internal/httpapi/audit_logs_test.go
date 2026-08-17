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

func TestAuditLogRouteRedactsSensitiveFields(t *testing.T) {
	store, err := repository.Open(filepath.Join(t.TempDir(), "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
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
	store.Audit(context.Background(), nil, &admin.ID, "project", "project", "updated", map[string]any{
		"repositoryPath": "/private/workspace", "label": "visible",
	})
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	login := func(email string) *http.Cookie {
		_, cookie, err := auth.Login(context.Background(), email, password, false)
		if err != nil {
			t.Fatal(err)
		}
		return cookie
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/admin/logs?level=info&limit=20", nil)
	adminRequest.AddCookie(login(admin.Email))
	adminResponse := httptest.NewRecorder()
	api.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body = %s", adminResponse.Code, adminResponse.Body.String())
	}
	body := adminResponse.Body.String()
	if !strings.Contains(body, "已隐藏") || strings.Contains(body, "/private/workspace") {
		t.Fatalf("sensitive audit value was not redacted: %s", body)
	}

	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/admin/logs", nil)
	viewerRequest.AddCookie(login(viewer.Email))
	viewerResponse := httptest.NewRecorder()
	api.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, body = %s", viewerResponse.Code, viewerResponse.Body.String())
	}
}

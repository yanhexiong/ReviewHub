package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

func TestCollaboratorRoutesEnforceProjectPermissions(t *testing.T) {
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
	users := []domain.User{
		{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1},
		{ID: "manager", Email: "manager@example.test", DisplayName: "Manager", PasswordHash: string(hash), Role: "reviewer", IsActive: 1},
		{ID: "reviewer", Email: "reviewer@example.test", DisplayName: "Reviewer", PasswordHash: string(hash), Role: "reviewer", IsActive: 1},
	}
	for _, user := range users {
		if err := store.CreateUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{
		ID: "project", Name: "Project", Slug: "project", RepositoryPath: "/tmp/project",
		SourcePDFPath: "/tmp/project/paper.pdf", CreatedByUserID: "owner",
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
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		recording := httptest.NewRecorder()
		input := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		input.Header.Set("Content-Type", "application/json")
		input.AddCookie(cookie)
		api.ServeHTTP(recording, input)
		return recording
	}

	managerAdded := request(http.MethodPost, "/api/projects/project/collaborators", `{"email":"manager@example.test","permission":"manage"}`, login("owner@example.test"))
	if managerAdded.Code != http.StatusOK {
		t.Fatalf("owner add manager status = %d, body = %s", managerAdded.Code, managerAdded.Body.String())
	}
	var ownerResult struct {
		Collaborators []map[string]any `json:"collaborators"`
	}
	if err := json.NewDecoder(managerAdded.Body).Decode(&ownerResult); err != nil {
		t.Fatal(err)
	}
	if len(ownerResult.Collaborators) != 1 || ownerResult.Collaborators[0]["permission"] != "manage" {
		t.Fatalf("unexpected collaborators: %#v", ownerResult.Collaborators)
	}

	managerEscalation := request(http.MethodPost, "/api/projects/project/collaborators", `{"email":"reviewer@example.test","permission":"manage"}`, login("manager@example.test"))
	if managerEscalation.Code != http.StatusForbidden {
		t.Fatalf("manager grant status = %d, body = %s", managerEscalation.Code, managerEscalation.Body.String())
	}

	reviewerAdded := request(http.MethodPost, "/api/projects/project/collaborators", `{"email":"reviewer@example.test","permission":"review"}`, login("owner@example.test"))
	if reviewerAdded.Code != http.StatusOK {
		t.Fatalf("owner add reviewer status = %d, body = %s", reviewerAdded.Code, reviewerAdded.Body.String())
	}
	reviewerRead := request(http.MethodGet, "/api/projects/project/collaborators", "", login("reviewer@example.test"))
	if reviewerRead.Code != http.StatusForbidden {
		t.Fatalf("reviewer list status = %d, body = %s", reviewerRead.Code, reviewerRead.Body.String())
	}

	removed := request(http.MethodDelete, "/api/projects/project/collaborators/reviewer", "", login("owner@example.test"))
	if removed.Code != http.StatusOK {
		t.Fatalf("owner remove reviewer status = %d, body = %s", removed.Code, removed.Body.String())
	}
}

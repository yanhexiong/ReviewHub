package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

func TestSnapshotListRouteUsesGoAuthorizationAndHidesPaths(t *testing.T) {
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
	owner := domain.User{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1}
	viewer := domain.User{ID: "viewer", Email: "viewer@example.test", DisplayName: "Viewer", PasswordHash: string(hash), Role: "reviewer", IsActive: 1}
	for _, user := range []domain.User{owner, viewer} {
		if err := store.CreateUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{
		ID: "project", Name: "Project", Slug: "project", RepositoryPath: "/tmp/project",
		SourcePDFPath: "/tmp/project/paper.pdf", CreatedByUserID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSnapshot(context.Background(), repository.NewSnapshot{
		ID: "snapshot", ProjectID: "project", VersionNumber: 1,
		OriginalPDFPath: "/private/source/paper.pdf", ArchivedPDFPath: "snapshots/project/paper.pdf",
		OriginalFileName: "paper.pdf", FileSizeBytes: 42, SHA256: "hash", PageCount: 1,
		ArchivedByUserID: owner.ID,
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

	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/snapshots", nil)
	ownerRequest.AddCookie(login(owner.Email))
	ownerResponse := httptest.NewRecorder()
	api.ServeHTTP(ownerResponse, ownerRequest)
	if ownerResponse.Code != http.StatusOK {
		t.Fatalf("owner status = %d, body = %s", ownerResponse.Code, ownerResponse.Body.String())
	}
	var body struct {
		Snapshots []map[string]any `json:"snapshots"`
	}
	if err := json.NewDecoder(ownerResponse.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Snapshots) != 1 {
		t.Fatalf("snapshot count = %d", len(body.Snapshots))
	}
	if _, exposed := body.Snapshots[0]["archived_pdf_path"]; exposed {
		t.Fatal("archived PDF path was exposed to the browser")
	}
	if body.Snapshots[0]["source_kind"] != "project" {
		t.Fatalf("source kind = %#v", body.Snapshots[0]["source_kind"])
	}

	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/snapshots", nil)
	viewerRequest.AddCookie(login(viewer.Email))
	viewerResponse := httptest.NewRecorder()
	api.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, body = %s", viewerResponse.Code, viewerResponse.Body.String())
	}

	updateRequest := httptest.NewRequest(http.MethodPatch, "/api/projects/project/snapshots/snapshot", bytes.NewBufferString(`{"label":"Reviewed version","note":"Prepared for reviewer"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.AddCookie(login(owner.Email))
	updateResponse := httptest.NewRecorder()
	api.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("owner update status = %d, body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	var updated struct {
		Snapshot map[string]any `json:"snapshot"`
	}
	if err := json.NewDecoder(updateResponse.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Snapshot["snapshot_label"] != "Reviewed version" {
		t.Fatalf("snapshot label = %#v", updated.Snapshot["snapshot_label"])
	}

	forbiddenUpdate := httptest.NewRequest(http.MethodPatch, "/api/projects/project/snapshots/snapshot", bytes.NewBufferString(`{"label":"No access"}`))
	forbiddenUpdate.Header.Set("Content-Type", "application/json")
	forbiddenUpdate.AddCookie(login(viewer.Email))
	forbiddenResponse := httptest.NewRecorder()
	api.ServeHTTP(forbiddenResponse, forbiddenUpdate)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer update status = %d, body = %s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}
}

func TestSnapshotListIncludesSafeProjectPDFFilesForManagers(t *testing.T) {
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
	owner := domain.User{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1}
	viewer := domain.User{ID: "viewer", Email: "viewer@example.test", DisplayName: "Viewer", PasswordHash: string(hash), Role: "reviewer", IsActive: 1}
	for _, user := range []domain.User{owner, viewer} {
		if err := store.CreateUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	repositoryPath := filepath.Join(t.TempDir(), "paper")
	if err := os.MkdirAll(filepath.Join(repositoryPath, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.pdf", "nested/appendix.PDF", "nested/notes.txt"} {
		if err := os.WriteFile(filepath.Join(repositoryPath, filepath.FromSlash(name)), []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{
		ID: "pdf-project", Name: "PDF Project", Slug: "pdf-project", RepositoryPath: repositoryPath,
		SourcePDFPath: filepath.Join(repositoryPath, "main.pdf"), CreatedByUserID: owner.ID,
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

	request := httptest.NewRequest(http.MethodGet, "/api/projects/pdf-project/snapshots?includePdfFiles=1", nil)
	request.AddCookie(login(owner.Email))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("manager status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		PDFFiles []struct {
			ID           string  `json:"id"`
			Label        string  `json:"label"`
			RelativePath *string `json:"relativePath"`
			Configured   bool    `json:"configured"`
		} `json:"pdfFiles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.PDFFiles) != 2 || !body.PDFFiles[0].Configured || body.PDFFiles[0].ID != "repo:main.pdf" {
		t.Fatalf("unexpected PDF options: %#v", body.PDFFiles)
	}
	if body.PDFFiles[1].RelativePath == nil || *body.PDFFiles[1].RelativePath != "nested/appendix.PDF" {
		t.Fatalf("relative PDF path = %#v", body.PDFFiles[1].RelativePath)
	}
	if strings.Contains(response.Body.String(), repositoryPath) {
		t.Fatal("absolute repository path was exposed")
	}

	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/projects/pdf-project/snapshots?includePdfFiles=1", nil)
	viewerRequest.AddCookie(login(viewer.Email))
	viewerResponse := httptest.NewRecorder()
	api.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, body = %s", viewerResponse.Code, viewerResponse.Body.String())
	}
}

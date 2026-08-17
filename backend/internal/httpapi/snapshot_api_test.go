package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
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

func TestSnapshotArchiveCompareDeleteAndSettingsRoutes(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	repositoryPath := filepath.Join(root, "paper")
	if err := os.MkdirAll(repositoryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(repositoryPath, "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4\n1 0 obj << /Type /Page >>\n%%EOF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := repository.Open(filepath.Join(data, "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{
		MaxPDFBytes: 2_000_000, MaxImportBytes: 2_000_000, MaxProjectsPerUser: 20, MaxUsers: 100,
		AllowedRoots: []string{root},
	}); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password-12"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	owner := domain.User{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1}
	if err := store.CreateUser(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{
		ID: "project", Name: "Project", Slug: "project", RepositoryPath: repositoryPath,
		SourcePDFPath: pdfPath, CreatedByUserID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetDataDirectory(data)
	login := func(request *http.Request) {
		_, cookie, loginErr := auth.Login(context.Background(), owner.Email, "test-password-12", false)
		if loginErr != nil {
			t.Fatal(loginErr)
		}
		request.AddCookie(cookie)
	}
	postJSON := func(value string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/projects/project/snapshots", strings.NewReader(value))
		request.Header.Set("Content-Type", "application/json")
		login(request)
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		return response
	}

	first := postJSON(`{"label":"first"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first archive status = %d, body = %s", first.Code, first.Body.String())
	}
	var firstBody struct {
		Snapshot map[string]any `json:"snapshot"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if firstBody.Snapshot["version_number"] != float64(1) || firstBody.Snapshot["page_count"] != float64(1) {
		t.Fatalf("unexpected first snapshot: %#v", firstBody.Snapshot)
	}
	archived, err := filepath.Glob(filepath.Join(data, "snapshots", "project", "*.pdf"))
	if err != nil || len(archived) != 1 {
		t.Fatalf("archive file count = %d, err = %v", len(archived), err)
	}
	duplicate := postJSON(`{}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4\n1 0 obj << /Type /Page >> changed\n%%EOF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := postJSON(`{}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("second archive status = %d, body = %s", second.Code, second.Body.String())
	}

	var secondBody struct {
		Snapshot map[string]any `json:"snapshot"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatal(err)
	}
	firstID := firstBody.Snapshot["id"].(string)
	secondID := secondBody.Snapshot["id"].(string)
	if secondBody.Snapshot["version_number"] != float64(2) {
		t.Fatalf("second version = %#v", secondBody.Snapshot["version_number"])
	}
	comment, err := store.CreateComment(context.Background(), "project", owner.ID, repository.NewComment{SnapshotID: firstID, PageNumber: 1, AnchorType: "page_note", Content: "check", Category: "general", Priority: "high"})
	if err != nil {
		t.Fatal(err)
	}
	_ = comment
	compareRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/compare?snapshotA="+firstID+"&snapshotB="+secondID, nil)
	login(compareRequest)
	compareResponse := httptest.NewRecorder()
	api.ServeHTTP(compareResponse, compareRequest)
	if compareResponse.Code != http.StatusOK {
		t.Fatalf("compare status = %d, body = %s", compareResponse.Code, compareResponse.Body.String())
	}
	var compareBody struct {
		Pages          []map[string]any `json:"pages"`
		MotherComments []map[string]any `json:"motherComments"`
	}
	if err := json.Unmarshal(compareResponse.Body.Bytes(), &compareBody); err != nil {
		t.Fatal(err)
	}
	if len(compareBody.Pages) != 1 || compareBody.Pages[0]["status"] != "modified" || len(compareBody.MotherComments) != 1 {
		t.Fatalf("unexpected compare body: %#v", compareBody)
	}

	settingsPut := httptest.NewRequest(http.MethodPut, "/api/projects/project/vscode/settings", strings.NewReader(`{"content":"[1]"}`))
	settingsPut.Header.Set("Content-Type", "application/json")
	login(settingsPut)
	settingsResponse := httptest.NewRecorder()
	api.ServeHTTP(settingsResponse, settingsPut)
	if settingsResponse.Code != http.StatusBadRequest {
		t.Fatalf("array settings status = %d, body = %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	settingsPut = httptest.NewRequest(http.MethodPut, "/api/projects/project/vscode/settings", strings.NewReader(`{"content":"{\"editor.fontSize\":14}"}`))
	settingsPut.Header.Set("Content-Type", "application/json")
	login(settingsPut)
	settingsResponse = httptest.NewRecorder()
	api.ServeHTTP(settingsResponse, settingsPut)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("settings status = %d, body = %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	settingsGet := httptest.NewRequest(http.MethodGet, "/api/projects/project/vscode/settings", nil)
	login(settingsGet)
	settingsResponse = httptest.NewRecorder()
	api.ServeHTTP(settingsResponse, settingsGet)
	if settingsResponse.Code != http.StatusOK || !strings.Contains(settingsResponse.Body.String(), "editor.fontSize") {
		t.Fatalf("settings GET status = %d, body = %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	stat, err := os.Stat(filepath.Join(data, "vscode-workspaces", "project", "user-data", "User", "settings.json"))
	if err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatalf("settings permissions = %v, err = %v", stat.Mode().Perm(), err)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/projects/project/snapshots/"+firstID, nil)
	login(deleteRequest)
	deleteResponse := httptest.NewRecorder()
	api.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if snapshot, err := store.SnapshotByID(context.Background(), "project", firstID); err != nil || snapshot != nil {
		t.Fatalf("deleted snapshot still present: %#v, err = %v", snapshot, err)
	}
}

func TestSnapshotMultipartManualGit(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	store, err := repository.Open(filepath.Join(data, "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{MaxPDFBytes: 2_000_000, MaxImportBytes: 2_000_000, MaxProjectsPerUser: 20, MaxUsers: 100, AllowedRoots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password-12"), bcrypt.DefaultCost)
	owner := domain.User{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1}
	if err := store.CreateUser(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{ID: "project", Name: "Project", Slug: "project", RepositoryPath: root, SourcePDFPath: "", CreatedByUserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetDataDirectory(data)
	requestBody := &bytes.Buffer{}
	writer := multipart.NewWriter(requestBody)
	part, err := writer.CreateFormFile("pdf", "uploaded.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("%PDF-1.4\n1 0 obj << /Type /Page >>\n%%EOF\n"))
	_ = writer.WriteField("gitAssociation", "manual")
	_ = writer.WriteField("gitCommitSha", "0123456789abcdef0123456789abcdef01234567")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/projects/project/snapshots", requestBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	_, cookie, err := auth.Login(context.Background(), owner.Email, "test-password-12", false)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("multipart status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"source_kind":"external"`) || !strings.Contains(response.Body.String(), `"has_git_source":false`) {
		t.Fatalf("unexpected multipart response: %s", response.Body.String())
	}
}

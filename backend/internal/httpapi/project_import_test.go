package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/importer"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

func TestCreateProjectArchiveUsesGoImporterAndProgress(t *testing.T) {
	_, api, password := newImportTestAPI(t)
	archive := makeZip(t, map[string][]byte{
		"paper/main.tex": []byte("\\documentclass{article}\n"),
		"paper/main.pdf": []byte("%PDF-1.7\nplaceholder"),
	})
	body, contentType := multipartArchiveBody(t, archive)
	request := httptest.NewRequest(http.MethodPost, "/api/projects", body)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-Review-Hub-Progress-ID", "123e4567-e89b-12d3-a456-426614174000")
	request.AddCookie(loginCookie(t, api.auth, "owner@example.test", password))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Project map[string]any `json:"project"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Project["source_type"] != "archive" || result.Project["pdf_configured"] != true {
		t.Fatalf("unexpected imported project = %#v", result.Project)
	}

	progress := httptest.NewRequest(http.MethodGet, "/api/projects/progress/123e4567-e89b-12d3-a456-426614174000", nil)
	progress.AddCookie(loginCookie(t, api.auth, "owner@example.test", password))
	progressResponse := httptest.NewRecorder()
	api.ServeHTTP(progressResponse, progress)
	if progressResponse.Code != http.StatusOK || !bytes.Contains(progressResponse.Body.Bytes(), []byte(`"done":true`)) {
		t.Fatalf("progress status = %d, body = %s", progressResponse.Code, progressResponse.Body.String())
	}
}

func TestCreateProjectArchiveReturnsPendingWhenPDFIsMissing(t *testing.T) {
	store, api, password := newImportTestAPI(t)
	archive := makeZip(t, map[string][]byte{"paper/main.tex": []byte("\\documentclass{article}\n")})
	body, contentType := multipartArchiveBody(t, archive)
	request := httptest.NewRequest(http.MethodPost, "/api/projects", body)
	request.Header.Set("Content-Type", contentType)
	request.AddCookie(loginCookie(t, api.auth, "owner@example.test", password))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Pending struct {
			ID string `json:"id"`
		} `json:"pending"`
		TexCandidates []string `json:"texCandidates"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Pending.ID == "" || len(result.TexCandidates) != 1 || result.TexCandidates[0] != "main.tex" {
		t.Fatalf("unexpected pending result = %#v", result)
	}
	pending, err := store.PendingProject(context.Background(), result.Pending.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePendingProject(context.Background(), result.Pending.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(pending.WorkspacePath); err != nil {
		t.Fatal(err)
	}
}

func newImportTestAPI(t *testing.T) (*repository.Store, *API, string) {
	t.Helper()
	dataDirectory := t.TempDir()
	store, err := repository.Open(filepath.Join(dataDirectory, "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{MaxPDFBytes: 2_000_000, MaxImportBytes: 3_000_000, MaxProjectsPerUser: 20, MaxUsers: 100, AllowedRoots: []string{dataDirectory}}); err != nil {
		t.Fatal(err)
	}
	password := "test-password-12"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUser(context.Background(), domain.User{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetDataDirectory(dataDirectory)
	api.SetImporter(importer.New(dataDirectory, 3_000_000))
	return store, api, password
}

func loginCookie(t *testing.T, auth *service.AuthService, email, password string) *http.Cookie {
	t.Helper()
	_, cookie, err := auth.Login(context.Background(), email, password, false)
	if err != nil {
		t.Fatal(err)
	}
	return cookie
}

func multipartArchiveBody(t *testing.T, archive []byte) (*bytes.Reader, string) {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := multipart.NewWriter(buffer)
	fields := map[string]string{"name": "Imported paper", "slug": "imported-paper", "description": "", "sourceType": "archive", "sourcePdfPath": "main.pdf"}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="repositoryArchive"; filename="paper.zip"`)
	header.Set("Content-Type", "application/zip")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buffer.Bytes()), writer.FormDataContentType()
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	for name, data := range files {
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

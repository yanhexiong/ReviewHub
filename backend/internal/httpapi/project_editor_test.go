package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

func TestProjectEditorListsReadsAndWritesOnlySafeTextFiles(t *testing.T) {
	store, api, password := newImportTestAPI(t)
	workspace := filepath.Join(api.dataDirectory, "workspaces", "editor-project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.tex"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "notes.bin"), []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{ID: "editor-project", Name: "Editor", Slug: "editor", RepositoryPath: relativeWorkingPath(workspace), SourcePDFPath: relativeWorkingPath(filepath.Join(workspace, "main.pdf")), CreatedByUserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, api.auth, "owner@example.test", password)
	listRequest := httptest.NewRequest(http.MethodGet, "/api/projects/editor-project/editor", nil)
	listRequest.AddCookie(cookie)
	listResponse := httptest.NewRecorder()
	api.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	if bytes.Contains(listResponse.Body.Bytes(), []byte("notes.bin")) || !bytes.Contains(listResponse.Body.Bytes(), []byte("main.tex")) {
		t.Fatalf("unexpected editor file list: %s", listResponse.Body.String())
	}

	readRequest := httptest.NewRequest(http.MethodGet, "/api/projects/editor-project/editor?path=main.tex", nil)
	readRequest.AddCookie(cookie)
	readResponse := httptest.NewRecorder()
	api.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK || !bytes.Contains(readResponse.Body.Bytes(), []byte(`"content":"old"`)) {
		t.Fatalf("read status = %d, body = %s", readResponse.Code, readResponse.Body.String())
	}

	writeRequest := httptest.NewRequest(http.MethodPut, "/api/projects/editor-project/editor", bytes.NewBufferString(`{"path":"main.tex","content":"updated"}`))
	writeRequest.Header.Set("Content-Type", "application/json")
	writeRequest.AddCookie(cookie)
	writeResponse := httptest.NewRecorder()
	api.ServeHTTP(writeResponse, writeRequest)
	if writeResponse.Code != http.StatusOK || !bytes.Contains(writeResponse.Body.Bytes(), []byte(`"sha256"`)) {
		t.Fatalf("write status = %d, body = %s", writeResponse.Code, writeResponse.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(workspace, "main.tex"))
	if err != nil || string(content) != "updated" {
		t.Fatalf("saved content = %q, error = %v", content, err)
	}

	for _, path := range []string{"../secret.tex", "notes.bin"} {
		request := httptest.NewRequest(http.MethodGet, "/api/projects/editor-project/editor?path="+path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusForbidden {
			t.Fatalf("unsafe path %q status = %d, body = %s", path, response.Code, response.Body.String())
		}
	}

	var audit struct {
		File map[string]any `json:"file"`
	}
	if err := json.Unmarshal(writeResponse.Body.Bytes(), &audit); err != nil || audit.File["path"] != "main.tex" {
		t.Fatalf("unexpected write response = %s", writeResponse.Body.String())
	}
}

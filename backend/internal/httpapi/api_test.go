package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeHTTPPreservesDynamicPathValues(t *testing.T) {
	api := &API{
		mux:    http.NewServeMux(),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	api.mux.HandleFunc("GET /api/test/{value}", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"value": request.PathValue("value")})
	})

	request := httptest.NewRequest(http.MethodGet, "/api/test/expected-value", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Body.String() != "{\"value\":\"expected-value\"}\n" {
		t.Fatalf("path value was not forwarded: %s", response.Body.String())
	}
}

func TestServeHTTPRejectsUnregisteredAPIWithoutFallback(t *testing.T) {
	api := New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	request := httptest.NewRequest(http.MethodGet, "/api/not-migrated", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"API_NOT_FOUND"`) {
		t.Fatalf("missing not-found error: %s", response.Body.String())
	}
}

func TestServeHTTPServesStaticRoutesAndDynamicPlaceholders(t *testing.T) {
	root := t.TempDir()
	for relative, contents := range map[string]string{
		"index.html":       "home",
		"login/index.html": "login",
		"projects/__review_hub_project__/review/index.html": "review",
		"share/__review_hub_share__/index.html":             "share",
		"_next/static/chunks/main.js":                       "asset",
	} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	api := New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetStaticDirectory(root)

	tests := map[string]string{
		"/":                            "home",
		"/login":                       "login",
		"/login/":                      "login",
		"/projects/project-123/review": "review",
		"/share/token-value":           "share",
		"/_next/static/chunks/main.js": "asset",
	}
	for requestPath, expected := range tests {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != expected {
			t.Errorf("%s: status/body = %d/%q, want 200/%q", requestPath, response.Code, response.Body.String(), expected)
		}
	}
}

func TestServeHTTPRejectsUnsafeStaticPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := New(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetStaticDirectory(root)
	request := httptest.NewRequest(http.MethodGet, "/../secret.txt", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unsafe path status = %d, body = %s", response.Code, response.Body.String())
	}
}

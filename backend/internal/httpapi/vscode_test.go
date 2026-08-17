package httpapi

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
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

type vscodeTestFixture struct {
	api     *API
	owner   *http.Cookie
	viewer  *http.Cookie
	root    string
	cleanup func()
}

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func newVscodeTestFixture(t *testing.T) vscodeTestFixture {
	t.Helper()
	dataDirectory := t.TempDir()
	store, err := repository.Open(filepath.Join(dataDirectory, "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
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
	root := filepath.Join(dataDirectory, "source")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{ID: "project", Name: "Project", Slug: "project", RepositoryPath: root, SourcePDFPath: filepath.Join(root, "paper.pdf"), CreatedByUserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetDataDirectory(dataDirectory)
	_, ownerCookie, err := auth.Login(context.Background(), owner.Email, password, false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerCookie, err := auth.Login(context.Background(), viewer.Email, password, false)
	if err != nil {
		t.Fatal(err)
	}
	return vscodeTestFixture{api: api, owner: ownerCookie, viewer: viewerCookie, root: root, cleanup: func() { api.Close(); _ = store.Close() }}
}

func TestVscodeStatusRequiresManageAndReportsUnavailable(t *testing.T) {
	fixture := newVscodeTestFixture(t)
	defer fixture.cleanup()

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/projects/project/vscode", nil)
	unauthorizedResponse := httptest.NewRecorder()
	fixture.api.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body = %s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	forbidden := httptest.NewRequest(http.MethodGet, "/api/projects/project/vscode", nil)
	forbidden.AddCookie(fixture.viewer)
	forbiddenResponse := httptest.NewRecorder()
	fixture.api.ServeHTTP(forbiddenResponse, forbidden)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d, body = %s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}

	t.Setenv("PAPER_REVIEW_VSCODE_SERVER_BIN", filepath.Join(t.TempDir(), "missing-code-server"))
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project/vscode", nil)
	request.AddCookie(fixture.owner)
	response := httptest.NewRecorder()
	fixture.api.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "VSCODE_SERVER_UNAVAILABLE") {
		t.Fatalf("unavailable status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestVscodeProxyRewritesPathRedirectCookieAndWebSocket(t *testing.T) {
	fixture := newVscodeTestFixture(t)
	defer fixture.cleanup()

	upstream := newIPv4TestServer(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/redirect" || request.URL.RawQuery != "value=1" {
			t.Errorf("upstream path = %q?%s", request.URL.Path, request.URL.RawQuery)
		}
		if request.Header.Get("Cookie") != "code-server-session=proxy-token" {
			t.Errorf("upstream cookie = %q", request.Header.Get("Cookie"))
		}
		response.Header().Add("Set-Cookie", "session=upstream; Path=/; HttpOnly")
		response.Header().Set("Location", "/login")
		response.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	manager := fixture.api.vscode()
	manager.mu.Lock()
	manager.instances["project"] = &vscodeInstance{projectID: "project", repositoryPath: fixture.root, port: port, authCookie: "proxy-token", done: make(chan struct{})}
	manager.mu.Unlock()

	server := newIPv4TestServer(t, fixture.api)
	defer server.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequest(http.MethodGet, server.URL+"/vscode/projects/project/redirect?value=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(fixture.owner)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound || response.Header.Get("Location") != "/vscode/projects/project/login" {
		t.Fatalf("redirect status/location = %d/%q", response.StatusCode, response.Header.Get("Location"))
	}
	if cookie := response.Header.Get("Set-Cookie"); !strings.Contains(cookie, "Path=/vscode/projects/project/") {
		t.Fatalf("rewritten cookie = %q", cookie)
	}

	wsUpstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wsUpstream.Close()
	go func() {
		connection, acceptErr := wsUpstream.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		request, readErr := http.ReadRequest(bufio.NewReader(connection))
		if readErr != nil {
			t.Errorf("read websocket request: %v", readErr)
			return
		}
		if request.URL.Path != "/ws" || request.Header.Get("Cookie") != "code-server-session=proxy-token" {
			t.Errorf("websocket upstream request path=%q cookie=%q", request.URL.Path, request.Header.Get("Cookie"))
		}
		_, _ = io.WriteString(connection, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	}()
	manager.mu.Lock()
	manager.instances["project"].port = wsUpstream.Addr().(*net.TCPAddr).Port
	manager.mu.Unlock()
	address := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = io.WriteString(connection, "GET /vscode/projects/project/ws HTTP/1.1\r\nHost: "+address+"\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: "+fixture.owner.Name+"="+fixture.owner.Value+"\r\n\r\n")
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("websocket status = %q", status)
	}
}

func TestConfigureVscodeUserDataMergesSettingsAndDisablesTerminal(t *testing.T) {
	fixture := newVscodeTestFixture(t)
	defer fixture.cleanup()
	userData := filepath.Join(t.TempDir(), "user-data")
	settingsPath := filepath.Join(userData, "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"editor.fontSize\": 15}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.api.configureVscodeUserData(context.Background(), userData, "project"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "editor.fontSize") || !strings.Contains(string(raw), "review-hub-disabled") {
		t.Fatalf("settings were not merged safely: %s", raw)
	}
}

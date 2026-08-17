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

func TestAdminSettingsAreOwnedByGoAPI(t *testing.T) {
	store, err := repository.Open(filepath.Join(t.TempDir(), "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SeedPlatformDefaults(context.Background(), repository.PlatformDefaults{
		MaxPDFBytes: 2_000_000, MaxImportBytes: 3_000_000,
		MaxProjectsPerUser: 20, MaxUsers: 100, AllowedRoots: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("password-with-12"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	admin := domain.User{
		ID: "admin-1", Email: "admin@example.test", DisplayName: "admin",
		PasswordHash: string(hash), Role: "admin", IsActive: 1,
	}
	if err := store.CreateUser(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "shared-session-secret", false, 100)
	_, cookie, err := auth.Login(context.Background(), admin.Email, "password-with-12", false)
	if err != nil {
		t.Fatal(err)
	}
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	var writtenHost string
	var writtenPort int
	api.SetListenerWriter(func(host string, port int) error {
		writtenHost = host
		writtenPort = port
		return nil
	})

	get := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	get.AddCookie(cookie)
	getResponse := httptest.NewRecorder()
	api.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var before map[string]any
	if err := json.NewDecoder(getResponse.Body).Decode(&before); err != nil {
		t.Fatal(err)
	}
	if before["maxUsers"] != float64(100) || before["registrationOpen"] != true {
		t.Fatalf("unexpected initial settings: %#v", before)
	}

	body := []byte(`{"registrationOpen":false,"maxPdfBytes":3000000,"maxImportBytes":4000000,"maxProjectsPerUser":2,"maxUsers":7}`)
	patch := httptest.NewRequest(http.MethodPatch, "/api/admin/settings", bytes.NewReader(body))
	patch.Header.Set("Content-Type", "application/json")
	patch.AddCookie(cookie)
	patchResponse := httptest.NewRecorder()
	api.ServeHTTP(patchResponse, patch)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body = %s", patchResponse.Code, patchResponse.Body.String())
	}
	settings, err := store.ResourceSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.MaxUsers != 7 || settings.MaxProjectsPerUser != 2 {
		t.Fatalf("settings were not persisted: %#v", settings)
	}
	open, err := store.RegistrationOpen(context.Background())
	if err != nil || open {
		t.Fatalf("registrationOpen = %v, err = %v", open, err)
	}
	listenerPatch := httptest.NewRequest(http.MethodPatch, "/api/admin/listener", bytes.NewReader([]byte(`{"host":"127.0.0.1","port":4321}`)))
	listenerPatch.Header.Set("Content-Type", "application/json")
	listenerPatch.AddCookie(cookie)
	listenerResponse := httptest.NewRecorder()
	api.ServeHTTP(listenerResponse, listenerPatch)
	if listenerResponse.Code != http.StatusOK {
		t.Fatalf("listener PATCH status = %d, body = %s", listenerResponse.Code, listenerResponse.Body.String())
	}
	if writtenHost != "127.0.0.1" || writtenPort != 4321 {
		t.Fatalf("listener writer received %q:%d", writtenHost, writtenPort)
	}
}

func TestSetupWritesFrontendListener(t *testing.T) {
	store, err := repository.Open(filepath.Join(t.TempDir(), "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	auth := service.NewAuthService(store, "shared-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	var writtenHost string
	var writtenPort int
	api.SetListenerWriter(func(host string, port int) error {
		writtenHost = host
		writtenPort = port
		return nil
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/setup",
		bytes.NewBufferString(`{"email":"admin@example.test","displayName":"Administrator","password":"password-with-12","confirmPassword":"password-with-12","listener":{"host":"localhost","port":4321}}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.Code, response.Body.String())
	}
	if writtenHost != "localhost" || writtenPort != 4321 {
		t.Fatalf("listener writer received %q:%d", writtenHost, writtenPort)
	}
	listener, err := store.ListenerSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listener.Host != "localhost" || listener.Port != 4321 {
		t.Fatalf("listener settings = %#v", listener)
	}
}

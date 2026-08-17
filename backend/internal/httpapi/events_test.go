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
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
}

func (recorder *flushRecorder) Flush() {
	select {
	case recorder.flushed <- struct{}{}:
	default:
	}
}

func TestProjectEventsFlushesConnectedEvent(t *testing.T) {
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
	if err := store.CreateUser(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{
		ID: "project", Name: "Project", Slug: "project", RepositoryPath: "/tmp/project",
		SourcePDFPath: "/tmp/project/paper.pdf", CreatedByUserID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	_, cookie, err := auth.Login(context.Background(), owner.Email, password, false)
	if err != nil {
		t.Fatal(err)
	}
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project/events", nil).WithContext(ctx)
	request.AddCookie(cookie)
	response := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		api.ServeHTTP(response, request)
		close(done)
	}()
	select {
	case <-response.flushed:
	case <-time.After(time.Second):
		t.Fatal("SSE connection did not flush the connected event")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop after request cancellation")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "event: connected") {
		t.Fatalf("missing connected event: %s", response.Body.String())
	}
}

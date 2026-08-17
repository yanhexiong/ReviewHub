package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
)

func TestProjectExportRoutesGenerateRecordsAndBundleInGo(t *testing.T) {
	dataDirectory := t.TempDir()
	store, err := repository.Open(filepath.Join(dataDirectory, "review-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password-12"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	owner := domain.User{ID: "owner", Email: "owner@example.test", DisplayName: "Owner", PasswordHash: string(hash), Role: "author", IsActive: 1}
	if err := store.CreateUser(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(context.Background(), repository.NewProject{ID: "project", Name: "Project", Slug: "project", RepositoryPath: dataDirectory, SourcePDFPath: filepath.Join(dataDirectory, "main.pdf"), CreatedByUserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(dataDirectory, "snapshots", "project", "000001-test.pdf")
	if err := os.MkdirAll(filepath.Dir(pdfPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdfPath, testPDF(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSnapshot(context.Background(), repository.NewSnapshot{ID: "snapshot", ProjectID: "project", VersionNumber: 1, OriginalPDFPath: "main.pdf", ArchivedPDFPath: "snapshots/project/000001-test.pdf", OriginalFileName: "paper.pdf", FileSizeBytes: 20, SHA256: "hash", PageCount: 1, ArchivedByUserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateComment(context.Background(), "project", owner.ID, repository.NewComment{SnapshotID: "snapshot", PageNumber: 1, AnchorType: "page_note", Content: "Check this paragraph", Category: "content", Priority: "normal"}); err != nil {
		t.Fatal(err)
	}
	auth := service.NewAuthService(store, "test-session-secret", false, 100)
	api := New(store, auth, service.NewReviewService(store, realtime.NewHub()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetDataDirectory(dataDirectory)
	_, cookie, err := auth.Login(context.Background(), owner.Email, "test-password-12", false)
	if err != nil {
		t.Fatal(err)
	}

	jsonRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/export?format=json&snapshotId=snapshot", nil)
	jsonRequest.AddCookie(cookie)
	jsonResponse := httptest.NewRecorder()
	api.ServeHTTP(jsonResponse, jsonRequest)
	if jsonResponse.Code != http.StatusOK || jsonResponse.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("json status = %d, content-type = %q, body = %s", jsonResponse.Code, jsonResponse.Header().Get("Content-Type"), jsonResponse.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(jsonResponse.Body.Bytes(), &payload); err != nil || payload["project"] == nil {
		t.Fatalf("invalid JSON export: %s", jsonResponse.Body.String())
	}
	if strings.Contains(jsonResponse.Body.String(), "snapshots/project/000001-test.pdf") {
		t.Fatal("JSON export exposed the managed PDF path")
	}

	for _, format := range []string{"markdown", "csv"} {
		request := httptest.NewRequest(http.MethodGet, "/api/projects/project/export?format="+format, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("%s export status = %d, body = %s", format, response.Code, response.Body.String())
		}
	}

	bundleRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/export?format=bundle&snapshotId=snapshot", nil)
	bundleRequest.AddCookie(cookie)
	bundleResponse := httptest.NewRecorder()
	api.ServeHTTP(bundleResponse, bundleRequest)
	if bundleResponse.Code != http.StatusOK {
		t.Fatalf("bundle status = %d, body = %s", bundleResponse.Code, bundleResponse.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(bundleResponse.Body.Bytes()), int64(bundleResponse.Body.Len()))
	if err != nil || len(archive.File) != 2 {
		t.Fatalf("invalid bundle: err=%v files=%d", err, len(archive.File))
	}

	annotatedRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/export?format=annotated-pdf&snapshotId=snapshot", nil)
	annotatedRequest.AddCookie(cookie)
	annotatedResponse := httptest.NewRecorder()
	api.ServeHTTP(annotatedResponse, annotatedRequest)
	if annotatedResponse.Code != http.StatusOK || annotatedResponse.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("annotated PDF status = %d, content-type = %q, body = %s", annotatedResponse.Code, annotatedResponse.Header().Get("Content-Type"), annotatedResponse.Body.String())
	}
	if err := pdfapi.Validate(bytes.NewReader(annotatedResponse.Body.Bytes()), model.NewDefaultConfiguration()); err != nil {
		t.Fatalf("annotated PDF is not parseable: %v", err)
	}
	annotations, err := pdfapi.Annotations(bytes.NewReader(annotatedResponse.Body.Bytes()), nil, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatalf("read annotated PDF annotations: %v", err)
	}
	if len(annotations[1][model.AnnText].Map) == 0 {
		t.Fatal("annotated PDF contains no review annotation or page-note fallback")
	}

	outsider := domain.User{ID: "outsider", Email: "outsider@example.test", DisplayName: "Outsider", PasswordHash: string(hash), Role: "author", IsActive: 1}
	if err := store.CreateUser(context.Background(), outsider); err != nil {
		t.Fatal(err)
	}
	_, outsiderCookie, err := auth.Login(context.Background(), outsider.Email, "test-password-12", false)
	if err != nil {
		t.Fatal(err)
	}
	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/api/projects/project/export?format=annotated-pdf&snapshotId=snapshot", nil)
	forbiddenRequest.AddCookie(outsiderCookie)
	forbiddenResponse := httptest.NewRecorder()
	api.ServeHTTP(forbiddenResponse, forbiddenRequest)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("annotated PDF export without project access status = %d, body = %s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}
}

func testPDF() []byte {
	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R >>\nendobj\n",
		"4 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj\n",
	}
	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = document.Len()
		document.WriteString(object)
	}
	xrefOffset := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	return document.Bytes()
}

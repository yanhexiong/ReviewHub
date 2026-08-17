package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

func TestImportArchiveStripsCommonRootAndPreservesGitMetadata(t *testing.T) {
	dataDirectory := t.TempDir()
	service := New(dataDirectory, 1<<20)
	archive := zipBytes(t, map[string]string{
		"paper-main/.git/HEAD": "ref: refs/heads/main\n",
		"paper-main/main.tex":  "\\documentclass{article}\n",
		"paper-main/README.md": "# paper\n",
	})
	workspace, err := service.ImportArchive(context.Background(), "12345678-1234-1234-1234-123456789abc", bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("ImportArchive() error = %v", err)
	}
	if workspace != filepath.Join(dataDirectory, "workspaces", "12345678-1234-1234-1234-123456789abc") {
		t.Fatalf("workspace = %q", workspace)
	}
	for _, name := range []string{"main.tex", "README.md", ".git/HEAD"} {
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(name))); err != nil {
			t.Fatalf("imported %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "paper-main")); !os.IsNotExist(err) {
		t.Fatalf("common archive root was not stripped, stat error = %v", err)
	}
}

func TestImportArchiveRejectsTraversal(t *testing.T) {
	service := New(t.TempDir(), 1<<20)
	archive := zipBytes(t, map[string]string{"../../outside.txt": "unsafe"})
	_, err := service.ImportArchive(context.Background(), "12345678-1234-1234-1234-123456789abc", bytes.NewReader(archive), int64(len(archive)))
	assertAPIErrorCode(t, err, "INVALID_ARCHIVE_PATH")
}

func TestImportArchiveRejectsSymlink(t *testing.T) {
	dataDirectory := t.TempDir()
	service := New(dataDirectory, 1<<20)
	archivePath := filepath.Join(t.TempDir(), "symlink.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "paper/link", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	// Unix mode bits mark this entry as a symbolic link. The content is never
	// extracted because repository imports do not permit links.
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("target")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ImportArchive(context.Background(), "12345678-1234-1234-1234-123456789abc", bytes.NewReader(archive), int64(len(archive)))
	assertAPIErrorCode(t, err, "UNSAFE_REPOSITORY_ENTRY")
}

func TestGitHubURLAndBranchValidation(t *testing.T) {
	if canonical, owner, repository, err := parseGitHubURL("https://github.com/yanhexiong/paper_26.git"); err != nil || canonical != "https://github.com/yanhexiong/paper_26" || owner != "yanhexiong" || repository != "paper_26" {
		t.Fatalf("parseGitHubURL() = %q, %q, %q, %v", canonical, owner, repository, err)
	}
	if _, _, _, err := parseGitHubURL("http://github.com/owner/repository"); err == nil {
		t.Fatal("parseGitHubURL accepted non-HTTPS URL")
	}
	if validBranch("../main") || validBranch("main..test") || validBranch("-main") {
		t.Fatal("validBranch accepted an unsafe branch")
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buffer := new(bytes.Buffer)
	writer := zip.NewWriter(buffer)
	for name, contents := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func assertAPIErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected API error %s", code)
	}
	apiError, ok := err.(*domain.APIError)
	if !ok || apiError.Code != code {
		t.Fatalf("error = %T %v, want code %s", err, err, code)
	}
}

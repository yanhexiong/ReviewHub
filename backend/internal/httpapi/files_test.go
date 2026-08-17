package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedDataPathRejectsArchiveSymlinkOutsideDataRoot(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(data, "snapshots"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.pdf")
	if err := os.WriteFile(outside, []byte("%PDF-1.7\n%%EOF\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(data, "snapshots", "linked.pdf")); err != nil {
		t.Fatal(err)
	}
	api := &API{dataDirectory: data}
	if _, err := api.managedDataPath("snapshots/linked.pdf"); err == nil {
		t.Fatal("archive symlink outside data root was accepted")
	}
}

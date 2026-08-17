package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallUpdateAndRestorePrevious(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "ReviewHub.AppImage")
	source := filepath.Join(directory, "downloaded.AppImage")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	backup := target + ".backup"
	previous, err := installUpdate(source, target, backup)
	if err != nil {
		t.Fatal(err)
	}
	if current, _ := os.ReadFile(target); string(current) != "new" {
		t.Fatalf("current AppImage = %q, want new", current)
	}
	if old, _ := os.ReadFile(backup); string(old) != "old" {
		t.Fatalf("backup AppImage = %q, want old", old)
	}
	if err := restorePrevious(target, previous, backup, false); err != nil {
		t.Fatal(err)
	}
	if current, _ := os.ReadFile(target); string(current) != "old" {
		t.Fatalf("restored AppImage = %q, want old", current)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("failed update must not remain a rollback candidate: %v", err)
	}
}

func TestSwapWithBackup(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "ReviewHub.AppImage")
	backup := target + ".backup"
	if err := os.WriteFile(target, []byte("current"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("previous"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := swapWithBackup(target, backup); err != nil {
		t.Fatal(err)
	}
	if current, _ := os.ReadFile(target); string(current) != "previous" {
		t.Fatalf("current AppImage = %q, want previous", current)
	}
	if preserved, _ := os.ReadFile(backup); string(preserved) != "current" {
		t.Fatalf("backup AppImage = %q, want current", preserved)
	}
}

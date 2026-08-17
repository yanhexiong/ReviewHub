package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (api *API) adminBackup(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.Checkpoint(request.Context()); err != nil {
		writeError(response, err)
		return
	}
	archive, err := api.createBackupArchive()
	if err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "system", "backup", "created", nil)
	response.Header().Set("Content-Type", "application/zip")
	response.Header().Set("Content-Disposition", `attachment; filename="review-hub-backup-`+time.Now().UTC().Format("20060102T150405Z")+`.zip"`)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(archive)
}

func (api *API) createBackupArchive() ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := []string{}
	databasePath := filepath.Join(api.dataDirectory, "paper-review.db")
	if err := addZipFile(writer, databasePath, "paper-review.db"); err != nil {
		return nil, err
	}
	files = append(files, "paper-review.db")
	ignored := map[string]bool{"backups": true, "logs": true, "temp": true, "updates": true}
	err := filepath.WalkDir(api.dataDirectory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == api.dataDirectory {
			return nil
		}
		relative, err := filepath.Rel(api.dataDirectory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		parts := strings.Split(relative, "/")
		if ignored[parts[0]] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if relative == "paper-review.db" || strings.HasSuffix(relative, "-wal") || strings.HasSuffix(relative, "-shm") {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if err := addZipFile(writer, path, relative); err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	sort.Strings(files)
	manifest, err := json.MarshalIndent(map[string]any{"format": "review-hub-backup", "formatVersion": 1, "createdAt": time.Now().UTC().Format(time.RFC3339), "schemaVersion": 12, "files": files}, "", "  ")
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	entry, err := writer.Create("manifest.json")
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	if _, err := entry.Write(manifest); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func addZipFile(writer *zip.Writer, path, name string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	stat, err := source.Stat()
	if err != nil {
		return err
	}
	header.SetModTime(stat.ModTime())
	destination, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(destination, source)
	return err
}

package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

const currentSchemaVersion = 12

type restoreManifest struct {
	Format        string   `json:"format"`
	FormatVersion int      `json:"formatVersion"`
	CreatedAt     string   `json:"createdAt"`
	SchemaVersion int      `json:"schemaVersion"`
	Files         []string `json:"files"`
}

func (api *API) adminRestore(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.ResourceSettings(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, int64(settings.MaxImportBytes)+2<<20)
	if err := request.ParseMultipartForm(int64(settings.MaxImportBytes) + 2<<20); err != nil {
		writeError(response, domain.NewAPIError(400, "BACKUP_INVALID", "迁移包上传过大或格式无效"))
		return
	}
	file, header, err := request.FormFile("backup")
	if err != nil || header == nil {
		file, header, err = request.FormFile("migration")
	}
	if err != nil || header == nil || header.Size == 0 {
		writeError(response, domain.NewAPIError(400, "BACKUP_REQUIRED", "请选择迁移包 ZIP 文件"))
		return
	}
	defer file.Close()
	archiveBytes, err := io.ReadAll(io.LimitReader(file, int64(settings.MaxImportBytes)+1))
	if err != nil {
		writeError(response, domain.NewAPIError(400, "BACKUP_INVALID", "无法读取迁移包"))
		return
	}
	if len(archiveBytes) > settings.MaxImportBytes {
		writeError(response, domain.NewAPIError(413, "BACKUP_TOO_LARGE", "迁移包超过允许大小"))
		return
	}

	api.operationMu.Lock()
	defer api.operationMu.Unlock()
	result, err := api.restoreBackupArchive(request.Context(), archiveBytes, actor.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"ok": true, "manifest": result.Manifest, "preRestoreBackup": result.PreRestoreBackup})
}

type restoreResult struct {
	Manifest         restoreManifest
	PreRestoreBackup string
}

func (api *API) restoreBackupArchive(ctx context.Context, archiveBytes []byte, actorID string) (restoreResult, error) {
	settings, err := api.store.ResourceSettings(ctx)
	if err != nil {
		return restoreResult{}, err
	}
	if len(archiveBytes) > settings.MaxImportBytes {
		return restoreResult{}, domain.NewAPIError(413, "BACKUP_TOO_LARGE", "迁移包超过允许大小")
	}
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return restoreResult{}, domain.NewAPIError(400, "BACKUP_INVALID", "迁移包不是有效 ZIP 文件")
	}
	entries := make(map[string]*zip.File, len(archive.File))
	var unpacked int64
	for _, entry := range archive.File {
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return restoreResult{}, domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "迁移包不允许包含符号链接")
		}
		name, err := safeArchivePath(entry.Name)
		if err != nil {
			return restoreResult{}, err
		}
		if _, exists := entries[name]; exists {
			return restoreResult{}, domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "迁移包包含重复路径")
		}
		if len(entries) >= 50_000 {
			return restoreResult{}, domain.NewAPIError(413, "BACKUP_TOO_MANY_FILES", "迁移包文件数量过多")
		}
		unpacked += int64(entry.UncompressedSize64)
		if unpacked > int64(settings.MaxImportBytes)*3 {
			return restoreResult{}, domain.NewAPIError(413, "BACKUP_TOO_LARGE", "迁移包解压后超过允许大小")
		}
		entries[name] = entry
	}
	manifestEntry, ok := entries["manifest.json"]
	if !ok {
		return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP", "迁移包缺少 manifest.json")
	}
	manifestBytes, err := readZipEntry(manifestEntry, 1<<20)
	if err != nil {
		return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP", "迁移包清单无法读取")
	}
	var manifest restoreManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.Format != "review-hub-backup" || manifest.FormatVersion != 1 || manifest.SchemaVersion < 1 || manifest.SchemaVersion > currentSchemaVersion || len(manifest.Files) == 0 {
		return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP_VERSION", "不支持的迁移包版本")
	}
	manifestFiles := make(map[string]struct{}, len(manifest.Files))
	for _, name := range manifest.Files {
		normalized, pathErr := safeArchivePath(name)
		if pathErr != nil {
			return restoreResult{}, pathErr
		}
		manifestFiles[normalized] = struct{}{}
	}
	if _, ok := manifestFiles["paper-review.db"]; !ok {
		return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP", "迁移包清单缺少 paper-review.db")
	}
	databaseEntry, ok := entries["paper-review.db"]
	if !ok {
		return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP", "迁移包缺少 paper-review.db")
	}

	dataDirectory := api.dataDirectory
	if strings.TrimSpace(dataDirectory) == "" {
		return restoreResult{}, domain.NewAPIError(500, "INTERNAL_ERROR", "数据目录不可用")
	}
	tempRoot := filepath.Join(dataDirectory, "temp", "restore-"+uuid.NewString())
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return restoreResult{}, err
	}
	defer os.RemoveAll(tempRoot)
	importedDatabase := filepath.Join(tempRoot, "paper-review.db")
	importedData := filepath.Join(tempRoot, "data")
	if err := os.MkdirAll(importedData, 0o700); err != nil {
		return restoreResult{}, err
	}
	if err := copyZipEntry(databaseEntry, importedDatabase, 0o600); err != nil {
		return restoreResult{}, err
	}
	if err := repository.ValidateDatabaseFile(importedDatabase); err != nil {
		return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP_DATABASE", "迁移包数据库结构无效")
	}
	for name, entry := range entries {
		if name == "manifest.json" || name == "paper-review.db" {
			continue
		}
		if !allowedRestorePath(name) {
			return restoreResult{}, domain.NewAPIError(400, "INVALID_BACKUP_PATH", "迁移包包含不支持的数据路径："+name)
		}
		target := filepath.Join(importedData, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return restoreResult{}, err
		}
		if err := copyZipEntry(entry, target, 0o600); err != nil {
			return restoreResult{}, err
		}
	}

	preRestoreBytes, err := api.createBackupArchive()
	if err != nil {
		return restoreResult{}, err
	}
	backupDirectory := filepath.Join(dataDirectory, "backups")
	if err := os.MkdirAll(backupDirectory, 0o700); err != nil {
		return restoreResult{}, err
	}
	preRestoreName := "pre-restore-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".zip"
	preRestoreName = strings.NewReplacer(":", "-", ".", "-").Replace(preRestoreName)
	if err := os.WriteFile(filepath.Join(backupDirectory, preRestoreName), preRestoreBytes, 0o600); err != nil {
		return restoreResult{}, err
	}

	if err := api.replaceRestoredData(ctx, importedDatabase, importedData, tempRoot); err != nil {
		return restoreResult{}, err
	}
	api.store.Audit(ctx, nil, &actorID, "system", "restore", "completed", map[string]any{"schemaVersion": manifest.SchemaVersion, "preRestoreBackup": preRestoreName})
	return restoreResult{Manifest: manifest, PreRestoreBackup: preRestoreName}, nil
}

func safeArchivePath(value string) (string, error) {
	normalized := strings.ReplaceAll(value, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "\x00") || normalized == "." {
		return "", domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "迁移包包含不安全路径")
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "迁移包包含不安全路径")
		}
	}
	return normalized, nil
}

func allowedRestorePath(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		return true
	}
	switch parts[0] {
	case "snapshots", "workspaces", "exports", "source-assets", "external":
		return true
	default:
		return false
	}
}

func readZipEntry(entry *zip.File, limit int64) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, limit))
}

func copyZipEntry(entry *zip.File, destination string, mode os.FileMode) error {
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	temporary := destination + ".tmp-" + uuid.NewString()
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, reader)
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (api *API) replaceRestoredData(ctx context.Context, importedDatabase, importedData, tempRoot string) error {
	databasePath := filepath.Join(api.dataDirectory, "paper-review.db")
	previousDatabase := filepath.Join(tempRoot, "previous-paper-review.db")
	if err := api.store.Close(); err != nil {
		return err
	}
	type directoryMove struct {
		current, imported, previous string
		moved, installed            bool
	}
	type fileMove struct {
		current, imported, previous string
		moved, installed            bool
	}
	managedDirectories := []string{"snapshots", "workspaces", "exports", "source-assets", "external"}
	moves := make([]*directoryMove, 0, len(managedDirectories))
	rootMoves := []*fileMove{}
	rollback := func() {
		_ = api.store.Close()
		_ = os.Remove(databasePath)
		_ = os.Rename(previousDatabase, databasePath)
		for _, move := range moves {
			if move.installed {
				_ = os.RemoveAll(move.current)
			}
			if move.moved {
				_ = os.Rename(move.previous, move.current)
			}
		}
		for _, move := range rootMoves {
			if move.installed {
				_ = os.Remove(move.current)
			}
			if move.moved {
				_ = os.Rename(move.previous, move.current)
			}
		}
		_ = api.store.Reopen(context.Background())
	}
	failed := func(err error) error {
		rollback()
		return err
	}
	if err := os.Rename(databasePath, previousDatabase); err != nil {
		_ = api.store.Reopen(context.Background())
		return err
	}
	if err := os.Rename(importedDatabase, databasePath); err != nil {
		_ = os.Rename(previousDatabase, databasePath)
		_ = api.store.Reopen(context.Background())
		return err
	}
	_ = os.Remove(databasePath + "-wal")
	_ = os.Remove(databasePath + "-shm")
	for _, name := range managedDirectories {
		moves = append(moves, &directoryMove{current: filepath.Join(api.dataDirectory, name), imported: filepath.Join(importedData, name), previous: filepath.Join(tempRoot, "previous-"+name)})
	}
	entries, err := os.ReadDir(importedData)
	if err != nil {
		return failed(err)
	}
	rootFiles := []string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		rootFiles = append(rootFiles, entry.Name())
	}
	sort.Strings(rootFiles)
	for _, move := range moves {
		if err := os.RemoveAll(move.previous); err != nil {
			return failed(err)
		}
		if err := os.Rename(move.current, move.previous); err == nil {
			move.moved = true
		} else if !os.IsNotExist(err) {
			return failed(err)
		}
		if err := os.Rename(move.imported, move.current); err != nil && !os.IsNotExist(err) {
			return failed(err)
		} else if err == nil {
			move.installed = true
		}
		if err := os.MkdirAll(move.current, 0o700); err != nil {
			return failed(err)
		}
		move.installed = true
	}
	for _, name := range rootFiles {
		move := &fileMove{current: filepath.Join(api.dataDirectory, name), imported: filepath.Join(importedData, name), previous: filepath.Join(tempRoot, "previous-root-"+name)}
		rootMoves = append(rootMoves, move)
		if err := os.Remove(move.previous); err != nil && !os.IsNotExist(err) {
			return failed(err)
		}
		if err := os.Rename(move.current, move.previous); err == nil {
			move.moved = true
		} else if !os.IsNotExist(err) {
			return failed(err)
		}
		if err := os.Rename(move.imported, move.current); err != nil {
			return failed(err)
		}
		move.installed = true
	}
	if err := api.store.Reopen(ctx); err != nil {
		return failed(err)
	}
	return nil
}

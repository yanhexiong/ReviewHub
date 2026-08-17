package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func (api *API) adminOperations(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.RequireRole(request, "admin"); err != nil {
		writeError(response, err)
		return
	}
	schemaVersion, err := api.store.SchemaVersion(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	backups := backupInfo(api.dataDirectory)
	dataExists := false
	databaseBytes := int64(0)
	if stat, err := os.Stat(api.dataDirectory); err == nil {
		dataExists = stat.IsDir()
	}
	if stat, err := os.Stat(filepath.Join(api.dataDirectory, "paper-review.db")); err == nil {
		databaseBytes = stat.Size()
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"dataDirectoryExists": dataExists,
		"databaseBytes":       databaseBytes,
		"schemaVersion":       schemaVersion,
		"latestMigration":     fmt.Sprintf("go-schema-v%d", schemaVersion),
		"migrationSummary": map[string]any{
			"authority": "go",
			"version":   schemaVersion,
		},
		"migrations": []map[string]any{{
			"version": schemaVersion,
			"status":  "applied",
		}},
		"backups":       backups,
		"sqliteVersion": map[string]string{"version": "modernc.org/sqlite"},
		"nodeVersion":   runtime.Version(),
		"platform":      runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func backupInfo(dataDirectory string) []map[string]any {
	entries, err := os.ReadDir(filepath.Join(dataDirectory, "backups"))
	if err != nil {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		stat, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, map[string]any{"file": entry.Name(), "bytes": stat.Size(), "modifiedAt": stat.ModTime().UTC().Format(time.RFC3339Nano)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i]["file"].(string) > result[j]["file"].(string) })
	if len(result) > 20 {
		result = result[:20]
	}
	return result
}

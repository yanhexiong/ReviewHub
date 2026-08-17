package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

func (api *API) snapshotPDF(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID, snapshotID := request.PathValue("projectID"), request.PathValue("snapshotID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "view"); err != nil {
		writeError(response, err)
		return
	}
	snapshot, err := api.store.SnapshotByID(request.Context(), projectID, snapshotID)
	if err != nil {
		writeError(response, err)
		return
	}
	if snapshot == nil {
		writeError(response, domain.NewAPIError(404, "NOT_FOUND", "快照不存在"))
		return
	}
	archivedPath, _ := snapshot["archived_pdf_path"].(string)
	filePath, err := api.managedDataPath(archivedPath)
	if err != nil {
		writeError(response, err)
		return
	}
	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		writeError(response, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在"))
		return
	}
	if err != nil {
		writeError(response, err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeError(response, err)
		return
	}
	if !stat.Mode().IsRegular() {
		writeError(response, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在"))
		return
	}
	name, _ := snapshot["original_file_name"].(string)
	name = safeDownloadName(name, "snapshot.pdf")
	response.Header().Set("Content-Type", "application/pdf")
	response.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	response.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeContent(response, request, name, stat.ModTime(), file)
}

func (api *API) managedDataPath(value string) (string, error) {
	dataDirectory := api.dataDirectory
	if strings.TrimSpace(dataDirectory) == "" {
		dataDirectory = "data"
	}
	root, err := filepath.Abs(dataDirectory)
	if err != nil || root == "." {
		return "", domain.NewAPIError(500, "INTERNAL_ERROR", "数据目录不可用")
	}
	// Resolve the data root and the existing target before returning it.  The
	// database stores relative paths, but a replaced archive can otherwise be a
	// symlink to an arbitrary host file while still passing the lexical check.
	if canonicalRoot, rootErr := filepath.EvalSymlinks(root); rootErr == nil {
		root = filepath.Clean(canonicalRoot)
	} else if !os.IsNotExist(rootErr) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在数据目录内")
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", domain.NewAPIError(400, "PATH_NOT_ALLOWED", "文件路径无效")
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在数据目录内")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(candidate); resolveErr == nil {
		if !pathInside(root, resolved) {
			return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在数据目录内")
		}
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(resolveErr) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在数据目录内")
	}
	// A missing file is still returned so callers can preserve their specific
	// NOT_FOUND response.  Its existing parent must not escape through a
	// symlink, however.
	if parent, parentErr := filepath.EvalSymlinks(filepath.Dir(candidate)); parentErr == nil && !pathInside(root, parent) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在数据目录内")
	}
	return candidate, nil
}

func safeDownloadName(value, fallback string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.NewReplacer("\r", "", "\n", "", `"`, "", "\\", "").Replace(value)
	if value == "" || value == "." || value == string(filepath.Separator) {
		return fallback
	}
	return value
}

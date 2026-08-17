package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/editor"
)

func (api *API) projectEditor(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	project, err := api.store.ProjectByID(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	repositoryPath, _ := project["repository_path"].(string)
	root, err := api.safeExistingPath(request, repositoryPath, false)
	if err != nil {
		writeError(response, err)
		return
	}
	if request.Method == http.MethodGet {
		filePath := strings.TrimSpace(request.URL.Query().Get("path"))
		if filePath == "" {
			files, err := editor.ListFiles(root)
			if err != nil {
				writeError(response, err)
				return
			}
			writeJSON(response, http.StatusOK, map[string]any{"files": files, "gitStatuses": editor.GitStatuses(request.Context(), root)})
			return
		}
		file, err := editor.ReadFile(root, filePath)
		if err != nil {
			writeError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"file": file})
		return
	}
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 6<<20))
	if err := decoder.Decode(&input); err != nil {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if strings.TrimSpace(input.Path) == "" || len(input.Content) > 5_000_000 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	file, err := editor.WriteFile(root, input.Path, input.Content)
	if err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "project_file", file.Path, "updated", map[string]any{"path": file.Path, "size": file.Size, "sha256": file.SHA256})
	writeJSON(response, http.StatusOK, map[string]any{"file": file, "gitStatuses": editor.GitStatuses(request.Context(), root)})
}

package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/importprogress"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/secretbox"
)

func (api *API) syncProject(response http.ResponseWriter, request *http.Request) {
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
	sourceType, _ := project["source_type"].(string)
	sourceURI, _ := project["source_uri"].(string)
	if sourceType != "github" || strings.TrimSpace(sourceURI) == "" {
		writeError(response, domain.NewAPIError(400, "SYNC_NOT_SUPPORTED", "只有 GitHub 项目支持同步"))
		return
	}
	var input struct {
		GithubToken      string `json:"githubToken"`
		ClearGithubToken bool   `json:"clearGithubToken"`
		Branch           string `json:"branch"`
	}
	if request.Body != nil && request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, err)
			return
		}
	}
	if len(input.GithubToken) > 500 || len(input.Branch) > 200 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "同步参数长度无效"))
		return
	}
	progressID := importprogress.IDFromHeader(request.Header.Get("X-Review-Hub-Progress-ID"))
	api.progress.Start(progressID, user.ID)
	api.progress.Update(progressID, user.ID, 8, "正在读取同步配置")

	ownerID, _ := project["created_by_user_id"].(string)
	currentCredential, _ := project["source_credential_ciphertext"].(string)
	storedToken := api.decryptOptional(currentCredential)
	projectGit, err := api.store.ProjectGitSettings(request.Context(), projectID)
	if err != nil {
		api.progress.Fail(progressID, user.ID, "无法读取项目 Git 配置")
		writeError(response, err)
		return
	}
	if storedToken == "" {
		storedToken = api.decryptOptional(projectGit.AccessTokenCiphertext)
	}
	accountToken := api.userGitHubToken(request.Context(), ownerID)
	effectiveToken := strings.TrimSpace(input.GithubToken)
	if effectiveToken == "" && !input.ClearGithubToken {
		effectiveToken = storedToken
	}
	if effectiveToken == "" && !input.ClearGithubToken {
		effectiveToken = accountToken
	}
	branch := strings.TrimSpace(input.Branch)
	if branch == "" {
		branch, _ = project["source_branch"].(string)
	}

	oldWorkspace := api.storedAbsolutePath(project["workspace_path"])
	oldPDF := api.storedAbsolutePath(project["source_pdf_path"])
	sourceRelative := ""
	localFallback := true
	if oldWorkspace != "" && oldPDF != "" && pathInside(oldWorkspace, oldPDF) {
		sourceRelative, _ = filepath.Rel(oldWorkspace, oldPDF)
		localFallback = false
	}
	stageID := projectID + "-" + uuid.NewString()
	api.progress.Update(progressID, user.ID, 18, "正在同步 GitHub 仓库")
	cloned, err := api.importer.CloneGitHub(request.Context(), stageID, sourceURI, branch, effectiveToken)
	if err != nil {
		api.progress.Fail(progressID, user.ID, publicImportError(err))
		writeError(response, err)
		return
	}
	stagedWorkspace := cloned.Workspace
	defer func() {
		if stagedWorkspace != "" {
			_ = os.RemoveAll(stagedWorkspace)
		}
	}()
	api.progress.Update(progressID, user.ID, 70, "正在检查同步后的 PDF")
	var sourcePDF string
	if localFallback {
		if oldPDF == "" {
			writeError(response, domain.NewAPIError(400, "SYNC_PDF_NOT_FOUND", "同步仓库没有配置的 PDF，请先编译 PDF 或重新选择路径"))
			return
		}
		if err := api.assertPDF(request.Context(), oldPDF); err != nil {
			writeError(response, domain.NewAPIError(400, "SYNC_PDF_NOT_FOUND", "同步仓库没有配置的 PDF，请先编译 PDF 或重新选择路径"))
			return
		}
		sourcePDF = oldPDF
	} else {
		candidate, pathErr := managedWorkspacePath(stagedWorkspace, sourceRelative)
		if pathErr != nil {
			writeError(response, pathErr)
			return
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			previous, previousErr := managedWorkspacePath(oldWorkspace, sourceRelative)
			if previousErr != nil {
				writeError(response, domain.NewAPIError(400, "SYNC_PDF_NOT_FOUND", "同步仓库没有配置的 PDF，请先编译 PDF 或重新选择路径"))
				return
			}
			if err := api.assertPDF(request.Context(), previous); err != nil {
				writeError(response, domain.NewAPIError(400, "SYNC_PDF_NOT_FOUND", "同步仓库没有配置的 PDF，请先编译 PDF 或重新选择路径"))
				return
			}
			if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
				writeError(response, err)
				return
			}
			if err := copyFile(previous, candidate); err != nil {
				writeError(response, err)
				return
			}
		}
		if err := api.assertPDF(request.Context(), candidate); err != nil {
			writeError(response, err)
			return
		}
		sourcePDF = filepath.Join(api.dataDirectory, "workspaces", projectID, sourceRelative)
	}

	workspace := filepath.Join(api.dataDirectory, "workspaces", projectID)
	previousWorkspace := filepath.Join(api.dataDirectory, "workspaces", ".previous-"+projectID+"-"+uuid.NewString())
	previousMoved, installed := false, false
	defer func() {
		if installed {
			_ = os.RemoveAll(workspace)
		}
		if previousMoved {
			_ = os.Rename(previousWorkspace, workspace)
		}
	}()
	if err := os.Rename(workspace, previousWorkspace); err != nil && !os.IsNotExist(err) {
		writeError(response, err)
		return
	} else if err == nil {
		previousMoved = true
	}
	if err := os.Rename(stagedWorkspace, workspace); err != nil {
		writeError(response, err)
		return
	}
	installed = true
	newCredential := currentCredential
	if input.GithubToken != "" {
		newCredential = api.encryptOptional(input.GithubToken)
	} else if input.ClearGithubToken {
		newCredential = ""
	}
	newSourcePDF := sourcePDF
	if !localFallback {
		newSourcePDF = filepath.Join(workspace, sourceRelative)
	}
	if err := api.store.UpdateProject(request.Context(), repository.ProjectUpdate{ID: projectID, Name: projectString(project["name"]), Slug: projectString(project["slug"]), Description: nullableProjectDescription(project["description"]), RepositoryPath: relativeWorkingPath(workspace), SourcePDFPath: relativeWorkingPath(newSourcePDF), SourceCredentialCiphertext: newCredential}); err != nil {
		writeError(response, err)
		return
	}
	if previousMoved {
		_ = os.RemoveAll(previousWorkspace)
		previousMoved = false
	}
	installed = false
	api.store.Audit(request.Context(), &projectID, &user.ID, "project", projectID, "github_synced", map[string]any{"sourceUri": cloned.URL, "sourceBranch": cloned.Branch})
	api.progress.Complete(progressID, user.ID, "项目同步完成")
	updated, err := api.store.PublicProject(request.Context(), projectID, user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"project": updated})
}

func (api *API) decryptOptional(value string) string {
	if strings.TrimSpace(value) == "" || api.encryptionKey == "" {
		return ""
	}
	decoded, err := secretbox.Decrypt(api.encryptionKey, value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(decoded)
}

func (api *API) storedAbsolutePath(value any) string {
	pathValue, _ := value.(string)
	if strings.TrimSpace(pathValue) == "" {
		return ""
	}
	absolute, err := filepath.Abs(pathValue)
	if err != nil {
		return ""
	}
	return absolute
}

func nullableProjectDescription(value any) *string {
	description, ok := value.(string)
	if !ok {
		empty := ""
		return &empty
	}
	return &description
}

func projectString(value any) string {
	text, _ := value.(string)
	return text
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}

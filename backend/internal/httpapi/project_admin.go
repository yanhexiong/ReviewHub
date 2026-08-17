package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/secretbox"
)

var projectSlugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func (api *API) createProject(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin", "author")
	if err != nil {
		writeError(response, err)
		return
	}
	if strings.Contains(strings.ToLower(request.Header.Get("content-type")), "multipart/form-data") {
		api.createImportedProject(response, request, user)
		return
	}
	var input struct{ Name, Slug, Description, RepositoryPath, SourcePDFPath, GitUserName, GitUserEmail string }
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.Name, input.Slug, input.Description = strings.TrimSpace(input.Name), strings.TrimSpace(input.Slug), strings.TrimSpace(input.Description)
	if input.Name == "" || len(input.Name) > 120 || !projectSlugPattern.MatchString(input.Slug) || len(input.Description) > 1000 || input.RepositoryPath == "" || input.SourcePDFPath == "" {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if err := api.assertProjectQuota(request, user); err != nil {
		writeError(response, err)
		return
	}
	repositoryPath, err := api.safeExistingPath(request, input.RepositoryPath, false)
	if err != nil {
		writeError(response, err)
		return
	}
	pdfPath, err := api.safeExistingPath(request, input.SourcePDFPath, true)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.assertPDF(request.Context(), pdfPath); err != nil {
		writeError(response, err)
		return
	}
	projectID := uuid.NewString()
	project := repository.NewProject{ID: projectID, Name: input.Name, Slug: input.Slug, Description: input.Description, RepositoryPath: relativeWorkingPath(repositoryPath), SourcePDFPath: relativeWorkingPath(pdfPath), SourceType: "local", CreatedByUserID: user.ID}
	if err := api.store.CreateProject(request.Context(), project); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(response, domain.NewAPIError(409, "SLUG_EXISTS", "URL 标识已被其他项目使用"))
		} else {
			writeError(response, err)
		}
		return
	}
	if input.GitUserName != "" || input.GitUserEmail != "" {
		_ = api.store.SaveProjectGitSettings(request.Context(), repository.GitSettings{ProjectID: projectID, UserName: strings.TrimSpace(input.GitUserName), UserEmail: strings.TrimSpace(input.GitUserEmail)})
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "project", projectID, "created", map[string]any{"name": input.Name, "slug": input.Slug, "sourceType": "local"})
	created, err := api.store.PublicProject(request.Context(), projectID, user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"project": created})
}

func (api *API) updateProject(response http.ResponseWriter, request *http.Request) {
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
	var input struct {
		Name, Slug, Description, RepositoryPath, SourcePDFPath, GithubToken string
		ClearGithubToken                                                    bool
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	name, _ := project["name"].(string)
	slug, _ := project["slug"].(string)
	description, _ := project["description"].(string)
	repositoryPath, _ := project["repository_path"].(string)
	sourcePDFPath, _ := project["source_pdf_path"].(string)
	sourceType, _ := project["source_type"].(string)
	credential, _ := project["source_credential_ciphertext"].(string)
	if input.Name != "" {
		name = strings.TrimSpace(input.Name)
	}
	if input.Slug != "" {
		slug = strings.TrimSpace(input.Slug)
	}
	if input.Description != "" {
		description = strings.TrimSpace(input.Description)
	}
	if name == "" || len(name) > 120 || !projectSlugPattern.MatchString(slug) || len(description) > 1000 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if input.RepositoryPath != "" {
		resolved, err := api.safeExistingPath(request, input.RepositoryPath, false)
		if err != nil {
			writeError(response, err)
			return
		}
		repositoryPath = relativeWorkingPath(resolved)
	}
	if input.SourcePDFPath != "" {
		resolved, err := api.safeExistingPath(request, input.SourcePDFPath, true)
		if err != nil {
			writeError(response, err)
			return
		}
		if err := api.assertPDF(request.Context(), resolved); err != nil {
			writeError(response, err)
			return
		}
		sourcePDFPath = relativeWorkingPath(resolved)
	}
	if input.GithubToken != "" {
		if sourceType != "github" {
			writeError(response, domain.NewAPIError(400, "GITHUB_TOKEN_NOT_SUPPORTED", "只有 GitHub 项目可以配置访问令牌"))
			return
		}
		credential, err = secretbox.Encrypt(api.encryptionKey, strings.TrimSpace(input.GithubToken))
		if err != nil {
			writeError(response, err)
			return
		}
	} else if input.ClearGithubToken {
		credential = ""
	}
	descriptionValue := description
	if strings.TrimSpace(descriptionValue) == "" {
		descriptionValue = ""
	}
	if err := api.store.UpdateProject(request.Context(), repository.ProjectUpdate{ID: projectID, Name: name, Slug: slug, Description: &descriptionValue, RepositoryPath: repositoryPath, SourcePDFPath: sourcePDFPath, SourceCredentialCiphertext: credential}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(response, domain.NewAPIError(409, "SLUG_EXISTS", "URL 标识已被其他项目使用"))
		} else {
			writeError(response, err)
		}
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "project", projectID, "updated", map[string]any{"name": name, "slug": slug, "hasSourceCredential": credential != ""})
	updated, err := api.store.PublicProject(request.Context(), projectID, user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"project": updated})
}

func (api *API) deleteProject(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "owner"); err != nil {
		writeError(response, err)
		return
	}
	project, err := api.store.ProjectByID(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.DeleteProject(request.Context(), projectID); err != nil {
		writeError(response, err)
		return
	}
	api.removeManagedProjectData(project)
	api.store.Audit(request.Context(), &projectID, &user.ID, "project", projectID, "deleted", map[string]any{"name": project["name"], "slug": project["slug"]})
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) assertProjectQuota(request *http.Request, user domain.User) error {
	settings, err := api.store.ResourceSettings(request.Context())
	if err != nil {
		return err
	}
	limit := settings.MaxProjectsPerUser
	if user.ProjectLimit != nil {
		limit = int(*user.ProjectLimit)
	}
	if limit <= 0 {
		return nil
	}
	projects, err := api.store.ListProjects(request.Context(), user)
	if err != nil {
		return err
	}
	owned := 0
	for _, project := range projects {
		if project["created_by_user_id"] == user.ID {
			owned++
		}
	}
	if owned >= limit {
		return domain.NewAPIError(403, "PROJECT_LIMIT_REACHED", "该账户已达到项目数量上限")
	}
	return nil
}

func (api *API) safeExistingPath(request *http.Request, value string, fileOnly bool) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", domain.NewAPIError(400, "PATH_NOT_FOUND", "路径不存在或不可读取")
	}
	canonical, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", domain.NewAPIError(400, "PATH_NOT_FOUND", "路径不存在或不可读取")
	}
	stat, err := os.Stat(canonical)
	if err != nil {
		return "", domain.NewAPIError(400, "PATH_NOT_FOUND", "路径不存在或不可读取")
	}
	if fileOnly && !stat.Mode().IsRegular() {
		return "", domain.NewAPIError(400, "INVALID_PDF", "源路径不是文件")
	}
	allowed, err := api.store.AllowedRoots(request.Context())
	if err != nil {
		return "", err
	}
	inside := false
	for _, root := range allowed {
		if pathInside(root, canonical) {
			inside = true
			break
		}
	}
	if !inside && !api.isManagedWorkspace(canonical) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "路径不在允许的目录内")
	}
	return canonical, nil
}

func (api *API) assertPDF(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return domain.NewAPIError(400, "PATH_NOT_FOUND", "PDF 文件不可读取")
	}
	defer file.Close()
	if settings, settingsErr := api.store.ResourceSettings(ctx); settingsErr == nil {
		if stat, statErr := file.Stat(); statErr == nil && stat.Size() > int64(settings.MaxPDFBytes) {
			return domain.NewAPIError(413, "PDF_TOO_LARGE", "PDF 文件超过管理员设置的大小上限")
		}
	}
	header := make([]byte, 5)
	if _, err := file.Read(header); err != nil || string(header) != "%PDF-" {
		return domain.NewAPIError(400, "INVALID_PDF", "源文件不是有效 PDF")
	}
	return nil
}

func relativeWorkingPath(value string) string {
	cwd, _ := os.Getwd()
	relative, err := filepath.Rel(cwd, value)
	if err != nil {
		return value
	}
	return filepath.ToSlash(relative)
}
func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func (api *API) isManagedWorkspace(value string) bool {
	root := filepath.Join(api.dataDirectory, "workspaces")
	return pathInside(root, value)
}
func (api *API) removeManagedProjectData(project map[string]any) {
	workspace, _ := project["workspace_path"].(string)
	if workspace != "" {
		absolute, _ := filepath.Abs(workspace)
		if api.isManagedWorkspace(absolute) {
			_ = os.RemoveAll(absolute)
		}
	}
	id, _ := project["id"].(string)
	if id != "" {
		root := filepath.Join(api.dataDirectory, "snapshots", id)
		if pathInside(filepath.Join(api.dataDirectory, "snapshots"), root) {
			_ = os.RemoveAll(root)
		}
	}
}

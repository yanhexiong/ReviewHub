package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/importer"
	"github.com/yanhexiong/review-hub/backend/internal/importprogress"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/texcompile"
)

func (api *API) usePendingPDF(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	progressID := importprogress.IDFromHeader(request.Header.Get("X-Review-Hub-Progress-ID"))
	api.progress.Start(progressID, user.ID)
	pending, err := api.store.PendingProject(request.Context(), request.PathValue("pendingID"), user.ID)
	if err != nil {
		api.progress.Fail(progressID, user.ID, publicImportError(err))
		writeError(response, err)
		return
	}
	var input struct {
		SourcePDFPath string `json:"sourcePdfPath"`
		TexRootPath   string `json:"texRootPath"`
	}
	if err := decodeJSON(request, &input); err != nil {
		api.progress.Fail(progressID, user.ID, "Invalid pending PDF request")
		writeError(response, err)
		return
	}
	input.SourcePDFPath = strings.TrimSpace(input.SourcePDFPath)
	input.TexRootPath = strings.TrimSpace(input.TexRootPath)
	if input.SourcePDFPath == "" || len(input.SourcePDFPath) > 2000 || len(input.TexRootPath) > 500 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "PDF 路径参数无效"))
		return
	}
	api.progress.Update(progressID, user.ID, 20, "正在检查 PDF 文件")
	workspace, err := api.pendingWorkspace(pending.WorkspacePath)
	if err != nil {
		writeError(response, err)
		return
	}
	sourcePDF, pathErr := managedWorkspacePath(workspace, input.SourcePDFPath)
	if pathErr != nil {
		sourcePDF, err = api.safeExistingPath(request, input.SourcePDFPath, true)
		if err != nil {
			writeError(response, err)
			return
		}
	}
	if _, err := os.Stat(sourcePDF); err != nil {
		writeError(response, domain.NewAPIError(400, "PATH_NOT_FOUND", "PDF 文件不可读取"))
		return
	}
	if err := api.assertPDF(request.Context(), sourcePDF); err != nil {
		writeError(response, err)
		return
	}
	if input.TexRootPath != "" {
		candidates, err := importer.ListTexFiles(workspace)
		if err != nil || !containsString(candidates, input.TexRootPath) {
			writeError(response, domain.NewAPIError(400, "TEX_ROOT_NOT_FOUND", "TeX 入口文件不存在于仓库中，请从候选文件中选择"))
			return
		}
	}
	config, _, err := api.pendingCompileConfig(request, user.ID, "")
	if err != nil {
		writeError(response, err)
		return
	}
	project, err := api.finalizePendingProject(request.Context(), user, pending, sourcePDF, input.TexRootPath, "", config)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.DeletePendingProject(request.Context(), pending.ID, user.ID); err != nil {
		writeError(response, err)
		return
	}
	api.progress.Complete(progressID, user.ID, "项目创建完成")
	writeJSON(response, http.StatusCreated, map[string]any{"project": project})
}

func (api *API) deletePendingProject(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	pending, err := api.store.PendingProject(request.Context(), request.PathValue("pendingID"), user.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.DeletePendingProject(request.Context(), pending.ID, user.ID); err != nil {
		writeError(response, err)
		return
	}
	workspace, _ := filepath.Abs(pending.WorkspacePath)
	if api.isManagedWorkspace(workspace) {
		_ = os.RemoveAll(workspace)
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) compilePendingProject(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	progressID := importprogress.IDFromHeader(request.Header.Get("X-Review-Hub-Progress-ID"))
	api.progress.Start(progressID, user.ID)
	pending, err := api.store.PendingProject(request.Context(), request.PathValue("pendingID"), user.ID)
	if err != nil {
		api.progress.Fail(progressID, user.ID, publicImportError(err))
		writeError(response, err)
		return
	}
	var input struct {
		TexRootPath  string `json:"texRootPath"`
		TexProfileID string `json:"texProfileId"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.TexRootPath, input.TexProfileID = strings.TrimSpace(input.TexRootPath), strings.TrimSpace(input.TexProfileID)
	if input.TexRootPath == "" || len(input.TexRootPath) > 500 || len(input.TexProfileID) > 100 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "TeX 编译参数无效"))
		return
	}
	workspace, err := api.pendingWorkspace(pending.WorkspacePath)
	if err != nil {
		writeError(response, err)
		return
	}
	candidates, err := importer.ListTexFiles(workspace)
	if err != nil || !containsString(candidates, input.TexRootPath) {
		writeError(response, domain.NewAPIError(400, "TEX_ROOT_NOT_FOUND", "TeX 入口文件不存在于仓库中，请从候选文件中选择"))
		return
	}
	config, profile, err := api.pendingCompileConfig(request, user.ID, input.TexProfileID)
	if err != nil {
		writeError(response, err)
		return
	}
	api.progress.Update(progressID, user.ID, 16, "已确认 TeX 编译入口")
	compiled, err := texcompile.Compile(request.Context(), workspace, input.TexRootPath, config)
	if err != nil {
		api.progress.Fail(progressID, user.ID, publicImportError(err))
		writeError(response, err)
		return
	}
	api.progress.Update(progressID, user.ID, 88, "编译完成，正在创建项目记录")
	project, err := api.finalizePendingProject(request.Context(), user, pending, compiled.PDFPath, input.TexRootPath, profileID(profile), config)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.DeletePendingProject(request.Context(), pending.ID, user.ID); err != nil {
		writeError(response, err)
		return
	}
	api.progress.Complete(progressID, user.ID, "项目创建完成")
	writeJSON(response, http.StatusCreated, map[string]any{"project": project})
}

func (api *API) pendingCompileConfig(request *http.Request, userID, profileID string) (texcompile.Config, *repository.TexLiveProfile, error) {
	defaults, err := api.store.UserVscodeDefaults(request.Context(), userID)
	if err != nil {
		return texcompile.Config{}, nil, err
	}
	config := texcompile.Config{Engine: defaults.Engine, BuildTool: defaults.BuildTool, OutputDirectory: defaults.OutputDirectory, TexLiveBinPath: defaults.TexLiveBinPath, ShellEscape: defaults.ShellEscape}
	if profileID == "" {
		return config, nil, nil
	}
	profile, err := api.store.TexLiveProfile(request.Context(), profileID)
	if err != nil {
		return texcompile.Config{}, nil, err
	}
	if profile == nil {
		return texcompile.Config{}, nil, domain.NewAPIError(400, "TEX_PROFILE_NOT_FOUND", "所选 TeX Live 配置不存在或已被删除")
	}
	return texcompile.Config{Engine: profile.Engine, BuildTool: profile.BuildTool, OutputDirectory: profile.OutputDirectory, TexLiveBinPath: profile.TexLiveBinPath, ShellEscape: profile.ShellEscape}, profile, nil
}

func (api *API) pendingWorkspace(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || !api.isManagedWorkspace(absolute) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "待处理项目工作区无效")
	}
	if stat, err := os.Stat(absolute); err != nil || !stat.IsDir() {
		return "", domain.NewAPIError(404, "PENDING_NOT_FOUND", "待创建项目工作区不存在")
	}
	return absolute, nil
}

func (api *API) finalizePendingProject(ctx context.Context, user domain.User, pending repository.PendingProject, sourcePDF, texRoot, profileID string, config texcompile.Config) (map[string]any, error) {
	projectID := uuid.NewString()
	project := repository.NewProject{ID: projectID, Name: pending.Payload.Name, Slug: pending.Payload.Slug, Description: pending.Payload.Description,
		RepositoryPath: relativeWorkingPath(pending.WorkspacePath), SourcePDFPath: relativeWorkingPath(sourcePDF), SourceType: pending.Payload.SourceType,
		SourceURI: pending.Payload.SourceURI, SourceBranch: pending.Payload.SourceBranch, WorkspacePath: relativeWorkingPath(pending.WorkspacePath),
		SourceCredentialCiphertext: pending.Payload.SourceCredentialCiphertext, CreatedByUserID: user.ID}
	if err := api.store.CreateProject(ctx, project); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, domain.NewAPIError(409, "SLUG_EXISTS", "URL 标识已被其他项目使用")
		}
		return nil, err
	}
	if pending.Payload.GitUserName != "" || pending.Payload.GitUserEmail != "" {
		if err := api.store.SaveProjectGitSettings(ctx, repository.GitSettings{ProjectID: projectID, UserName: pending.Payload.GitUserName, UserEmail: pending.Payload.GitUserEmail}); err != nil {
			return nil, err
		}
	}
	if texRoot != "" {
		if err := api.store.SaveProjectTexSettings(ctx, repository.TexSettings{ProjectID: projectID, Engine: config.Engine, BuildTool: config.BuildTool, OutputDirectory: config.OutputDirectory, AutoBuild: "off", PDFPreview: "review-hub", SyncTex: true, ShellEscape: config.ShellEscape, TexLiveBinPath: config.TexLiveBinPath, TexRootPath: texRoot, ProfileID: profileID}); err != nil {
			return nil, err
		}
	}
	api.store.Audit(ctx, &projectID, &user.ID, "project", projectID, "created", map[string]any{"name": pending.Payload.Name, "slug": pending.Payload.Slug, "sourceType": pending.Payload.SourceType})
	return api.store.PublicProject(ctx, projectID, user)
}

func profileID(profile *repository.TexLiveProfile) string {
	if profile == nil {
		return ""
	}
	return profile.ID
}

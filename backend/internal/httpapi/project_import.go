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
	"github.com/yanhexiong/review-hub/backend/internal/secretbox"
)

func (api *API) importProgress(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	id := importprogress.IDFromHeader(request.PathValue("progressID"))
	if id == "" {
		id = importprogress.IDFromHeader(strings.TrimPrefix(request.URL.Path, "/api/projects/progress/"))
	}
	state, ok := api.progress.Get(id, user.ID)
	if !ok {
		writeError(response, domain.NewAPIError(404, "PROGRESS_NOT_FOUND", "导入进度已过期"))
		return
	}
	writeJSON(response, http.StatusOK, state)
}

func (api *API) createImportedProject(response http.ResponseWriter, request *http.Request, user domain.User) {
	if api.importer == nil {
		writeError(response, domain.NewAPIError(503, "IMPORT_UNAVAILABLE", "项目导入服务尚未就绪"))
		return
	}
	settings, err := api.store.ResourceSettings(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	progressID := importprogress.IDFromHeader(request.Header.Get("X-Review-Hub-Progress-ID"))
	api.progress.Start(progressID, user.ID)
	keepWorkspace := false
	workspace := ""
	projectID := uuid.NewString()
	defer func() {
		if !keepWorkspace && workspace != "" {
			_ = os.RemoveAll(workspace)
		}
	}()

	if err := request.ParseMultipartForm(int64(settings.MaxImportBytes) + 2<<20); err != nil {
		api.progress.Fail(progressID, user.ID, "导入请求过大或格式无效")
		writeError(response, domain.NewAPIError(400, "INVALID_MULTIPART", "导入请求过大或格式无效"))
		return
	}
	value := func(name string) string { return strings.TrimSpace(request.FormValue(name)) }
	name, slug, description := value("name"), value("slug"), value("description")
	if name == "" || len(name) > 120 || !projectSlugPattern.MatchString(slug) || len(description) > 1000 {
		api.progress.Fail(progressID, user.ID, "项目配置无效")
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if err := api.assertProjectQuota(request, user); err != nil {
		api.progress.Fail(progressID, user.ID, err.Error())
		writeError(response, err)
		return
	}
	sourceType := value("sourceType")
	if sourceType != "archive" && sourceType != "github" {
		api.progress.Fail(progressID, user.ID, "项目来源无效")
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "项目来源必须是仓库压缩包或 GitHub"))
		return
	}
	gitUserName, gitUserEmail := value("gitUserName"), value("gitUserEmail")
	if len(gitUserName) > 120 || len(gitUserEmail) > 254 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "Git 用户信息长度无效"))
		return
	}
	requestedPDFPath := value("sourcePdfPath")
	texRoot := value("texRootPath")
	githubToken := value("githubToken")
	if len(requestedPDFPath) > 2000 || len(texRoot) > 500 || len(githubToken) > 500 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "导入参数长度无效"))
		return
	}
	effectiveToken := githubToken
	if effectiveToken == "" {
		effectiveToken = api.userGitHubToken(request.Context(), user.ID)
	}
	api.progress.Update(progressID, user.ID, 8, "正在验证项目配置")

	var sourceURI, sourceBranch string
	if sourceType == "archive" {
		archive, header, err := request.FormFile("repositoryArchive")
		if err != nil || header == nil || header.Size == 0 {
			api.progress.Fail(progressID, user.ID, "请选择仓库 ZIP 文件")
			writeError(response, domain.NewAPIError(400, "ARCHIVE_REQUIRED", "请选择仓库 ZIP 文件"))
			return
		}
		defer archive.Close()
		if header.Size > int64(settings.MaxImportBytes) {
			api.progress.Fail(progressID, user.ID, "仓库压缩包超过允许大小")
			writeError(response, domain.NewAPIError(413, "ARCHIVE_TOO_LARGE", "仓库压缩包超过允许大小"))
			return
		}
		sourceURI = filepath.Base(header.Filename)
		api.progress.Update(progressID, user.ID, 12, "正在读取仓库压缩包")
		workspace, err = api.importer.ImportArchive(request.Context(), projectID, archive, header.Size)
		if err != nil {
			api.progress.Fail(progressID, user.ID, publicImportError(err))
			writeError(response, err)
			return
		}
	} else {
		sourceURI = value("githubUrl")
		if sourceURI == "" {
			api.progress.Fail(progressID, user.ID, "GitHub 仓库地址不能为空")
			writeError(response, domain.NewAPIError(400, "INVALID_GITHUB_URL", "GitHub 仓库地址不能为空"))
			return
		}
		api.progress.Update(progressID, user.ID, 12, "正在连接 GitHub")
		result, err := api.importer.CloneGitHub(request.Context(), projectID, sourceURI, value("githubBranch"), effectiveToken)
		if err != nil {
			api.progress.Fail(progressID, user.ID, publicImportError(err))
			writeError(response, err)
			return
		}
		workspace, sourceURI, sourceBranch = result.Workspace, result.URL, result.Branch
	}
	keepWorkspace = true
	api.progress.Update(progressID, user.ID, 78, "正在扫描仓库文件")
	texCandidates, err := importer.ListTexFiles(workspace)
	if err != nil {
		api.progress.Fail(progressID, user.ID, publicImportError(err))
		writeError(response, err)
		return
	}
	if texRoot == "" && len(texCandidates) > 0 {
		for _, candidate := range texCandidates {
			if candidate == "main.tex" {
				texRoot = candidate
				break
			}
		}
	}
	if texRoot != "" && !containsString(texCandidates, texRoot) {
		api.progress.Fail(progressID, user.ID, "TeX 入口文件不存在")
		writeError(response, domain.NewAPIError(400, "TEX_ROOT_NOT_FOUND", "TeX 入口文件不存在于仓库中，请从候选文件中选择"))
		return
	}
	api.progress.Update(progressID, user.ID, 88, "正在检查审阅用 PDF")
	pdfPath := requestedPDFPath
	if pdfPath == "" {
		pdfPath = "main.pdf"
	}
	sourcePDF, pdfErr := managedWorkspacePath(workspace, pdfPath)
	if pdfErr == nil {
		if _, err := os.Stat(sourcePDF); err != nil {
			pdfErr = err
		}
	}
	if pdfErr != nil {
		// Preserve the original browser contract: a repository without a built
		// PDF becomes a pending project instead of asking for a deployment path.
		pending, err := api.store.CreatePendingProject(request.Context(), workspace, repository.PendingProjectPayload{
			UserID: user.ID, Name: name, Slug: slug, Description: description,
			SourceType: sourceType, SourceURI: sourceURI, SourceBranch: sourceBranch,
			SourceCredentialCiphertext: api.encryptOptional(effectiveToken), RequestedPDFPath: requestedPDFPath,
			GitUserName: gitUserName, GitUserEmail: gitUserEmail,
		})
		if err != nil {
			keepWorkspace = false
			writeError(response, err)
			return
		}
		api.progress.Complete(progressID, user.ID, "等待选择 PDF 处理方式")
		writeJSON(response, http.StatusAccepted, map[string]any{
			"pending":       map[string]any{"id": pending.ID, "requestedPdfPath": requestedPDFPath},
			"texCandidates": texCandidates, "texRootRequired": texRoot == "",
		})
		return
	}
	if err := api.assertPDF(request.Context(), sourcePDF); err != nil {
		writeError(response, err)
		return
	}
	project, err := api.finalizeImportedProject(request.Context(), user, projectID, name, slug, description, workspace, sourcePDF, sourceType, sourceURI, sourceBranch, api.encryptOptional(effectiveToken), gitUserName, gitUserEmail, texRoot)
	if err != nil {
		writeError(response, err)
		return
	}
	api.progress.Complete(progressID, user.ID, "项目创建完成")
	writeJSON(response, http.StatusCreated, map[string]any{"project": project, "texRootCandidates": texCandidates, "texRootRequired": texRoot == ""})
}

func (api *API) userGitHubToken(ctx context.Context, userID string) string {
	defaults, err := api.store.UserVscodeDefaults(ctx, userID)
	if err != nil || defaults.AccessTokenCiphertext == "" || api.encryptionKey == "" {
		return ""
	}
	token, err := secretbox.Decrypt(api.encryptionKey, defaults.AccessTokenCiphertext)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

func (api *API) encryptOptional(value string) string {
	if strings.TrimSpace(value) == "" || api.encryptionKey == "" {
		return ""
	}
	encoded, err := secretbox.Encrypt(api.encryptionKey, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return encoded
}

func (api *API) finalizeImportedProject(ctx context.Context, user domain.User, id, name, slug, description, workspace, sourcePDF, sourceType, sourceURI, sourceBranch, credential, gitUserName, gitUserEmail, texRoot string) (map[string]any, error) {
	project := repository.NewProject{ID: id, Name: name, Slug: slug, Description: description,
		RepositoryPath: relativeWorkingPath(workspace), SourcePDFPath: relativeWorkingPath(sourcePDF),
		SourceType: sourceType, SourceURI: sourceURI, SourceBranch: sourceBranch,
		WorkspacePath: relativeWorkingPath(workspace), SourceCredentialCiphertext: credential, CreatedByUserID: user.ID}
	if err := api.store.CreateProject(ctx, project); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, domain.NewAPIError(409, "SLUG_EXISTS", "URL 标识已被其他项目使用")
		}
		return nil, err
	}
	if gitUserName != "" || gitUserEmail != "" || credential != "" {
		if err := api.store.SaveProjectGitSettings(ctx, repository.GitSettings{ProjectID: id, UserName: gitUserName, UserEmail: gitUserEmail}); err != nil {
			return nil, err
		}
	}
	if texRoot != "" {
		if err := api.store.SaveProjectTexSettings(ctx, repository.TexSettings{ProjectID: id, Engine: "pdflatex", BuildTool: "latexmk", OutputDirectory: "build", AutoBuild: "off", PDFPreview: "review-hub", SyncTex: true, TexRootPath: texRoot}); err != nil {
			return nil, err
		}
	}
	api.store.Audit(ctx, &id, &user.ID, "project", id, "created", map[string]any{"name": name, "slug": slug, "sourceType": sourceType, "sourceUri": sourceURI, "sourceBranch": sourceBranch})
	return api.store.PublicProject(ctx, id, user)
}

func managedWorkspacePath(workspace, relative string) (string, error) {
	relative = filepath.Clean(filepath.FromSlash(strings.TrimSpace(relative)))
	if relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", domain.NewAPIError(400, "PATH_NOT_ALLOWED", "PDF 路径必须位于导入仓库内")
	}
	path := filepath.Join(workspace, relative)
	if !pathInside(workspace, path) {
		return "", domain.NewAPIError(400, "PATH_NOT_ALLOWED", "PDF 路径必须位于导入仓库内")
	}
	return path, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func publicImportError(err error) string {
	if apiError, ok := err.(*domain.APIError); ok {
		return apiError.Message
	}
	return "项目导入失败"
}

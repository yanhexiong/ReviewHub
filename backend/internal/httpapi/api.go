package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/importer"
	"github.com/yanhexiong/review-hub/backend/internal/importprogress"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/service"
	appupdate "github.com/yanhexiong/review-hub/backend/internal/update"
)

type API struct {
	mux             *http.ServeMux
	store           *repository.Store
	auth            *service.AuthService
	review          *service.ReviewService
	updater         *appupdate.Service
	logger          *slog.Logger
	encryptionKey   string
	dataDirectory   string
	staticDirectory string
	importer        *importer.Service
	progress        *importprogress.Service
	listenerWriter  func(host string, port int) error
	operationMu     sync.RWMutex
	snapshotMu      sync.Mutex
}

func New(store *repository.Store, auth *service.AuthService, review *service.ReviewService, logger *slog.Logger) *API {
	api := &API{
		mux:      http.NewServeMux(),
		store:    store,
		auth:     auth,
		review:   review,
		logger:   logger,
		progress: importprogress.New(),
	}
	api.registerRoutes()
	return api
}

func (api *API) registerRoutes() {
	api.mux.HandleFunc("GET /api/system/health", api.health)
	api.mux.HandleFunc("GET /api/editor-assets/{path...}", api.editorAsset)
	api.mux.HandleFunc("GET /api/pdf-worker", api.pdfWorker)
	api.mux.HandleFunc("GET /api/auth/register/status", api.registrationStatus)
	api.mux.HandleFunc("GET /api/auth/setup/status", api.setupStatus)
	api.mux.HandleFunc("POST /api/auth/login", api.login)
	api.mux.HandleFunc("POST /api/auth/logout", api.logout)
	api.mux.HandleFunc("GET /api/auth/me", api.me)
	api.mux.HandleFunc("PATCH /api/auth/me", api.updateMe)
	api.mux.HandleFunc("POST /api/auth/register", api.register)
	api.mux.HandleFunc("POST /api/auth/setup", api.setup)
	api.mux.HandleFunc("GET /api/admin/settings", api.adminSettings)
	api.mux.HandleFunc("GET /api/admin", api.adminOverview)
	api.mux.HandleFunc("PATCH /api/admin", api.adminRootPatch)
	api.mux.HandleFunc("POST /api/admin", api.adminRootCreate)
	api.mux.HandleFunc("PATCH /api/admin/users/{userID}", api.adminUserUpdate)
	api.mux.HandleFunc("GET /api/admin/shares", api.adminShares)
	api.mux.HandleFunc("GET /api/admin/operations", api.adminOperations)
	api.mux.HandleFunc("GET /api/admin/backup", api.adminBackup)
	api.mux.HandleFunc("POST /api/admin/restore", api.adminRestore)
	api.mux.HandleFunc("POST /api/admin/migrations", api.adminRestore)
	api.mux.HandleFunc("GET /api/admin/logs", api.auditLogs)
	api.mux.HandleFunc("PATCH /api/admin/logs", api.updateAuditSettings)
	api.mux.HandleFunc("POST /api/admin/logs/purge", api.purgeAuditSettings)
	api.mux.HandleFunc("GET /api/admin/updates", api.adminUpdates)
	api.mux.HandleFunc("POST /api/admin/updates", api.scheduleUpdate)
	api.mux.HandleFunc("POST /api/admin/updates/offline", api.scheduleOfflineUpdate)
	api.mux.HandleFunc("PATCH /api/admin/settings", api.updateAdminSettings)
	api.mux.HandleFunc("PATCH /api/admin/listener", api.updateListener)
	api.mux.HandleFunc("GET /api/admin/texlive-profiles", api.adminTexLiveProfiles)
	api.mux.HandleFunc("POST /api/admin/texlive-profiles", api.adminTexLiveProfiles)
	api.mux.HandleFunc("PATCH /api/admin/texlive-profiles/{profileID}", api.adminTexLiveProfile)
	api.mux.HandleFunc("DELETE /api/admin/texlive-profiles/{profileID}", api.adminTexLiveProfile)
	api.mux.HandleFunc("GET /api/texlive-profiles", api.publicTexLiveProfiles)
	api.mux.HandleFunc("GET /api/settings/allowed-roots", api.allowedRoots)
	api.mux.HandleFunc("PATCH /api/settings/allowed-roots", api.updateAllowedRoots)
	api.mux.HandleFunc("GET /api/me/vscode-defaults", api.userVscodeDefaults)
	api.mux.HandleFunc("PUT /api/me/vscode-defaults", api.saveUserVscodeDefaults)

	api.mux.HandleFunc("GET /api/projects", api.projects)
	api.mux.HandleFunc("POST /api/projects", api.createProject)
	api.mux.HandleFunc("POST /api/projects/{projectID}/sync", api.syncProject)
	api.mux.HandleFunc("POST /api/projects/pending/{pendingID}/source-pdf", api.usePendingPDF)
	api.mux.HandleFunc("POST /api/projects/pending/{pendingID}/compile", api.compilePendingProject)
	api.mux.HandleFunc("DELETE /api/projects/pending/{pendingID}", api.deletePendingProject)
	api.mux.HandleFunc("GET /api/projects/{projectID}/editor", api.projectEditor)
	api.mux.HandleFunc("PUT /api/projects/{projectID}/editor", api.projectEditor)
	api.mux.HandleFunc("GET /api/projects/{projectID}", api.project)
	api.mux.HandleFunc("PATCH /api/projects/{projectID}", api.updateProject)
	api.mux.HandleFunc("DELETE /api/projects/{projectID}", api.deleteProject)
	api.mux.HandleFunc("GET /api/projects/{projectID}/collaborators", api.collaborators)
	api.mux.HandleFunc("POST /api/projects/{projectID}/collaborators", api.saveCollaborator)
	api.mux.HandleFunc("DELETE /api/projects/{projectID}/collaborators/{userID}", api.deleteCollaborator)
	api.mux.HandleFunc("GET /api/projects/{projectID}/snapshots", api.snapshots)
	api.mux.HandleFunc("POST /api/projects/{projectID}/snapshots", api.createSnapshot)
	api.mux.HandleFunc("PATCH /api/projects/{projectID}/snapshots/{snapshotID}", api.updateSnapshotMetadata)
	api.mux.HandleFunc("DELETE /api/projects/{projectID}/snapshots/{snapshotID}", api.deleteSnapshot)
	api.mux.HandleFunc("GET /api/projects/{projectID}/snapshots/{snapshotID}/pdf", api.snapshotPDF)
	api.mux.HandleFunc("GET /api/projects/{projectID}/compare", api.compareSnapshots)
	api.mux.HandleFunc("GET /api/projects/{projectID}/vscode/settings", api.vscodeSettings)
	api.mux.HandleFunc("PUT /api/projects/{projectID}/vscode/settings", api.vscodeSettings)
	api.mux.HandleFunc("GET /api/projects/{projectID}/vscode", api.vscodeStatus)
	api.mux.HandleFunc("POST /api/projects/{projectID}/vscode/restart", api.restartVscode)
	api.mux.HandleFunc("POST /api/projects/{projectID}/vscode/return-source", api.returnVscodeSource)
	api.mux.HandleFunc("POST /api/projects/{projectID}/vscode/switch-source", api.switchVscodeSource)
	api.mux.HandleFunc("/vscode/projects/{projectID}", api.vscodeProxy)
	api.mux.HandleFunc("/vscode/projects/{projectID}/{path...}", api.vscodeProxy)
	api.mux.HandleFunc("GET /api/projects/{projectID}/export", api.projectExport)
	api.mux.HandleFunc("GET /api/projects/{projectID}/comments", api.comments)
	api.mux.HandleFunc("POST /api/projects/{projectID}/comments", api.createComment)
	api.mux.HandleFunc("GET /api/projects/{projectID}/git-config", api.projectGitSettings)
	api.mux.HandleFunc("PUT /api/projects/{projectID}/git-config", api.saveProjectGitSettings)
	api.mux.HandleFunc("GET /api/projects/{projectID}/texlive-config", api.projectTexSettings)
	api.mux.HandleFunc("PUT /api/projects/{projectID}/texlive-config", api.saveProjectTexSettings)
	api.mux.HandleFunc("GET /api/projects/{projectID}/events", api.events)
	api.mux.HandleFunc("GET /api/projects/{projectID}/shares", api.projectShares)
	api.mux.HandleFunc("POST /api/projects/{projectID}/shares", api.createProjectShare)
	api.mux.HandleFunc("DELETE /api/projects/{projectID}/shares/{shareID}", api.revokeProjectShare)
	api.mux.HandleFunc("DELETE /api/comments/{commentID}", api.deleteComment)
	api.mux.HandleFunc("POST /api/comments/{commentID}/transition", api.transitionComment)
	api.mux.HandleFunc("GET /api/comments/{commentID}/replies", api.replies)
	api.mux.HandleFunc("POST /api/comments/{commentID}/replies", api.createReply)
	api.mux.HandleFunc("GET /api/share/{token}", api.shareOverview)
	api.mux.HandleFunc("POST /api/share/{token}/access", api.shareAccess)
	api.mux.HandleFunc("POST /api/share/{token}/comments", api.shareComment)
	api.mux.HandleFunc("GET /api/share/{token}/events", api.shareEvents)
	api.mux.HandleFunc("GET /api/share/{token}/pdf", api.sharePDF)
}

func (api *API) SetUpdateService(updater *appupdate.Service) {
	api.updater = updater
}

func (api *API) SetEncryptionKey(key string) {
	api.encryptionKey = key
}

func (api *API) SetDataDirectory(directory string) {
	api.dataDirectory = directory
}

// SetStaticDirectory enables the release mode where the Go API serves the
// statically exported Next.js site directly. An empty directory keeps the
// development API-only behavior unchanged.
func (api *API) SetStaticDirectory(directory string) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		api.staticDirectory = ""
		return
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		api.staticDirectory = ""
		return
	}
	api.staticDirectory = filepath.Clean(absolute)
}

func (api *API) SetImporter(service *importer.Service) {
	api.importer = service
}

// SetListenerWriter connects privileged listener changes to the process
// bootstrap configuration. Tests may leave it unset; SQLite still retains the
// displayed setting in that case.
func (api *API) SetListenerWriter(writer func(host string, port int) error) {
	api.listenerWriter = writer
}

func (api *API) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	restoreRequest := request.Method == http.MethodPost && (request.URL.Path == "/api/admin/restore" || request.URL.Path == "/api/admin/migrations")
	if !restoreRequest {
		api.operationMu.RLock()
		defer api.operationMu.RUnlock()
	}
	started := time.Now()
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
	}
	response.Header().Set("X-Request-ID", requestID)
	_, pattern := api.mux.Handler(request)
	handledByGo := pattern != "" || (request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/projects/progress/")) || strings.HasPrefix(request.URL.Path, "/vscode/projects/")
	recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
	defer func() {
		if recovered := recover(); recovered != nil {
			api.logger.Error("api panic recovered", "request_id", requestID, "panic", fmt.Sprint(recovered))
			writeError(recorder, domain.NewAPIError(500, "INTERNAL_ERROR", "服务器处理请求失败"))
		}
		api.logger.Info("api request", "request_id", requestID, "method", request.Method, "path", request.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds(), "handler", handledByGo)
	}()
	if request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/projects/progress/") {
		api.importProgress(recorder, request)
		return
	}
	if handledByGo {
		// ServeMux populates Request.PathValue for patterns such as
		// /api/projects/{projectID}; invoking its selected handler directly does not.
		api.mux.ServeHTTP(recorder, request)
		return
	}
	if api.staticDirectory != "" && (request.Method == http.MethodGet || request.Method == http.MethodHead) {
		api.serveStatic(recorder, request)
		return
	}
	writeJSON(recorder, http.StatusNotFound, map[string]any{
		"error": map[string]string{
			"code":    "API_NOT_FOUND",
			"message": "请求的 API 不存在",
		},
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Flush() {
	if flusher, ok := recorder.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ReverseProxy uses Hijacker for HTTP Upgrade requests such as code-server's
// WebSocket transport. Preserve that optional ResponseWriter capability while
// retaining request-status accounting for ordinary responses.
func (recorder *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := recorder.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (recorder *statusRecorder) Unwrap() http.ResponseWriter { return recorder.ResponseWriter }

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, err error) {
	var apiError *domain.APIError
	if errors.As(err, &apiError) {
		writeJSON(response, apiError.Status, map[string]any{"error": map[string]string{"code": apiError.Code, "message": apiError.Message}})
		return
	}
	writeJSON(response, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "INTERNAL_ERROR", "message": "服务器处理请求失败"}})
}

func decodeJSON(request *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	if err := decoder.Decode(destination); err != nil {
		return domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	return nil
}

func (api *API) health(response http.ResponseWriter, request *http.Request) {
	if err := api.store.Ping(request.Context()); err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{"status": "error", "code": "DATABASE_UNAVAILABLE"})
		return
	}
	version, err := api.store.SchemaVersion(request.Context())
	if err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{"status": "error", "code": "DATABASE_UNAVAILABLE"})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"status": "ok", "engine": "sqlite", "schemaVersion": version})
}

func (api *API) registrationStatus(response http.ResponseWriter, request *http.Request) {
	open, err := api.store.RegistrationOpen(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"registrationOpen": open})
}

func (api *API) setupStatus(response http.ResponseWriter, request *http.Request) {
	required, err := api.store.SetupRequired(request.Context())
	if err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{"setupRequired": true, "database": map[string]string{"status": "error"}})
		return
	}
	version, err := api.store.SchemaVersion(request.Context())
	if err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{"setupRequired": true, "database": map[string]string{"status": "error"}})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"setupRequired": required, "database": map[string]any{"status": "ok", "engine": "sqlite", "schemaVersion": version}})
}

func (api *API) login(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		RememberMe bool   `json:"rememberMe"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	user, cookie, err := api.auth.Login(request.Context(), input.Email, input.Password, input.RememberMe)
	if err != nil {
		writeError(response, err)
		return
	}
	http.SetCookie(response, cookie)
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (api *API) logout(response http.ResponseWriter, _ *http.Request) {
	http.SetCookie(response, api.auth.LogoutCookie())
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) me(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (api *API) updateMe(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		DisplayName string `json:"displayName"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	user, err = api.auth.UpdateDisplayName(request.Context(), user, input.DisplayName)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (api *API) register(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	user, cookie, err := api.auth.Register(request.Context(), input.Email, input.Password)
	if err != nil {
		writeError(response, err)
		return
	}
	http.SetCookie(response, cookie)
	writeJSON(response, http.StatusCreated, map[string]any{"user": user})
}

func (api *API) setup(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email           string `json:"email"`
		DisplayName     string `json:"displayName"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirmPassword"`
		Listener        struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"listener"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	user, cookie, err := api.auth.Setup(request.Context(), input.Email, input.DisplayName, input.Password, input.ConfirmPassword, input.Listener.Host, input.Listener.Port)
	if err != nil {
		writeError(response, err)
		return
	}
	if api.listenerWriter != nil {
		if err := api.listenerWriter(strings.TrimSpace(input.Listener.Host), input.Listener.Port); err != nil {
			writeError(response, err)
			return
		}
	}
	http.SetCookie(response, cookie)
	writeJSON(response, http.StatusCreated, map[string]any{"user": user, "listener": input.Listener, "restartRequired": true})
}

func (api *API) adminSettings(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.RequireRole(request, "admin"); err != nil {
		writeError(response, err)
		return
	}
	registrationOpen, err := api.store.RegistrationOpen(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.ResourceSettings(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"registrationOpen":   registrationOpen,
		"maxPdfBytes":        settings.MaxPDFBytes,
		"maxImportBytes":     settings.MaxImportBytes,
		"maxProjectsPerUser": settings.MaxProjectsPerUser,
		"maxUsers":           settings.MaxUsers,
	})
}

func (api *API) adminOverview(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.RequireRole(request, "admin"); err != nil {
		writeError(response, err)
		return
	}
	overview, err := api.store.AdminOverview(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, overview)
}

func (api *API) adminUpdates(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.RequireRole(request, "admin"); err != nil {
		writeError(response, err)
		return
	}
	if api.updater == nil {
		writeError(response, domain.NewAPIError(503, "UPDATE_UNAVAILABLE", "应用更新服务尚未就绪。"))
		return
	}
	writeJSON(response, http.StatusOK, api.updater.Overview(request.Context()))
}

func (api *API) scheduleUpdate(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if api.updater == nil {
		writeError(response, domain.NewAPIError(503, "UPDATE_UNAVAILABLE", "应用更新服务尚未就绪。"))
		return
	}
	var input struct {
		Action  string `json:"action"`
		Version string `json:"version"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	healthURL, err := api.updateHealthURL(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	var version string
	switch input.Action {
	case "apply":
		version, err = api.updater.QueueLatest(request.Context(), healthURL)
	case "rollback-local":
		err = api.updater.QueueLocalRollback(healthURL)
		version = "previous"
	case "rollback-release":
		version, err = api.updater.QueueReleaseRollback(request.Context(), input.Version, healthURL)
	default:
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if err != nil {
		writeUpdateError(response, err)
		return
	}
	action := "update_scheduled"
	if input.Action == "rollback-local" || input.Action == "rollback-release" {
		action = "rollback_scheduled"
	}
	api.store.Audit(request.Context(), nil, &user.ID, "application", "review-hub", action, map[string]string{
		"version": version,
		"source":  "official_release",
	})
	writeJSON(response, http.StatusAccepted, map[string]any{"scheduled": true, "version": version})
}

func (api *API) scheduleOfflineUpdate(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if api.updater == nil {
		writeError(response, domain.NewAPIError(503, "UPDATE_UNAVAILABLE", "应用更新服务尚未就绪。"))
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, appupdate.MaxOfflinePackageBytes)
	multipart, err := request.MultipartReader()
	if err != nil {
		writeError(response, domain.NewAPIError(400, "OFFLINE_PACKAGE_INVALID", "请选择官方离线更新包。"))
		return
	}
	var packageReader io.Reader
	parts := 0
	for {
		part, nextErr := multipart.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			writeError(response, domain.NewAPIError(400, "OFFLINE_PACKAGE_INVALID", "离线更新包上传不完整。"))
			return
		}
		parts++
		if parts > 4 {
			_ = part.Close()
			writeError(response, domain.NewAPIError(400, "OFFLINE_PACKAGE_INVALID", "离线更新请求包含过多字段。"))
			return
		}
		if part.FormName() != "package" || part.FileName() == "" || packageReader != nil {
			_ = part.Close()
			continue
		}
		packageReader = part
		break
	}
	if packageReader == nil {
		writeError(response, domain.NewAPIError(400, "OFFLINE_PACKAGE_INVALID", "请选择官方离线更新包。"))
		return
	}
	defer func() {
		if closer, ok := packageReader.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	healthURL, err := api.updateHealthURL(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	version, err := api.updater.QueueOffline(request.Context(), packageReader, healthURL)
	if err != nil {
		writeUpdateError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &user.ID, "application", "review-hub", "offline_update_scheduled", map[string]string{
		"version": version,
		"source":  "offline_package",
	})
	writeJSON(response, http.StatusAccepted, map[string]any{"scheduled": true, "version": version})
}

func (api *API) updateHealthURL(ctx context.Context) (string, error) {
	listener, err := api.store.ListenerSettings(ctx)
	if err != nil {
		return "", err
	}
	return "http://127.0.0.1:" + strconv.Itoa(listener.Port) + "/api/system/health", nil
}

func writeUpdateError(response http.ResponseWriter, err error) {
	code := appupdate.ErrorCode(err)
	status := http.StatusInternalServerError
	switch code {
	case "ALREADY_UP_TO_DATE", "UPDATE_UNSUPPORTED_RUNTIME", "ROLLBACK_UNAVAILABLE":
		status = http.StatusConflict
	case "INVALID_ROLLBACK_VERSION", "OFFLINE_PACKAGE_INVALID", "UPDATE_CHECKSUM_MISMATCH", "UPDATE_CHECKSUM_UNAVAILABLE":
		status = http.StatusBadRequest
	case "UPDATE_RELEASE_UNAVAILABLE", "UPDATE_DOWNLOAD_FAILED", "UPDATE_ASSET_UNAVAILABLE":
		status = http.StatusBadGateway
	case "UPDATE_HELPER_UNAVAILABLE":
		status = http.StatusServiceUnavailable
	}
	writeError(response, domain.NewAPIError(status, code, err.Error()))
}

var auditEntityPattern = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)
var auditActionPattern = regexp.MustCompile(`^[a-z0-9_.-]{1,120}$`)
var auditSensitiveKeyPattern = regexp.MustCompile(`(?i)(path|root|directory|dir|workspace|archive|patch)$`)

func (api *API) auditLogs(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.RequireRole(request, "admin"); err != nil {
		writeError(response, err)
		return
	}
	filter := auditLogFilter(request)
	result, err := api.store.AuditLogs(request.Context(), filter)
	if err != nil {
		writeError(response, err)
		return
	}
	for _, row := range result.Rows {
		if row["entity_type"] == "project_file" {
			row["entity_id"] = "项目文件"
		}
		row["after_json"] = redactAuditJSON(stringOrEmpty(row["after_json"]))
	}
	entityTypes, err := api.store.AuditEntityTypes(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.AuditSettings(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"audit":       result.Rows,
		"total":       result.Total,
		"limit":       filter.Limit,
		"offset":      filter.Offset,
		"levels":      []string{"info", "warning", "error", "critical"},
		"entityTypes": entityTypes,
		"settings":    settings,
	})
}

func auditLogFilter(request *http.Request) repository.AuditLogFilter {
	query := request.URL.Query()
	filter := repository.AuditLogFilter{
		From:   parseAuditDate(query.Get("from"), false),
		To:     parseAuditDate(query.Get("to"), true),
		Level:  validAuditLevel(query.Get("level")),
		Limit:  boundedInteger(query.Get("limit"), 50, 1, 100),
		Offset: boundedInteger(query.Get("offset"), 0, 0, 1_000_000),
	}
	if value := strings.TrimSpace(query.Get("entityType")); auditEntityPattern.MatchString(value) {
		filter.EntityType = value
	}
	if value := strings.TrimSpace(query.Get("action")); auditActionPattern.MatchString(value) {
		filter.Action = value
	}
	filter.Search = strings.TrimSpace(query.Get("search"))
	if len(filter.Search) > 120 {
		filter.Search = filter.Search[:120]
	}
	return filter
}

func parseAuditDate(value string, endOfDay bool) *int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	if endOfDay {
		parsed = parsed.Add(24 * time.Hour)
	}
	milliseconds := parsed.UnixMilli()
	return &milliseconds
}

func validAuditLevel(value string) string {
	switch value {
	case "info", "warning", "error", "critical":
		return value
	default:
		return ""
	}
}

func redactAuditJSON(value string) any {
	if value == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return "详情已隐藏"
	}
	encoded, err := json.Marshal(redactAuditValue(parsed))
	if err != nil {
		return "详情已隐藏"
	}
	return string(encoded)
}

func redactAuditValue(value any) any {
	switch item := value.(type) {
	case []any:
		for index := range item {
			item[index] = redactAuditValue(item[index])
		}
		return item
	case map[string]any:
		for key, child := range item {
			if auditSensitiveKeyPattern.MatchString(key) {
				item[key] = "已隐藏"
				continue
			}
			item[key] = redactAuditValue(child)
		}
		return item
	default:
		return value
	}
}

func stringOrEmpty(value any) string {
	text, _ := value.(string)
	return text
}

func (api *API) updateAdminSettings(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		RegistrationOpen   bool `json:"registrationOpen"`
		MaxPDFBytes        int  `json:"maxPdfBytes"`
		MaxImportBytes     int  `json:"maxImportBytes"`
		MaxProjectsPerUser int  `json:"maxProjectsPerUser"`
		MaxUsers           int  `json:"maxUsers"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if input.MaxPDFBytes < 1_048_576 || input.MaxPDFBytes > 4_294_967_296 ||
		input.MaxImportBytes < 1_048_576 || input.MaxImportBytes > 8_589_934_592 ||
		input.MaxProjectsPerUser < 0 || input.MaxProjectsPerUser > 100_000 ||
		input.MaxUsers < 1 || input.MaxUsers > 1_000_000 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	settings := repository.ResourceSettings{
		MaxPDFBytes: input.MaxPDFBytes, MaxImportBytes: input.MaxImportBytes,
		MaxProjectsPerUser: input.MaxProjectsPerUser, MaxUsers: input.MaxUsers,
	}
	if err := api.store.SetRegistrationOpen(request.Context(), input.RegistrationOpen); err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.UpdateResourceSettings(request.Context(), settings); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &user.ID, "app_setting", "resource_limits", "updated", settings)
	writeJSON(response, http.StatusOK, map[string]any{
		"registrationOpen":   input.RegistrationOpen,
		"maxPdfBytes":        input.MaxPDFBytes,
		"maxImportBytes":     input.MaxImportBytes,
		"maxProjectsPerUser": input.MaxProjectsPerUser,
		"maxUsers":           input.MaxUsers,
	})
}

func (api *API) updateListener(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if !domain.ValidListener(input.Host, input.Port) {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if err := api.store.SaveListenerSettings(request.Context(), strings.TrimSpace(input.Host), input.Port); err != nil {
		writeError(response, err)
		return
	}
	if api.listenerWriter != nil {
		if err := api.listenerWriter(strings.TrimSpace(input.Host), input.Port); err != nil {
			writeError(response, err)
			return
		}
	}
	api.store.Audit(request.Context(), nil, &user.ID, "app_setting", "listener_settings", "updated", map[string]any{"host": input.Host, "port": input.Port})
	writeJSON(response, http.StatusOK, map[string]any{"listener": map[string]any{"host": input.Host, "port": input.Port}, "restartRequired": true})
}

func (api *API) allowedRoots(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	roots, err := api.store.AllowedRoots(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	// Deployment paths are hidden by default. An authenticated administrator can
	// explicitly request them while editing this privileged setting.
	visibleRoots := []string{}
	if request.URL.Query().Get("reveal") == "1" {
		visibleRoots = roots
	}
	writeJSON(response, http.StatusOK, map[string]any{"roots": visibleRoots, "configuredCount": len(roots), "configuredBy": user.Email})
}

func (api *API) updateAllowedRoots(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Roots []string `json:"roots"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if len(input.Roots) == 0 || len(input.Roots) > 20 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	roots := make([]string, 0, len(input.Roots))
	seen := map[string]bool{}
	for _, root := range input.Roots {
		canonical, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			writeError(response, domain.NewAPIError(400, "ROOT_NOT_FOUND", "目录不存在或不可访问"))
			return
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			writeError(response, domain.NewAPIError(400, "ROOT_NOT_FOUND", "目录不存在或不可访问"))
			return
		}
		if !seen[canonical] {
			seen[canonical] = true
			roots = append(roots, canonical)
		}
	}
	if err := api.store.SaveAllowedRoots(request.Context(), roots); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &user.ID, "app_setting", "allowed_roots", "updated", map[string]any{"count": len(roots)})
	writeJSON(response, http.StatusOK, map[string]int{"configuredCount": len(roots)})
}

func (api *API) projects(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projects, err := api.review.ListProjects(request.Context(), user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"projects": projects})
}

func (api *API) project(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	project, err := api.review.Project(request.Context(), request.PathValue("projectID"), user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"project": project})
}

func (api *API) collaborators(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	owner, collaborators, err := api.review.Collaborators(request.Context(), request.PathValue("projectID"), user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"owner": owner, "collaborators": collaborators})
}

func (api *API) saveCollaborator(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Email      string `json:"email"`
		Permission string `json:"permission"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	owner, collaborators, err := api.review.SaveCollaborator(request.Context(), request.PathValue("projectID"), input.Email, input.Permission, user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"owner": owner, "collaborators": collaborators})
}

func (api *API) deleteCollaborator(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if err := api.review.RemoveCollaborator(request.Context(), request.PathValue("projectID"), request.PathValue("userID"), user); err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) snapshots(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	snapshots, err := api.review.Snapshots(request.Context(), projectID, user)
	if err != nil {
		writeError(response, err)
		return
	}
	pdfFiles := []projectPDFFile{}
	if request.URL.Query().Get("includePdfFiles") == "1" {
		if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
			writeError(response, err)
			return
		}
		pdfFiles, err = api.listProjectPDFFiles(request.Context(), projectID)
		if err != nil {
			writeError(response, err)
			return
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{"snapshots": snapshots, "pdfFiles": pdfFiles})
}

func (api *API) updateSnapshotMetadata(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Label string `json:"label"`
		Note  string `json:"note"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	snapshot, err := api.review.UpdateSnapshotMetadata(request.Context(), request.PathValue("projectID"), request.PathValue("snapshotID"), input.Label, input.Note, user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"snapshot": snapshot})
}

func (api *API) comments(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	filter := repository.CommentFilter{
		SnapshotID: request.URL.Query().Get("snapshotId"),
		Status:     request.URL.Query().Get("status"),
		Category:   request.URL.Query().Get("category"),
		Priority:   request.URL.Query().Get("priority"),
		Limit:      boundedInteger(request.URL.Query().Get("limit"), 500, 1, 1000),
		Offset:     boundedInteger(request.URL.Query().Get("offset"), 0, 0, int(^uint(0)>>1)),
	}
	comments, err := api.review.ListComments(request.Context(), request.PathValue("projectID"), user, filter)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"comments": comments})
}

func (api *API) createComment(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input repository.NewComment
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	comment, err := api.review.CreateComment(request.Context(), request.PathValue("projectID"), user, input)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"comment": comment})
}

func (api *API) deleteComment(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if _, err := api.review.DeleteComment(request.Context(), request.PathValue("commentID"), user); err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) transitionComment(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Status string  `json:"status"`
		Note   *string `json:"note"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if _, err := api.review.TransitionComment(request.Context(), request.PathValue("commentID"), input.Status, input.Note, user); err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) replies(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	replies, err := api.review.ListReplies(request.Context(), request.PathValue("commentID"), user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"replies": replies})
}

func (api *API) createReply(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	reply, _, _, err := api.review.CreateReply(request.Context(), request.PathValue("commentID"), input.Content, user)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"id": reply["id"]})
}

func (api *API) events(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	channel, cancel, revision, err := api.review.Subscribe(request.Context(), request.PathValue("projectID"), user)
	if err != nil {
		writeError(response, err)
		return
	}
	defer cancel()
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, domain.NewAPIError(500, "INTERNAL_ERROR", "服务器处理请求失败"))
		return
	}
	writeSSE(response, "connected", map[string]any{"projectId": request.PathValue("projectID"), "revision": revision})
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case event := <-channel:
			writeSSE(response, event.Type, event)
			flusher.Flush()
		case <-heartbeat.C:
			writeSSE(response, "heartbeat", map[string]any{"projectId": request.PathValue("projectID"), "revision": revision})
			flusher.Flush()
		}
	}
}

func writeSSE(response http.ResponseWriter, event string, payload any) {
	encoded, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, encoded)
}

func boundedInteger(value string, fallback, minimum, maximum int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum {
		return fallback
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}

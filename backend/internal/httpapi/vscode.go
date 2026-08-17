package httpapi

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/secretbox"
)

const (
	vscodeStartupTimeout = 30 * time.Second
	vscodeIdleTimeout    = 30 * time.Minute
	vscodeMaxInstances   = 8
)

type vscodeInstance struct {
	projectID        string
	repositoryPath   string
	sourceSnapshotID string
	port             int
	authCookie       string
	command          *exec.Cmd
	done             chan struct{}
	lastUsedAt       time.Time
	sshKeyPath       string
}

type vscodeManager struct {
	mu        sync.Mutex
	instances map[string]*vscodeInstance
	starts    map[string]chan struct{}
}

var apiVscodeManagers sync.Map // map[*API]*vscodeManager

func (api *API) vscode() *vscodeManager {
	value, _ := apiVscodeManagers.LoadOrStore(api, &vscodeManager{
		instances: map[string]*vscodeInstance{},
		starts:    map[string]chan struct{}{},
	})
	return value.(*vscodeManager)
}

// Close releases only code-server child processes owned by this API process.
func (api *API) Close() {
	manager := api.vscode()
	manager.mu.Lock()
	instances := make([]*vscodeInstance, 0, len(manager.instances))
	for _, instance := range manager.instances {
		instances = append(instances, instance)
	}
	manager.instances = map[string]*vscodeInstance{}
	manager.mu.Unlock()
	for _, instance := range instances {
		stopVscodeInstance(instance)
	}
	apiVscodeManagers.Delete(api)
}

func (api *API) vscodeBinary() string {
	if configured := strings.TrimSpace(os.Getenv("PAPER_REVIEW_VSCODE_SERVER_BIN")); configured != "" {
		return filepath.Clean(configured)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(workingDirectory, "vendor", "code-server", "bin", "code-server")
}

func (api *API) vscodeAvailable() (string, error) {
	binary := api.vscodeBinary()
	info, err := os.Stat(binary)
	if binary == "" || err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return "", domain.NewAPIError(503, "VSCODE_SERVER_UNAVAILABLE", "在线编辑器服务暂不可用，请联系管理员检查独立编辑器服务配置。")
	}
	if strings.Contains(strings.ToLower(filepath.Base(binary)), "openvscode") {
		return "", domain.NewAPIError(503, "VSCODE_SERVER_UNSUPPORTED", "当前部署需要带密码认证的 code-server；openvscode-server 暂不支持此项目代理。")
	}
	if err := api.checkVscodeExtensions(); err != nil {
		return "", err
	}
	return binary, nil
}

func (api *API) vscodeExtensionsDirectory() string {
	if configured := strings.TrimSpace(os.Getenv("REVIEW_HUB_VSCODE_EXTENSIONS_DIR")); configured != "" {
		if absolute, err := filepath.Abs(configured); err == nil {
			return absolute
		}
	}
	return filepath.Join(api.dataDirectory, "vscode-extensions")
}

func (api *API) checkVscodeExtensions() error {
	const id, version = "james-yu.latex-workshop", "10.12.0"
	path := filepath.Join(api.vscodeExtensionsDirectory(), id+"-"+version, "package.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.NewAPIError(503, "VSCODE_EXTENSIONS_MISSING", "缺少固定 VS Code 扩展：james-yu.latex-workshop@10.12.0")
	}
	var manifest struct {
		Name, Publisher, Version string
	}
	if json.Unmarshal(raw, &manifest) != nil || strings.ToLower(manifest.Publisher+"."+manifest.Name) != id || manifest.Version != version {
		return domain.NewAPIError(503, "VSCODE_EXTENSIONS_MISSING", "缺少固定 VS Code 扩展：james-yu.latex-workshop@10.12.0")
	}
	return nil
}

func (api *API) projectEditorRoot(ctx context.Context, projectID string, user domain.User) (map[string]any, string, error) {
	project, err := api.store.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	if _, err := api.store.ProjectAccess(ctx, projectID, user, "manage"); err != nil {
		return nil, "", err
	}
	repositoryPath, _ := project["repository_path"].(string)
	root, err := filepath.EvalSymlinks(strings.TrimSpace(repositoryPath))
	if err != nil {
		return nil, "", domain.NewAPIError(404, "EDITOR_ROOT_NOT_FOUND", "项目源码目录不存在")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, "", domain.NewAPIError(404, "EDITOR_ROOT_NOT_FOUND", "项目源码目录不存在")
	}
	return project, root, nil
}

func (api *API) vscodeStatus(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err == nil {
		_, _, err = api.projectEditorRoot(request.Context(), request.PathValue("projectID"), user)
	}
	if err != nil {
		writeError(response, err)
		return
	}
	if _, err := api.vscodeAvailable(); err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]any{"mode": "unavailable", "error": apiErrorBody(err)})
		return
	}
	projectID := request.PathValue("projectID")
	writeJSON(response, http.StatusOK, map[string]any{
		"mode":          "vscode-server",
		"url":           "/vscode/projects/" + url.PathEscape(projectID) + "/",
		"sourceVersion": api.activeVscodeSource(projectID),
	})
}

func apiErrorBody(err error) map[string]string {
	var apiError *domain.APIError
	if errors.As(err, &apiError) {
		return map[string]string{"code": apiError.Code, "message": apiError.Message}
	}
	return map[string]string{"code": "INTERNAL_ERROR", "message": "服务器处理请求失败"}
}

func (api *API) restartVscode(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err == nil {
		_, _, err = api.projectEditorRoot(request.Context(), request.PathValue("projectID"), user)
	}
	if err != nil {
		writeError(response, err)
		return
	}
	api.stopVscode(request.PathValue("projectID"))
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) returnVscodeSource(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	projectID := request.PathValue("projectID")
	var root string
	if err == nil {
		_, root, err = api.projectEditorRoot(request.Context(), projectID, user)
	}
	if err == nil {
		err = api.ensureVscode(request.Context(), projectID, root, "")
	}
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": "/projects/" + url.PathEscape(projectID) + "/editor"})
}

func (api *API) switchVscodeSource(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	projectID := request.PathValue("projectID")
	if err == nil {
		_, _, err = api.projectEditorRoot(request.Context(), projectID, user)
	}
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		SnapshotID string `json:"snapshotId"`
	}
	if err := decodeJSON(request, &input); err != nil || !safeVscodeID(input.SnapshotID) {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	snapshotID := strings.TrimSpace(input.SnapshotID)
	root, err := api.sourceVersionExport(request.Context(), projectID, snapshotID)
	if err == nil {
		err = api.ensureVscode(request.Context(), projectID, root, snapshotID)
	}
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"url": "/projects/" + url.PathEscape(projectID) + "/editor?source=" + url.QueryEscape(snapshotID)})
}

func safeVscodeID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

// sourceVersionExport uses only git archive/rev-parse. It never alters the
// source repository or its Git state; the rendered source lives under data/.
func (api *API) sourceVersionExport(ctx context.Context, projectID, snapshotID string) (string, error) {
	if !safeVscodeID(projectID) || !safeVscodeID(snapshotID) {
		return "", domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	snapshot, err := api.store.SnapshotByID(ctx, projectID, snapshotID)
	if err != nil {
		return "", err
	}
	if snapshot == nil {
		return "", domain.NewAPIError(404, "NOT_FOUND", "快照不存在")
	}
	sha, _ := snapshot["git_commit_sha"].(string)
	if strings.TrimSpace(sha) == "" {
		return "", domain.NewAPIError(409, "SOURCE_COMMIT_MISSING", "该版本没有 Git 提交信息，无法切换到对应源码")
	}
	project, err := api.store.ProjectByID(ctx, projectID)
	if err != nil {
		return "", err
	}
	repositoryPath, _ := project["repository_path"].(string)
	repositoryPath, err = filepath.EvalSymlinks(repositoryPath)
	if err != nil {
		return "", domain.NewAPIError(404, "EDITOR_ROOT_NOT_FOUND", "项目源码目录不存在")
	}
	worktree := filepath.Join(api.dataDirectory, "source-versions", projectID, snapshotID)
	marker := filepath.Join(worktree, ".review-hub-source-sha")
	if stored, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(stored)) == sha {
		return worktree, nil
	}
	if err := os.RemoveAll(worktree); err != nil {
		return "", err
	}
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		return "", err
	}
	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", sha)
	archive.Dir = repositoryPath
	archive.Env = gitReadOnlyEnvironment()
	stream, err := archive.StdoutPipe()
	if err != nil {
		return "", err
	}
	var diagnostics limitedVscodeBuffer
	archive.Stderr = &diagnostics
	if err := archive.Start(); err != nil {
		return "", domain.NewAPIError(409, "SOURCE_EXPORT_FAILED", "无法导出该版本的独立源码工作区")
	}
	extractErr := extractVscodeTar(stream, worktree)
	waitErr := archive.Wait()
	if extractErr != nil || waitErr != nil {
		_ = os.RemoveAll(worktree)
		return "", domain.NewAPIError(409, "SOURCE_EXPORT_FAILED", "无法导出该版本的独立源码工作区")
	}
	if patch, _ := snapshot["source_patch"].(string); patch != "" {
		if err := applySourcePatch(ctx, worktree, patch); err != nil {
			_ = os.RemoveAll(worktree)
			return "", domain.NewAPIError(409, "SOURCE_PATCH_APPLY_FAILED", "无法恢复该版本记录的源码改动")
		}
	}
	if archivePath, _ := snapshot["source_untracked_archive_path"].(string); archivePath != "" {
		path, err := api.managedDataPath(archivePath)
		if err != nil || extractVscodeGzipTar(path, worktree) != nil {
			_ = os.RemoveAll(worktree)
			return "", domain.NewAPIError(409, "SOURCE_UNTRACKED_RESTORE_FAILED", "无法恢复该版本记录的未跟踪文件")
		}
	}
	if err := os.WriteFile(marker, []byte(sha+"\n"), 0o600); err != nil {
		return "", err
	}
	return worktree, nil
}

func gitReadOnlyEnvironment() []string {
	return append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_OPTIONAL_LOCKS=0")
}

func applySourcePatch(ctx context.Context, directory, patch string) error {
	command := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
	command.Dir = directory
	command.Env = gitReadOnlyEnvironment()
	command.Stdin = strings.NewReader(patch)
	return command.Run()
}

func extractVscodeGzipTar(path, destination string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	reader, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer reader.Close()
	return extractVscodeTar(reader, destination)
}

func extractVscodeTar(input io.Reader, destination string) error {
	reader := tar.NewReader(io.LimitReader(input, 2<<30))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return errors.New("unsafe archive path")
		}
		path := filepath.Join(destination, name)
		if !pathInside(destination, path) {
			return errors.New("archive path escapes destination")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > 1<<30 {
				return errors.New("invalid archive file size")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, io.LimitReader(reader, header.Size))
			closeErr := output.Close()
			if copyErr != nil || closeErr != nil {
				return errors.New("archive write failed")
			}
		default:
			return errors.New("unsupported archive entry")
		}
	}
}

func (api *API) activeVscodeSource(projectID string) any {
	manager := api.vscode()
	manager.mu.Lock()
	instance := manager.instances[projectID]
	manager.mu.Unlock()
	if instance == nil || instance.sourceSnapshotID == "" {
		return nil
	}
	return map[string]string{"snapshotId": instance.sourceSnapshotID}
}

func instanceAlive(instance *vscodeInstance) bool {
	if instance == nil {
		return false
	}
	select {
	case <-instance.done:
		return false
	default:
		return true
	}
}

func (api *API) ensureVscode(ctx context.Context, projectID, root, sourceSnapshotID string) error {
	manager := api.vscode()
	for {
		manager.mu.Lock()
		current := manager.instances[projectID]
		if instanceAlive(current) && current.repositoryPath == root {
			current.lastUsedAt = time.Now()
			current.sourceSnapshotID = sourceSnapshotID
			manager.mu.Unlock()
			return nil
		}
		if current != nil {
			delete(manager.instances, projectID)
		}
		if waiting := manager.starts[projectID]; waiting != nil {
			manager.mu.Unlock()
			select {
			case <-waiting:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		waiting := make(chan struct{})
		manager.starts[projectID] = waiting
		manager.mu.Unlock()
		err := api.startVscode(ctx, projectID, root, sourceSnapshotID)
		manager.mu.Lock()
		delete(manager.starts, projectID)
		close(waiting)
		manager.mu.Unlock()
		return err
	}
}

func (api *API) startVscode(ctx context.Context, projectID, root, sourceSnapshotID string) error {
	binary, err := api.vscodeAvailable()
	if err != nil {
		return err
	}
	api.evictVscodeInstances()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return domain.NewAPIError(503, "VSCODE_PORT_ERROR", "无法分配 VS Code Server 端口")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	instanceRoot := filepath.Join(api.dataDirectory, "vscode-workspaces", projectID)
	userDataDir := filepath.Join(instanceRoot, "user-data")
	if err := api.configureVscodeUserData(ctx, userDataDir, projectID); err != nil {
		return err
	}
	gitEnvironment, sshKeyPath, err := api.vscodeGitEnvironment(ctx, projectID, instanceRoot)
	if err != nil {
		return err
	}
	cookieBytes := make([]byte, 32)
	if _, err := rand.Read(cookieBytes); err != nil {
		return err
	}
	authCookie := hex.EncodeToString(cookieBytes)
	environment := append([]string{}, os.Environ()...)
	environment = append(environment, gitEnvironment...)
	environment = append(environment, "HASHED_PASSWORD="+authCookie, "XDG_CONFIG_HOME="+filepath.Join(instanceRoot, "config"))
	environment = withoutEnvironment(environment, "PORT", "VSCODE_IPC_HOOK_CLI", "VSCODE_CLIENT_COMMAND", "VSCODE_CLIENT_COMMAND_CWD", "VSCODE_CLI_AUTHORITY")
	args := []string{
		"--bind-addr=127.0.0.1:" + strconv.Itoa(port), "--auth=password", "--disable-telemetry", "--disable-update-check", "--disable-workspace-trust", "--ignore-last-opened",
		"--user-data-dir=" + userDataDir, "--extensions-dir=" + api.vscodeExtensionsDirectory(), root,
	}
	command := exec.Command(binary, args...)
	command.Dir, command.Env = root, environment
	var logs limitedVscodeBuffer
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		_ = os.Remove(sshKeyPath)
		return domain.NewAPIError(503, "VSCODE_SERVER_START_FAILED", "VS Code Server 启动失败")
	}
	instance := &vscodeInstance{projectID: projectID, repositoryPath: root, sourceSnapshotID: sourceSnapshotID, port: port, authCookie: authCookie, command: command, done: make(chan struct{}), lastUsedAt: time.Now(), sshKeyPath: sshKeyPath}
	go func() {
		_ = command.Wait()
		close(instance.done)
		manager := api.vscode()
		manager.mu.Lock()
		if manager.instances[projectID] == instance {
			delete(manager.instances, projectID)
		}
		manager.mu.Unlock()
	}()
	deadline := time.Now().Add(vscodeStartupTimeout)
	for time.Now().Before(deadline) {
		if !instanceAlive(instance) {
			return domain.NewAPIError(503, "VSCODE_SERVER_START_FAILED", "VS Code Server 启动失败")
		}
		if vscodeProbe(port, authCookie) {
			manager := api.vscode()
			manager.mu.Lock()
			manager.instances[projectID] = instance
			manager.mu.Unlock()
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	stopVscodeInstance(instance)
	return domain.NewAPIError(503, "VSCODE_SERVER_TIMEOUT", "VS Code Server 启动超时")
}

type limitedVscodeBuffer struct {
	mu   sync.Mutex
	text string
}

func (buffer *limitedVscodeBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	buffer.text = (buffer.text + string(value))
	if len(buffer.text) > 8000 {
		buffer.text = buffer.text[len(buffer.text)-8000:]
	}
	buffer.mu.Unlock()
	return len(value), nil
}

func (api *API) configureVscodeUserData(ctx context.Context, userDataDir, projectID string) error {
	if err := os.MkdirAll(filepath.Join(userDataDir, "User"), 0o700); err != nil {
		return err
	}
	settingsPath := filepath.Join(userDataDir, "User", "settings.json")
	settings := map[string]any{}
	if raw, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	for key, value := range terminalSafeSettings() {
		settings[key] = value
	}
	if err := api.applyTexSettings(ctx, projectID, settings); err != nil {
		return err
	}
	encoded, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	keys, _ := json.MarshalIndent([]map[string]string{{"key": "ctrl+`", "command": "-workbench.action.terminal.toggleTerminal"}, {"key": "ctrl+shift+`", "command": "-workbench.action.terminal.new"}, {"key": "ctrl+b", "command": "-workbench.action.toggleSidebarVisibility"}}, "", "  ")
	return os.WriteFile(filepath.Join(userDataDir, "User", "keybindings.json"), append(keys, '\n'), 0o600)
}

func terminalSafeSettings() map[string]any {
	return map[string]any{
		"extensions.autoUpdate":                        false,
		"extensions.autoCheckUpdates":                  false,
		"workbench.activityBar.location":               "default",
		"git.openRepositoryInParentFolders":            "never",
		"terminal.integrated.hideOnStartup":            "always",
		"terminal.integrated.tabs.enabled":             false,
		"terminal.integrated.profiles.linux":           map[string]any{"review-hub-disabled": map[string]string{"path": "false"}},
		"terminal.integrated.defaultProfile.linux":     "review-hub-disabled",
		"terminal.integrated.enablePersistentSessions": false,
		"latex-workshop.view.autoFocus.enabled":        true,
	}
}

func (api *API) applyTexSettings(ctx context.Context, projectID string, settings map[string]any) error {
	project, err := api.store.ProjectTexSettings(ctx, projectID)
	if err != nil {
		return err
	}
	if project.ProfileID != "" {
		if profile, err := api.store.TexLiveProfile(ctx, project.ProfileID); err != nil {
			return err
		} else if profile != nil {
			project.Engine, project.BuildTool, project.OutputDirectory = profile.Engine, profile.BuildTool, profile.OutputDirectory
			project.ShellEscape, project.TexLiveBinPath = profile.ShellEscape, profile.TexLiveBinPath
		}
	}
	settings["latex-workshop.latex.outDir"] = project.OutputDirectory
	settings["latex-workshop.latex.autoBuild.run"] = project.AutoBuild
	settings["latex-workshop.latex.recipe.default"] = project.Engine
	command := project.Engine
	if project.TexLiveBinPath != "" {
		command = filepath.Join(project.TexLiveBinPath, project.Engine)
	}
	args := []string{"%DOC%"}
	if project.ShellEscape {
		args = append([]string{"-shell-escape"}, args...)
	}
	settings["latex-workshop.latex.tools"] = []map[string]any{{"name": project.Engine, "command": command, "args": args}}
	settings["latex-workshop.latex.recipes"] = []map[string]any{{"name": project.Engine, "tools": []string{project.Engine}}}
	return nil
}

func cleanGitValue(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
}

func (api *API) vscodeGitEnvironment(ctx context.Context, projectID, instanceRoot string) ([]string, string, error) {
	project, err := api.store.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	ownerID, _ := project["created_by_user_id"].(string)
	projectSettings, err := api.store.ProjectGitSettings(ctx, projectID)
	if err != nil {
		return nil, "", err
	}
	defaults, err := api.store.UserVscodeDefaults(ctx, ownerID)
	if err != nil {
		return nil, "", err
	}
	name, email := cleanGitValue(projectSettings.UserName), cleanGitValue(projectSettings.UserEmail)
	if name == "" {
		name = cleanGitValue(defaults.UserName)
	}
	if email == "" {
		email = cleanGitValue(defaults.UserEmail)
	}
	token, err := decryptedPreference(api.encryptionKey, projectSettings.AccessTokenCiphertext, defaults.AccessTokenCiphertext)
	if err != nil {
		return nil, "", err
	}
	sshKey, err := decryptedPreference(api.encryptionKey, projectSettings.SSHKeyCiphertext, defaults.SSHKeyCiphertext)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(instanceRoot, 0o700); err != nil {
		return nil, "", err
	}
	gitConfigPath := filepath.Join(instanceRoot, "gitconfig")
	if err := os.WriteFile(gitConfigPath, []byte{}, 0o600); err != nil {
		return nil, "", err
	}
	if name != "" {
		if err := gitConfigSet(gitConfigPath, "user.name", name); err != nil {
			return nil, "", err
		}
	}
	if email != "" {
		if err := gitConfigSet(gitConfigPath, "user.email", email); err != nil {
			return nil, "", err
		}
	}
	askPassPath := filepath.Join(instanceRoot, "git-askpass.sh")
	askPass := "#!/bin/sh\ncase \"$1\" in\n  *Username*) printf '%s\\n' \"${REVIEW_HUB_GIT_USERNAME:-x-access-token}\" ;;\n  *Password*) printf '%s\\n' \"${REVIEW_HUB_GIT_TOKEN:-}\" ;;\n  *) printf '\\n' ;;\nesac\n"
	if err := os.WriteFile(askPassPath, []byte(askPass), 0o700); err != nil {
		return nil, "", err
	}
	sshKeyPath := ""
	if sshKey != "" {
		sshKeyPath = filepath.Join(instanceRoot, "id_ed25519")
		if err := os.WriteFile(sshKeyPath, []byte(sshKey), 0o600); err != nil {
			return nil, "", err
		}
	}
	environment := []string{"GIT_CONFIG_GLOBAL=" + gitConfigPath, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_ASKPASS=" + askPassPath, "SSH_ASKPASS=" + askPassPath, "GIT_TERMINAL_PROMPT=0", "REVIEW_HUB_GIT_TOKEN=" + token, "REVIEW_HUB_GIT_USERNAME=" + firstNonEmpty(name, "x-access-token")}
	if sshKeyPath != "" {
		environment = append(environment, "GIT_SSH_COMMAND=ssh -i "+shellQuote(sshKeyPath)+" -o IdentitiesOnly=yes")
	}
	return environment, sshKeyPath, nil
}

func decryptedPreference(key, primary, fallback string) (string, error) {
	value := primary
	if value == "" {
		value = fallback
	}
	if value == "" {
		return "", nil
	}
	plain, err := secretbox.Decrypt(key, value)
	if err != nil {
		return "", domain.NewAPIError(500, "SECRET_DECRYPT_FAILED", "无法读取在线编辑器的项目凭据")
	}
	return plain, nil
}

func gitConfigSet(path, key, value string) error {
	command := exec.Command("git", "config", "--file", path, key, value)
	command.Env = gitReadOnlyEnvironment()
	return command.Run()
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func withoutEnvironment(environment []string, keys ...string) []string {
	blocked := map[string]bool{}
	for _, key := range keys {
		blocked[key] = true
	}
	output := make([]string, 0, len(environment))
	for _, item := range environment {
		if key, _, ok := strings.Cut(item, "="); !ok || !blocked[key] {
			output = append(output, item)
		}
	}
	return output
}

func vscodeProbe(port int, cookie string) bool {
	client := &http.Client{Timeout: time.Second}
	request, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/", nil)
	request.Header.Set("Cookie", "code-server-session="+cookie)
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode > 0
}

func (api *API) evictVscodeInstances() {
	manager := api.vscode()
	manager.mu.Lock()
	var evicted []*vscodeInstance
	for id, instance := range manager.instances {
		if !instanceAlive(instance) || time.Since(instance.lastUsedAt) > vscodeIdleTimeout {
			delete(manager.instances, id)
			evicted = append(evicted, instance)
		}
	}
	for len(manager.instances) >= vscodeMaxInstances {
		var oldest *vscodeInstance
		for _, instance := range manager.instances {
			if oldest == nil || instance.lastUsedAt.Before(oldest.lastUsedAt) {
				oldest = instance
			}
		}
		if oldest == nil {
			break
		}
		delete(manager.instances, oldest.projectID)
		evicted = append(evicted, oldest)
	}
	manager.mu.Unlock()
	for _, instance := range evicted {
		stopVscodeInstance(instance)
	}
}

func stopVscodeInstance(instance *vscodeInstance) {
	if instance == nil {
		return
	}
	if instance.command != nil && instance.command.Process != nil && instanceAlive(instance) {
		_ = instance.command.Process.Signal(os.Interrupt)
		go func() {
			select {
			case <-instance.done:
			case <-time.After(3 * time.Second):
				_ = instance.command.Process.Kill()
			}
		}()
	}
	if instance.sshKeyPath != "" {
		_ = os.Remove(instance.sshKeyPath)
	}
}

func (api *API) stopVscode(projectID string) {
	manager := api.vscode()
	manager.mu.Lock()
	instance := manager.instances[projectID]
	delete(manager.instances, projectID)
	manager.mu.Unlock()
	stopVscodeInstance(instance)
}

func (api *API) vscodeProxy(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	projectID := request.PathValue("projectID")
	var root, sourceID string
	if err == nil {
		_, root, err = api.projectEditorRoot(request.Context(), projectID, user)
	}
	if err == nil {
		manager := api.vscode()
		manager.mu.Lock()
		active := manager.instances[projectID]
		if instanceAlive(active) && active.sourceSnapshotID != "" {
			root, sourceID = active.repositoryPath, active.sourceSnapshotID
		}
		manager.mu.Unlock()
		err = api.ensureVscode(request.Context(), projectID, root, sourceID)
	}
	if err != nil {
		writeError(response, err)
		return
	}
	manager := api.vscode()
	manager.mu.Lock()
	instance := manager.instances[projectID]
	if instanceAlive(instance) {
		instance.lastUsedAt = time.Now()
	} else {
		instance = nil
	}
	manager.mu.Unlock()
	if instance == nil {
		writeError(response, domain.NewAPIError(503, "VSCODE_NOT_READY", "VS Code Server 尚未就绪"))
		return
	}
	prefix := "/vscode/projects/" + url.PathEscape(projectID)
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(instance.port))
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(outgoing *http.Request) {
		originalDirector(outgoing)
		if strings.HasPrefix(outgoing.URL.Path, prefix) {
			outgoing.URL.Path = strings.TrimPrefix(outgoing.URL.Path, prefix)
		}
		if outgoing.URL.Path == "" {
			outgoing.URL.Path = "/"
		}
		outgoing.Header.Set("Cookie", "code-server-session="+instance.authCookie)
		outgoing.Header.Set("X-Forwarded-Host", request.Host)
	}
	proxy.ModifyResponse = func(upstream *http.Response) error {
		if location := upstream.Header.Get("Location"); strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") {
			upstream.Header.Set("Location", prefix+location)
		}
		cookies := upstream.Header.Values("Set-Cookie")
		if len(cookies) > 0 {
			upstream.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				upstream.Header.Add("Set-Cookie", rewriteVscodeCookiePath(cookie, prefix+"/"))
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		writeError(writer, domain.NewAPIError(503, "VSCODE_PROXY_FAILED", "VS Code Server 代理失败"))
	}
	proxy.ServeHTTP(response, request)
}

func rewriteVscodeCookiePath(cookie, path string) string {
	parts := strings.Split(cookie, ";")
	for index := 1; index < len(parts); index++ {
		key, _, found := strings.Cut(strings.TrimSpace(parts[index]), "=")
		if found && strings.EqualFold(key, "Path") {
			parts[index] = " Path=" + path
			return strings.Join(parts, ";")
		}
	}
	return strings.Join(parts, ";") + "; Path=" + path
}

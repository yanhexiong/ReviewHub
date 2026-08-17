package importer

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

var projectIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{16,80}$`)

type Service struct {
	DataDirectory  string
	MaxImportBytes int64
	Client         *http.Client
}

type GitHubResult struct{ Workspace, URL, Branch string }

func New(dataDirectory string, maxImportBytes int64) *Service {
	return &Service{DataDirectory: dataDirectory, MaxImportBytes: maxImportBytes, Client: &http.Client{Timeout: 120 * time.Second}}
}

// ListTexFiles returns bounded, repository-relative TeX entry candidates for
// the pending project flow. Hidden directories and dependency trees are not
// useful entry points and are skipped deliberately.
func ListTexFiles(root string) ([]string, error) {
	const maxCandidates = 1000
	results := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if len(results) < maxCandidates && strings.HasSuffix(strings.ToLower(entry.Name()), ".tex") {
			results = append(results, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(results)
	return results, nil
}

func (service *Service) workspaceRoot() string {
	return filepath.Join(service.DataDirectory, "workspaces")
}

func (service *Service) ImportArchive(ctx context.Context, projectID string, reader io.Reader, size int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !projectIDPattern.MatchString(projectID) {
		return "", domain.NewAPIError(400, "INVALID_PROJECT_ID", "项目标识无效")
	}
	if size > service.MaxImportBytes {
		return "", domain.NewAPIError(413, "ARCHIVE_TOO_LARGE", "仓库压缩包超过允许大小")
	}
	if err := os.MkdirAll(service.workspaceRoot(), 0o700); err != nil {
		return "", err
	}
	temporary, err := os.MkdirTemp(service.workspaceRoot(), ".import-"+projectID+"-")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(temporary)
		}
	}()
	archivePath := filepath.Join(temporary, "archive.zip")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(file, io.LimitReader(reader, service.MaxImportBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	stat, err := os.Stat(archivePath)
	if err != nil {
		return "", err
	}
	if stat.Size() > service.MaxImportBytes {
		return "", domain.NewAPIError(413, "ARCHIVE_TOO_LARGE", "仓库压缩包超过允许大小")
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", domain.NewAPIError(400, "INVALID_ARCHIVE", "仓库文件不是有效 ZIP 压缩包")
	}
	defer archive.Close()
	if len(archive.File) == 0 {
		return "", domain.NewAPIError(400, "EMPTY_ARCHIVE", "仓库压缩包为空")
	}
	if len(archive.File) > 50_000 {
		return "", domain.NewAPIError(413, "ARCHIVE_TOO_MANY_FILES", "仓库压缩包文件数量过多")
	}
	names := make([]string, 0, len(archive.File))
	total := int64(0)
	for _, entry := range archive.File {
		name, err := normalizeArchivePath(entry.Name)
		if err != nil {
			return "", err
		}
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		names = append(names, name)
		total += int64(entry.UncompressedSize64)
		if total > service.MaxImportBytes*3 {
			return "", domain.NewAPIError(413, "ARCHIVE_TOO_LARGE", "仓库压缩包解压后超过允许大小")
		}
	}
	if len(names) == 0 {
		return "", domain.NewAPIError(400, "EMPTY_ARCHIVE", "仓库压缩包为空")
	}
	if duplicateStrings(names) {
		return "", domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "仓库压缩包包含重复路径")
	}
	stripRoot := commonArchiveRoot(names)
	for _, entry := range archive.File {
		name, err := normalizeArchivePath(entry.Name)
		if err != nil {
			return "", err
		}
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		name = stripRoot(name)
		if name == "" {
			continue
		}
		target := filepath.Join(temporary, filepath.FromSlash(name))
		if !pathInside(temporary, target) {
			return "", domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "仓库压缩包包含不安全路径")
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", domain.NewAPIError(400, "UNSAFE_REPOSITORY_ENTRY", "仓库压缩包包含不受支持的符号链接")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		source, err := entry.Open()
		if err != nil {
			return "", err
		}
		destination, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			_ = source.Close()
			return "", err
		}
		_, copyErr := io.Copy(destination, io.LimitReader(source, service.MaxImportBytes*3+1))
		_ = source.Close()
		closeErr := destination.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	_ = os.Remove(archivePath)
	destination := filepath.Join(service.workspaceRoot(), projectID)
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	cleanup = false
	return destination, nil
}

func (service *Service) CloneGitHub(ctx context.Context, projectID, rawURL, branch, token string) (GitHubResult, error) {
	canonical, owner, repository, err := parseGitHubURL(rawURL)
	if err != nil {
		return GitHubResult{}, err
	}
	branch, err = service.resolveBranch(ctx, owner, repository, branch, token)
	if err != nil {
		return GitHubResult{}, err
	}
	if !projectIDPattern.MatchString(projectID) {
		return GitHubResult{}, domain.NewAPIError(400, "INVALID_PROJECT_ID", "项目标识无效")
	}
	if err := os.MkdirAll(service.workspaceRoot(), 0o700); err != nil {
		return GitHubResult{}, err
	}
	temporary, err := os.MkdirTemp(service.workspaceRoot(), ".import-"+projectID+"-")
	if err != nil {
		return GitHubResult{}, err
	}
	_ = os.RemoveAll(temporary)
	askpass, err := writeAskpass(service.workspaceRoot())
	if err != nil {
		return GitHubResult{}, err
	}
	defer os.Remove(askpass)
	command := exec.CommandContext(ctx, "git", "clone", "--branch", branch, canonical, temporary)
	command.Env = gitEnvironment(askpass, token)
	command.Dir = service.DataDirectory
	output, err := command.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(temporary)
		if errors.Is(err, exec.ErrNotFound) {
			return GitHubResult{}, domain.NewAPIError(500, "GIT_NOT_FOUND", "服务器未安装 Git，无法保留 GitHub 仓库历史")
		}
		_ = output
		return GitHubResult{}, domain.NewAPIError(502, "GITHUB_CLONE_FAILED", "GitHub 仓库克隆失败，请检查地址、分支和访问权限")
	}
	if err := validateWorkspace(temporary, service.MaxImportBytes); err != nil {
		_ = os.RemoveAll(temporary)
		return GitHubResult{}, err
	}
	destination := filepath.Join(service.workspaceRoot(), projectID)
	if err := os.RemoveAll(destination); err != nil {
		_ = os.RemoveAll(temporary)
		return GitHubResult{}, err
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.RemoveAll(temporary)
		return GitHubResult{}, err
	}
	return GitHubResult{Workspace: destination, URL: canonical, Branch: branch}, nil
}

func (service *Service) resolveBranch(ctx context.Context, owner, repository, branch, token string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repository), nil)
		if err != nil {
			return "", err
		}
		setGitHubHeaders(request, token)
		response, err := service.Client.Do(request)
		if err != nil {
			return "", domain.NewAPIError(502, "GITHUB_REPOSITORY_LOOKUP_FAILED", "无法读取 GitHub 仓库信息，请检查地址或访问权限")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			if response.StatusCode == http.StatusNotFound {
				return "", domain.NewAPIError(404, "GITHUB_REPOSITORY_LOOKUP_FAILED", "无法读取 GitHub 仓库信息，请检查地址或访问权限")
			}
			return "", domain.NewAPIError(502, "GITHUB_REPOSITORY_LOOKUP_FAILED", "无法读取 GitHub 仓库信息，请检查地址或访问权限")
		}
		var metadata struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := jsonDecoder(response.Body, &metadata); err != nil {
			return "", domain.NewAPIError(502, "GITHUB_REPOSITORY_LOOKUP_FAILED", "无法读取 GitHub 仓库信息，请检查地址或访问权限")
		}
		branch = strings.TrimSpace(metadata.DefaultBranch)
	}
	if !validBranch(branch) {
		return "", domain.NewAPIError(400, "INVALID_GITHUB_BRANCH", "GitHub 分支名称无效")
	}
	return branch, nil
}

func parseGitHubURL(raw string) (string, string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || (parsed.Hostname() != "github.com" && parsed.Hostname() != "www.github.com") {
		return "", "", "", domain.NewAPIError(400, "INVALID_GITHUB_URL", "只允许使用 HTTPS GitHub 仓库地址")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/"), "/")
	if len(parts) != 2 || !safeGitComponent(parts[0]) || !safeGitComponent(parts[1]) {
		return "", "", "", domain.NewAPIError(400, "INVALID_GITHUB_URL", "GitHub 地址必须是 owner/repository")
	}
	return "https://github.com/" + parts[0] + "/" + parts[1], parts[0], parts[1], nil
}
func safeGitComponent(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("._-", r) {
			return false
		}
	}
	return true
}
func validBranch(value string) bool {
	if value == "" || len(value) > 200 || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.HasSuffix(value, ".") || strings.HasSuffix(value, ".lock") || strings.Contains(value, "//") || strings.Contains(value, "@{") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("._/-", r) {
			return false
		}
	}
	return true
}
func setGitHubHeaders(request *http.Request, token string) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "review-hub")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if strings.TrimSpace(token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
}
func jsonDecoder(reader io.Reader, value any) error {
	decoder := json.NewDecoder(reader)
	return decoder.Decode(value)
}
func gitEnvironment(askpass, token string) []string {
	environment := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null"}
	if askpass != "" && token != "" {
		environment = append(environment, "GIT_ASKPASS="+askpass, "REVIEW_HUB_GIT_TOKEN="+token)
	}
	return environment
}
func writeAskpass(root string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	path := filepath.Join(root, ".review-hub-askpass-"+hex.EncodeToString(bytes)+".sh")
	content := "#!/bin/sh\ncase \"${1:-}\" in\n  *[Uu]sername*) printf '%s\\n' 'x-access-token' ;;\n  *) printf '%s\\n' \"${REVIEW_HUB_GIT_TOKEN:-}\" ;;\nesac\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return "", err
	}
	return path, nil
}
func validateWorkspace(root string, maxBytes int64) error {
	command := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree")
	if output, err := command.Output(); err != nil || strings.TrimSpace(string(output)) != "true" {
		return domain.NewAPIError(502, "GITHUB_CLONE_INVALID", "GitHub 仓库克隆完成但未识别为 Git 工作区")
	}
	fileCount := 0
	total := int64(0)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return domain.NewAPIError(400, "UNSAFE_REPOSITORY_ENTRY", "GitHub 仓库包含不受支持的符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return domain.NewAPIError(400, "UNSAFE_REPOSITORY_ENTRY", "GitHub 仓库包含不受支持的特殊文件")
		}
		fileCount++
		if fileCount > 50_000 {
			return domain.NewAPIError(413, "ARCHIVE_TOO_MANY_FILES", "GitHub 仓库文件数量过多")
		}
		stat, err := entry.Info()
		if err != nil {
			return err
		}
		total += stat.Size()
		if total > maxBytes {
			return domain.NewAPIError(413, "ARCHIVE_TOO_LARGE", "GitHub 仓库超过允许大小")
		}
		return nil
	})
}
func normalizeArchivePath(value string) (string, error) {
	normalized := strings.ReplaceAll(value, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") || strings.ContainsRune(normalized, 0) {
		return "", domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "仓库压缩包包含不安全路径")
	}
	parts := strings.Split(normalized, "/")
	output := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", domain.NewAPIError(400, "INVALID_ARCHIVE_PATH", "仓库压缩包包含不安全路径")
		}
		output = append(output, part)
	}
	return strings.Join(output, "/"), nil
}
func commonArchiveRoot(names []string) func(string) string {
	first := strings.Split(names[0], "/")[0]
	if first == "" {
		return func(name string) string { return name }
	}
	for _, name := range names {
		if !strings.Contains(name, "/") || strings.Split(name, "/")[0] != first {
			return func(name string) string { return name }
		}
	}
	prefix := first + "/"
	return func(name string) string { return strings.TrimPrefix(name, prefix) }
}
func duplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func sorted(values []string) []string { sort.Strings(values); return values }

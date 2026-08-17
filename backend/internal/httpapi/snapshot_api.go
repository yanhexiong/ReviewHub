package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/pdf"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

const (
	maxSnapshotLabel       = 100
	maxSnapshotNote        = 500
	maxSnapshotPath        = 2000
	maxSnapshotFileID      = 1000
	maxGitBranch           = 200
	maxGitCommitMessage    = 500
	maxVscodeSettingsBytes = 100_000
)

var gitSHA = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

type manualSnapshotGit struct {
	Branch        string
	CommitSHA     string
	CommitMessage string
}

type snapshotGit struct {
	Root      string
	Branch    string
	SHA       string
	ShortSHA  string
	Message   string
	Author    string
	Timestamp string
	Dirty     *int64
	Diff      string
}

func (api *API) createSnapshot(response http.ResponseWriter, request *http.Request) {
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

	settings, err := api.store.ResourceSettings(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	maxBytes := int64(settings.MaxPDFBytes)
	if maxBytes <= 0 {
		maxBytes = 100 * 1024 * 1024
	}

	var (
		pdfBytes     []byte
		originalPath string
		originalName string
		sourceKind   = "project"
		manualGit    *manualSnapshotGit
		label        string
		note         string
	)
	contentType := strings.ToLower(request.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		pdfBytes, originalName, manualGit, label, note, err = parseMultipartSnapshot(request, response, maxBytes)
		sourceKind = "external"
	} else {
		var input struct {
			Label            string `json:"label"`
			Note             string `json:"note"`
			PDFPath          string `json:"pdfPath"`
			PDFFileID        string `json:"pdfFileId"`
			GitAssociation   string `json:"gitAssociation"`
			GitBranch        string `json:"gitBranch"`
			GitCommitSHA     string `json:"gitCommitSha"`
			GitCommitMessage string `json:"gitCommitMessage"`
		}
		if err := decodeJSON(request, &input); err != nil {
			writeError(response, err)
			return
		}
		label, note, err = validateSnapshotMetadata(input.Label, input.Note)
		if err != nil {
			writeError(response, err)
			return
		}
		pdfPath := strings.TrimSpace(input.PDFPath)
		fileID := strings.TrimSpace(input.PDFFileID)
		if pdfPath != "" && fileID != "" {
			writeError(response, domain.NewAPIError(400, "PDF_SOURCE_CONFLICT", "只能选择 PDF 文件或填写手动路径其中一种方式"))
			return
		}
		if len(pdfPath) > maxSnapshotPath || len(fileID) > maxSnapshotFileID {
			writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
			return
		}
		if strings.TrimSpace(input.GitAssociation) != "" && strings.TrimSpace(input.GitAssociation) != "none" {
			manualGit, err = validateManualSnapshotGit(input.GitAssociation, input.GitBranch, input.GitCommitSHA, input.GitCommitMessage)
			if err != nil {
				writeError(response, err)
				return
			}
		}
		originalPath, err = api.resolveSnapshotSource(request, projectID, pdfPath, fileID)
		if err == nil {
			pdfBytes, originalName, err = readStablePDF(originalPath, maxBytes)
		}
	}
	if err != nil {
		writeError(response, err)
		return
	}
	metadata, err := pdf.Analyze(pdfBytes)
	if err != nil {
		writeError(response, domain.NewAPIError(400, "INVALID_PDF", "源文件不是有效 PDF"))
		return
	}
	hash := sha256.Sum256(pdfBytes)
	sha := hex.EncodeToString(hash[:])
	if strings.TrimSpace(originalName) == "" {
		originalName = "snapshot.pdf"
	}
	originalName = safeDownloadName(originalName, "snapshot.pdf")

	dataRoot, err := api.snapshotDataRoot()
	if err != nil {
		writeError(response, err)
		return
	}
	snapshotID := uuid.NewString()
	destination, relativeDestination, err := prepareSnapshotFile(dataRoot, projectID, snapshotID, sha, pdfBytes)
	if err != nil {
		writeError(response, err)
		return
	}
	removeArchive := true
	defer func() {
		if removeArchive {
			_ = os.Remove(destination)
		}
	}()

	git := snapshotGit{}
	if manualGit != nil {
		git.Branch = manualGit.Branch
		git.SHA = manualGit.CommitSHA
		git.ShortSHA = manualGit.CommitSHA
		if len(git.ShortSHA) > 7 {
			git.ShortSHA = git.ShortSHA[:7]
		}
		git.Message = manualGit.CommitMessage
	} else if sourceKind == "project" {
		git = readSnapshotGit(request.Context(), filepath.Dir(originalPath))
	}
	var dirty *int64
	if git.Dirty != nil {
		value := *git.Dirty
		dirty = &value
	}
	originalStoredPath := filepath.ToSlash(originalPath)
	if sourceKind == "external" {
		originalStoredPath = filepath.ToSlash(filepath.Join("external", projectID, snapshotID+"-"+originalName))
	}
	row := repository.NewSnapshot{
		ID:                 snapshotID,
		ProjectID:          projectID,
		OriginalPDFPath:    originalStoredPath,
		ArchivedPDFPath:    relativeDestination,
		OriginalFileName:   originalName,
		FileSizeBytes:      int64(len(pdfBytes)),
		SHA256:             sha,
		PageCount:          int64(metadata.PageCount),
		PageTextHashes:     mustJSON(metadata.PageTextHashes),
		SnapshotLabel:      label,
		SnapshotNote:       note,
		SourceKind:         sourceKind,
		ArchivedByUserID:   user.ID,
		GitRepositoryRoot:  git.Root,
		GitBranch:          git.Branch,
		GitCommitSHA:       git.SHA,
		GitCommitShortSHA:  git.ShortSHA,
		GitCommitMessage:   git.Message,
		GitCommitAuthor:    git.Author,
		GitCommitTimestamp: git.Timestamp,
		GitWorktreeDirty:   dirty,
		GitDiffStatSummary: git.Diff,
	}
	created, err := api.store.CreateSnapshotAtomic(request.Context(), row)
	if err != nil {
		writeError(response, err)
		return
	}
	removeArchive = false
	api.store.Audit(request.Context(), &projectID, &user.ID, "snapshot", snapshotID, "archived", map[string]any{
		"version":           created.VersionNumber,
		"sha256":            sha,
		"sourceKind":        sourceKind,
		"manuallyLinkedGit": manualGit != nil,
	})
	public, err := api.store.SnapshotByID(request.Context(), projectID, snapshotID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"snapshot": repository.PublicSnapshot(public)})
}

func parseMultipartSnapshot(request *http.Request, response http.ResponseWriter, maxBytes int64) ([]byte, string, *manualSnapshotGit, string, string, error) {
	// Keep the parser from accepting a body whose multipart framing is many
	// gigabytes larger than the configured PDF limit.
	request.Body = http.MaxBytesReader(response, request.Body, maxBytes+4*1024*1024)
	if err := request.ParseMultipartForm(32 << 20); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			return nil, "", nil, "", "", domain.NewAPIError(413, "PDF_TOO_LARGE", "PDF 文件超过管理员设置的大小上限")
		}
		return nil, "", nil, "", "", domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	var fileHeader *multipart.FileHeader
	if request.MultipartForm != nil {
		for _, headers := range request.MultipartForm.File {
			if len(headers) > 0 {
				fileHeader = headers[0]
				break
			}
		}
	}
	if fileHeader == nil {
		return nil, "", nil, "", "", domain.NewAPIError(400, "PDF_REQUIRED", "请选择要添加的 PDF 文件")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", nil, "", "", domain.NewAPIError(400, "PDF_NOT_FOUND", "PDF 文件不可读取")
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, "", nil, "", "", domain.NewAPIError(400, "PDF_NOT_FOUND", "PDF 文件不可读取")
	}
	if int64(len(contents)) > maxBytes {
		return nil, "", nil, "", "", domain.NewAPIError(413, "PDF_TOO_LARGE", "PDF 文件超过管理员设置的大小上限")
	}
	label, note, err := validateSnapshotMetadata(firstFormValue(request, "label"), firstFormValue(request, "note"))
	if err != nil {
		return nil, "", nil, "", "", err
	}
	association := strings.TrimSpace(firstFormValue(request, "gitAssociation"))
	branch := firstFormValue(request, "gitBranch")
	commitSHA := firstFormValue(request, "gitCommitSha")
	message := firstFormValue(request, "gitCommitMessage")
	var manual *manualSnapshotGit
	if association != "" && association != "none" {
		manual, err = validateManualSnapshotGit(association, branch, commitSHA, message)
		if err != nil {
			return nil, "", nil, "", "", err
		}
	}
	return contents, fileHeader.Filename, manual, label, note, nil
}

func firstFormValue(request *http.Request, key string) string {
	if request.MultipartForm == nil {
		return ""
	}
	values := request.MultipartForm.Value[key]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func validateSnapshotMetadata(labelValue, noteValue any) (string, string, error) {
	label := strings.TrimSpace(formValue(labelValue))
	note := strings.TrimSpace(formValue(noteValue))
	if len(label) > maxSnapshotLabel || len(note) > maxSnapshotNote {
		return "", "", domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	return label, note, nil
}

func formValue(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case []string:
		if len(item) > 0 {
			return item[0]
		}
	}
	return ""
}

func validateManualSnapshotGit(association, branch, commitSHA, message string) (*manualSnapshotGit, error) {
	if association != "manual" {
		return nil, domain.NewAPIError(400, "GIT_ASSOCIATION_INVALID", "Git 关联方式无效")
	}
	commitSHA = strings.TrimSpace(commitSHA)
	branch = strings.TrimSpace(branch)
	message = strings.TrimSpace(message)
	if !gitSHA.MatchString(commitSHA) {
		return nil, domain.NewAPIError(400, "INVALID_GIT_COMMIT", "手动关联的 Git 提交必须是 7-64 位提交 SHA")
	}
	if len(branch) > maxGitBranch || len(message) > maxGitCommitMessage || strings.ContainsAny(branch+message, "\x00\r\n") {
		return nil, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	return &manualSnapshotGit{Branch: branch, CommitSHA: commitSHA, CommitMessage: message}, nil
}

func (api *API) resolveSnapshotSource(request *http.Request, projectID, pdfPath, fileID string) (string, error) {
	project, err := api.store.ProjectByID(request.Context(), projectID)
	if err != nil {
		return "", err
	}
	var candidate string
	if fileID != "" {
		candidate, err = resolveProjectPDFOption(api, request.Context(), projectID, fileID)
	} else if pdfPath != "" {
		candidate = pdfPath
		if !filepath.IsAbs(candidate) {
			root := api.storedAbsolutePath(project["repository_path"])
			candidate = filepath.Join(root, filepath.FromSlash(candidate))
		}
	} else {
		candidate = api.storedAbsolutePath(project["source_pdf_path"])
	}
	if err != nil {
		return "", err
	}
	if candidate == "" {
		return "", domain.NewAPIError(404, "PDF_NOT_FOUND", "未找到项目配置的 PDF 文件")
	}
	return api.safeExistingPath(request, candidate, true)
}

func readStablePDF(path string, maxBytes int64) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", domain.NewAPIError(400, "PDF_NOT_FOUND", "PDF 文件不可读取")
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return nil, "", domain.NewAPIError(400, "INVALID_PDF", "源路径不是文件")
	}
	if before.Size() > maxBytes {
		return nil, "", domain.NewAPIError(413, "PDF_TOO_LARGE", "PDF 文件超过管理员设置的大小上限")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, "", domain.NewAPIError(400, "PDF_NOT_FOUND", "PDF 文件不可读取")
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, "", domain.NewAPIError(409, "PDF_CHANGING", "PDF 仍在写入，请稍后重试")
	}
	if int64(len(contents)) > maxBytes {
		return nil, "", domain.NewAPIError(413, "PDF_TOO_LARGE", "PDF 文件超过管理员设置的大小上限")
	}
	return contents, filepath.Base(path), nil
}

func (api *API) snapshotDataRoot() (string, error) {
	root := strings.TrimSpace(api.dataDirectory)
	if root == "" {
		root = "data"
	}
	abs, err := filepath.Abs(root)
	if err != nil || filepath.Clean(abs) == string(filepath.Separator) {
		return "", domain.NewAPIError(500, "INTERNAL_ERROR", "数据目录不可用")
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", domain.NewAPIError(500, "INTERNAL_ERROR", "数据目录不可用")
	}
	return filepath.Clean(abs), nil
}

func prepareSnapshotFile(dataRoot, projectID, snapshotID, sha string, data []byte) (string, string, error) {
	base := filepath.Join(dataRoot, "snapshots")
	directory := filepath.Join(base, projectID)
	if !pathInside(base, directory) {
		return "", "", domain.NewAPIError(400, "PATH_NOT_ALLOWED", "项目路径无效")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", domain.NewAPIError(500, "INTERNAL_ERROR", "无法创建快照目录")
	}
	name := snapshotID + "-" + sha[:12] + ".pdf"
	destination := filepath.Join(directory, name)
	temporary, err := os.OpenFile(filepath.Join(directory, "."+snapshotID+".tmp"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", "", err
	}
	removeTemporary = false
	relative, err := filepath.Rel(dataRoot, destination)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		_ = os.Remove(destination)
		return "", "", domain.NewAPIError(500, "INTERNAL_ERROR", "快照路径不可用")
	}
	return destination, filepath.ToSlash(relative), nil
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func readSnapshotGit(ctx context.Context, directory string) snapshotGit {
	root, err := runGit(ctx, directory, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(root) == "" {
		return snapshotGit{}
	}
	root = filepath.Clean(strings.TrimSpace(root))
	branch, _ := runGit(ctx, root, "branch", "--show-current")
	sha, _ := runGit(ctx, root, "rev-parse", "HEAD")
	log, _ := runGit(ctx, root, "log", "-1", "--format=%s%n%an%n%aI")
	status, _ := runGit(ctx, root, "status", "--porcelain")
	diff, _ := runGit(ctx, root, "diff", "--stat")
	lines := strings.Split(strings.TrimSpace(log), "\n")
	metadata := snapshotGit{Root: root, Branch: strings.TrimSpace(branch), SHA: strings.TrimSpace(sha), Diff: strings.TrimSpace(diff)}
	if len(metadata.SHA) > 7 {
		metadata.ShortSHA = metadata.SHA[:7]
	}
	if len(lines) > 0 {
		metadata.Message = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		metadata.Author = strings.TrimSpace(lines[1])
	}
	if len(lines) > 2 {
		metadata.Timestamp = strings.TrimSpace(lines[2])
	}
	if status != "" {
		value := int64(1)
		metadata.Dirty = &value
	} else {
		value := int64(0)
		metadata.Dirty = &value
	}
	return metadata
}

type limitedGitBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *limitedGitBuffer) Write(value []byte) (int, error) {
	if buffer.Len()+len(value) > buffer.limit {
		return 0, errors.New("git output too large")
	}
	return buffer.Buffer.Write(value)
}

func runGit(ctx context.Context, directory string, args ...string) (string, error) {
	commandContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, "git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null")
	var output, errorsOutput limitedGitBuffer
	output.limit = 1 << 20
	errorsOutput.limit = 64 << 10
	command.Stdout = &output
	command.Stderr = &errorsOutput
	if err := command.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}

func (api *API) deleteSnapshot(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID, snapshotID := request.PathValue("projectID"), request.PathValue("snapshotID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	snapshot, err := api.store.DeleteSnapshot(request.Context(), projectID, snapshotID)
	if err != nil {
		writeError(response, err)
		return
	}
	for _, key := range []string{"archived_pdf_path", "source_untracked_archive_path"} {
		value, _ := snapshot[key].(string)
		if value == "" {
			continue
		}
		if path, pathErr := api.managedDataPath(value); pathErr == nil {
			moveSnapshotToTrash(api, path, snapshotID)
		}
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "snapshot", snapshotID, "deleted", map[string]any{"version": snapshot["version_number"]})
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func moveSnapshotToTrash(api *API, source, snapshotID string) {
	root, err := api.snapshotDataRoot()
	if err != nil || !pathInside(root, source) {
		return
	}
	trash := filepath.Join(root, "temp", "snapshot-trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return
	}
	name := snapshotID + filepath.Ext(source)
	if err := os.Rename(source, filepath.Join(trash, name)); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(source)
	}
}

func (api *API) vscodeSettings(response http.ResponseWriter, request *http.Request) {
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
	root, err := api.snapshotDataRoot()
	if err != nil {
		writeError(response, err)
		return
	}
	target := filepath.Join(root, "vscode-workspaces", projectID, "user-data", "User", "settings.json")
	workspaceRoot := filepath.Join(root, "vscode-workspaces")
	if !pathInside(workspaceRoot, target) {
		writeError(response, domain.NewAPIError(400, "PATH_NOT_ALLOWED", "项目路径无效"))
		return
	}
	if request.Method == http.MethodGet {
		contents, err := os.ReadFile(target)
		if os.IsNotExist(err) {
			writeJSON(response, http.StatusOK, map[string]string{"content": "{}\n"})
			return
		}
		if err != nil {
			writeError(response, err)
			return
		}
		if len(contents) > maxVscodeSettingsBytes {
			writeError(response, domain.NewAPIError(413, "SETTINGS_TOO_LARGE", "settings.json 超过大小限制"))
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"content": string(contents)})
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if len([]byte(input.Content)) == 0 || len([]byte(input.Content)) > maxVscodeSettingsBytes {
		writeError(response, domain.NewAPIError(413, "SETTINGS_TOO_LARGE", "settings.json 超过大小限制"))
		return
	}
	var parsed any
	decoder := json.NewDecoder(strings.NewReader(input.Content))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		writeError(response, domain.NewAPIError(400, "INVALID_JSON", "settings.json 不是合法 JSON"))
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(response, domain.NewAPIError(400, "INVALID_JSON", "settings.json 不是合法 JSON"))
		return
	}
	if parsed == nil {
		writeError(response, domain.NewAPIError(400, "INVALID_JSON", "settings.json 顶层必须是 JSON 对象"))
		return
	}
	if _, ok := parsed.(map[string]any); !ok {
		writeError(response, domain.NewAPIError(400, "INVALID_JSON", "settings.json 顶层必须是 JSON 对象"))
		return
	}
	encoded, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		writeError(response, err)
		return
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxVscodeSettingsBytes {
		writeError(response, domain.NewAPIError(413, "SETTINGS_TOO_LARGE", "settings.json 超过大小限制"))
		return
	}
	if err := writePrivateJSONFile(target, encoded); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "vscode_settings", projectID, "updated", map[string]any{"bytes": len(encoded)})
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func writePrivateJSONFile(target string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".settings-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	remove = false
	return os.Chmod(target, 0o600)
}

func parsePageHashes(snapshot map[string]any) []string {
	value, _ := snapshot["page_text_hashes_json"].(string)
	var hashes []string
	if json.Unmarshal([]byte(value), &hashes) != nil {
		hashes = nil
	}
	pageCount := int64Value(snapshot["page_count"])
	if len(hashes) == 0 && pageCount > 0 {
		hashes = make([]string, pageCount)
	}
	return hashes
}

func int64Value(value any) int64 {
	switch item := value.(type) {
	case int64:
		return item
	case int:
		return int64(item)
	case float64:
		return int64(item)
	case json.Number:
		parsed, _ := item.Int64()
		return parsed
	default:
		return 0
	}
}

func (api *API) compareSnapshots(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "view"); err != nil {
		writeError(response, err)
		return
	}
	snapshotAID := strings.TrimSpace(request.URL.Query().Get("snapshotA"))
	snapshotBID := strings.TrimSpace(request.URL.Query().Get("snapshotB"))
	if snapshotAID == "" || snapshotBID == "" {
		writeError(response, domain.NewAPIError(400, "SNAPSHOTS_REQUIRED", "请选择两个要比对的版本"))
		return
	}
	snapshotA, err := api.store.SnapshotByID(request.Context(), projectID, snapshotAID)
	if err != nil {
		writeError(response, err)
		return
	}
	snapshotB, err := api.store.SnapshotByID(request.Context(), projectID, snapshotBID)
	if err != nil {
		writeError(response, err)
		return
	}
	if snapshotA == nil || snapshotB == nil {
		writeError(response, domain.NewAPIError(404, "NOT_FOUND", "快照不存在"))
		return
	}
	commentsA, err := api.store.ListSnapshotComments(request.Context(), projectID, snapshotAID)
	if err != nil {
		writeError(response, err)
		return
	}
	commentsB, err := api.store.ListSnapshotComments(request.Context(), projectID, snapshotBID)
	if err != nil {
		writeError(response, err)
		return
	}
	commentDiff := compareCommentRows(commentsA, commentsB)
	pages := comparePageRows(snapshotA, snapshotB)
	changed := 0
	for _, page := range pages {
		if page["status"] != "unchanged" {
			changed++
		}
	}
	publicA := repository.PublicSnapshot(snapshotA)
	publicB := repository.PublicSnapshot(snapshotB)
	writeJSON(response, http.StatusOK, map[string]any{
		"snapshotA":      publicA,
		"snapshotB":      publicB,
		"pages":          pages,
		"changedPages":   changed,
		"comments":       commentDiff,
		"motherComments": mapCommentsForCompare(commentsA),
		// The Go API stores reliable page hashes but does not pretend that a
		// byte-level PDF scan is equivalent to PDF.js text geometry. The
		// browser can still render both authorized PDFs while pages/comments
		// remain deterministic and testable here.
		"textDiff": map[string]any{
			"added":             []any{},
			"removed":           []any{},
			"compositeAdded":    []any{},
			"addedTokenCount":   0,
			"removedTokenCount": 0,
			"truncated":         false,
		},
		"notice": "Go API 当前按归档时保存的页级哈希提供可靠内容差异；PDF 文字坐标差异仍由浏览器 PDF.js 视图核对。",
	})
}

func comparePageRows(a, b map[string]any) []map[string]any {
	oldHashes, newHashes := parsePageHashes(a), parsePageHashes(b)
	count := len(oldHashes)
	if len(newHashes) > count {
		count = len(newHashes)
	}
	if count == 0 {
		count = int(int64Value(a["page_count"]))
		if int64Value(b["page_count"]) > int64(count) {
			count = int(int64Value(b["page_count"]))
		}
	}
	pages := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		status := "modified"
		if index >= len(oldHashes) {
			status = "added"
		} else if index >= len(newHashes) {
			status = "removed"
		} else if oldHashes[index] == newHashes[index] {
			status = "unchanged"
		}
		pages = append(pages, map[string]any{"page": index + 1, "status": status})
	}
	return pages
}

func compareCommentRows(before, after []map[string]any) map[string]any {
	bByID := make(map[string]map[string]any, len(after))
	bByAnchor := make(map[string]map[string]any, len(after))
	for _, comment := range after {
		bByID[stringMapValue(comment, "id")] = comment
		bByAnchor[commentAnchorKey(comment)] = comment
	}
	added := make([]map[string]any, 0)
	removed := make([]map[string]any, 0)
	modified := make([]map[string]any, 0)
	for _, comment := range before {
		other := bByID[stringMapValue(comment, "id")]
		if other == nil {
			other = bByAnchor[commentAnchorKey(comment)]
		}
		if other != nil {
			if commentComparableKey(comment) != commentComparableKey(other) {
				modified = append(modified, map[string]any{"before": mapCommentForCompare(comment), "after": mapCommentForCompare(other)})
			}
			delete(bByID, stringMapValue(other, "id"))
			continue
		}
		removed = append(removed, mapCommentForCompare(comment))
	}
	for _, comment := range after {
		if _, ok := bByID[stringMapValue(comment, "id")]; ok {
			added = append(added, mapCommentForCompare(comment))
		}
	}
	return map[string]any{
		"added": added, "removed": removed, "modified": modified,
		"addedCount": len(added), "removedCount": len(removed), "modifiedCount": len(modified),
	}
}

func mapCommentsForCompare(comments []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(comments))
	for _, comment := range comments {
		result = append(result, mapCommentForCompare(comment))
	}
	return result
}

func mapCommentForCompare(comment map[string]any) map[string]any {
	result := make(map[string]any, len(comment))
	for key, value := range comment {
		result[key] = value
	}
	if strings.TrimSpace(stringMapValue(result, "author_display_name")) == "" {
		result["author_display_name"] = "未知审阅人"
	}
	return result
}

func stringMapValue(value map[string]any, key string) string {
	item, _ := value[key].(string)
	return item
}

func commentAnchorKey(comment map[string]any) string {
	parts := []string{"page_number", "anchor_type", "normalized_x", "normalized_y", "normalized_width", "normalized_height", "selected_text"}
	values := make([]string, 0, len(parts))
	for _, key := range parts {
		values = append(values, fmt.Sprint(comment[key]))
	}
	return strings.Join(values, "|")
}

func commentComparableKey(comment map[string]any) string {
	parts := []string{"content", "status", "priority", "category", "resolved_at", "anchor_type", "normalized_x", "normalized_y", "normalized_width", "normalized_height", "selected_text"}
	values := make([]string, 0, len(parts))
	for _, key := range parts {
		values = append(values, fmt.Sprint(comment[key]))
	}
	return strings.Join(values, "|")
}

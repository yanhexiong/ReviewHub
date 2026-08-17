package editor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

const MaxFileBytes int64 = 2 * 1024 * 1024

var editableExtensions = map[string]bool{
	".bbx": true, ".bib": true, ".bst": true, ".cfg": true, ".cbx": true,
	".cls": true, ".def": true, ".json": true, ".latexmkrc": true,
	".md": true, ".sty": true, ".tex": true, ".toml": true, ".txt": true,
	".xml": true, ".yaml": true, ".yml": true,
}

var editableNames = map[string]bool{".latexmkrc": true, "Makefile": true}
var ignoredDirectories = map[string]bool{".git": true, ".next": true, "build": true, "dist": true, "node_modules": true, "out": true}

type File struct {
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	UpdatedAt float64 `json:"updatedAt"`
	TooLarge  bool    `json:"tooLarge"`
}

type Content struct {
	Path      string  `json:"path"`
	Content   string  `json:"content"`
	Size      int     `json:"size"`
	UpdatedAt float64 `json:"updatedAt"`
	SHA256    string  `json:"sha256"`
}

type GitStatus struct {
	Path        string `json:"path"`
	Code        string `json:"code"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func NormalizeRelativePath(value string) (string, error) {
	if value == "" || strings.ContainsRune(value, 0) || strings.Contains(value, "\\") {
		return "", domain.NewAPIError(400, "INVALID_EDITOR_PATH", "文件路径格式无效")
	}
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || strings.HasPrefix(normalized, "/") {
		return "", domain.NewAPIError(400, "INVALID_EDITOR_PATH", "文件路径必须位于项目仓库内")
	}
	return normalized, nil
}

func IsEditablePath(relative string) bool {
	base := filepath.Base(relative)
	if editableNames[base] {
		return true
	}
	return editableExtensions[strings.ToLower(filepath.Ext(base))]
}

func ListFiles(root string) ([]File, error) {
	files := make([]File, 0, 128)
	var visit func(string, string, int) error
	visit = func(directory, relativeDirectory string, depth int) error {
		if depth > 20 || len(files) >= 2000 {
			return nil
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
		for _, entry := range entries {
			if len(files) >= 2000 || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			relative := entry.Name()
			if relativeDirectory != "" {
				relative = relativeDirectory + "/" + entry.Name()
			}
			if entry.IsDir() {
				if ignoredDirectories[entry.Name()] {
					continue
				}
				if err := visit(filepath.Join(directory, entry.Name()), relative, depth+1); err != nil {
					return err
				}
				continue
			}
			if !entry.Type().IsRegular() || !IsEditablePath(relative) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files = append(files, File{Path: relative, Size: info.Size(), UpdatedAt: float64(info.ModTime().UnixMilli()), TooLarge: info.Size() > MaxFileBytes})
		}
		return nil
	}
	if err := visit(root, "", 0); err != nil {
		return nil, err
	}
	return files, nil
}

func ReadFile(root, relative string) (Content, error) {
	target, normalized, info, err := safeExistingFile(root, relative)
	if err != nil {
		return Content{}, err
	}
	if info.Size() > MaxFileBytes {
		return Content{}, domain.NewAPIError(413, "EDITOR_FILE_TOO_LARGE", "文件超过在线编辑器大小上限（2 MB）")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return Content{}, err
	}
	if bytesContainsZero(data) {
		return Content{}, domain.NewAPIError(400, "EDITOR_BINARY_FILE", "二进制文件不能在在线编辑器中打开")
	}
	return Content{Path: normalized, Content: string(data), Size: len(data), UpdatedAt: float64(info.ModTime().UnixMilli()), SHA256: hash(data)}, nil
}

func WriteFile(root, relative, content string) (Content, error) {
	normalized, err := NormalizeRelativePath(relative)
	if err != nil {
		return Content{}, err
	}
	if !IsEditablePath(normalized) {
		return Content{}, domain.NewAPIError(400, "EDITOR_FILE_TYPE", "在线编辑器只允许文本源文件")
	}
	if len(content) > int(MaxFileBytes) || bytesContainsZero([]byte(content)) {
		if len(content) > int(MaxFileBytes) {
			return Content{}, domain.NewAPIError(413, "EDITOR_FILE_TOO_LARGE", "文件超过在线编辑器大小上限（2 MB）")
		}
		return Content{}, domain.NewAPIError(400, "EDITOR_BINARY_FILE", "二进制内容不能保存为源文件")
	}
	target := filepath.Join(root, filepath.FromSlash(normalized))
	if !pathInside(root, target) {
		return Content{}, domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在项目仓库内")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil || !pathInside(root, parent) {
		return Content{}, domain.NewAPIError(404, "EDITOR_DIRECTORY_NOT_FOUND", "文件所在目录不存在")
	}
	var mode os.FileMode = 0o644
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Content{}, domain.NewAPIError(400, "EDITOR_FILE_INVALID", "只能保存普通文本文件")
		}
		mode = info.Mode()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Content{}, statErr
	}
	temporary, err := os.CreateTemp(parent, ".review-hub-editor-*")
	if err != nil {
		return Content{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return Content{}, err
	}
	if _, err := temporary.WriteString(content); err != nil {
		_ = temporary.Close()
		return Content{}, err
	}
	if err := temporary.Close(); err != nil {
		return Content{}, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Content{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return Content{}, err
	}
	return Content{Path: normalized, Content: content, Size: len(content), UpdatedAt: float64(info.ModTime().UnixMilli()), SHA256: hash([]byte(content))}, nil
}

func GitStatuses(ctx context.Context, root string) []GitStatus {
	command := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "--untracked-files=all", "-z")
	command.Env = append([]string(nil), os.Environ()...)
	command.Env = append(command.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null")
	output, err := command.Output()
	if err != nil {
		return []GitStatus{}
	}
	return parseGitStatuses(string(output))
}

func parseGitStatuses(output string) []GitStatus {
	records := strings.Split(output, "\x00")
	statuses := make([]GitStatus, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		code, path := record[:2], record[3:]
		if path == "" {
			continue
		}
		kind, label, description := classify(code)
		paths := []string{path}
		if (code[0] == 'R' || code[0] == 'C') && index+1 < len(records) && records[index+1] != "" {
			index++
			paths = append(paths, records[index])
		}
		for _, item := range paths {
			statuses = append(statuses, GitStatus{Path: item, Code: code, Kind: kind, Label: label, Description: description})
		}
	}
	return statuses
}

func classify(code string) (string, string, string) {
	switch {
	case code == "??":
		return "untracked", "U", "未跟踪文件"
	case strings.Contains(code, "U"):
		return "conflict", "!", "存在合并冲突"
	case strings.Contains(code, "D"):
		return "deleted", "D", "已删除"
	case strings.Contains(code, "R"):
		return "renamed", "R", "已重命名"
	case strings.Contains(code, "C"):
		return "copied", "C", "已复制"
	case strings.Contains(code, "A"):
		return "added", "A", "已新增"
	case strings.Contains(code, "M"):
		return "modified", "M", "已修改"
	default:
		return "changed", strings.TrimSpace(code), "Git 状态已变化"
	}
}

func safeExistingFile(root, relative string) (string, string, os.FileInfo, error) {
	normalized, err := NormalizeRelativePath(relative)
	if err != nil {
		return "", "", nil, err
	}
	if !IsEditablePath(normalized) {
		return "", "", nil, domain.NewAPIError(400, "EDITOR_FILE_TYPE", "在线编辑器只允许文本源文件")
	}
	target := filepath.Join(root, filepath.FromSlash(normalized))
	if !pathInside(root, target) {
		return "", "", nil, domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在项目仓库内")
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil, domain.NewAPIError(404, "EDITOR_FILE_NOT_FOUND", "源文件不存在")
	}
	if err != nil {
		return "", "", nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, domain.NewAPIError(400, "EDITOR_FILE_INVALID", "只能编辑普通文本文件")
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil || !pathInside(root, canonical) {
		return "", "", nil, domain.NewAPIError(403, "PATH_NOT_ALLOWED", "文件路径不在项目仓库内")
	}
	return target, normalized, info, nil
}

func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func hash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func bytesContainsZero(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

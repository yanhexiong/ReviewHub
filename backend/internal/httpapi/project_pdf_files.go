package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

const (
	maxProjectPDFFiles = 200
	maxProjectPDFDepth = 12
)

var ignoredProjectPDFDirectories = map[string]struct{}{
	".git": {}, ".next": {}, "node_modules": {}, "dist": {}, "out": {},
}

type projectPDFFile struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	FileName     string  `json:"fileName"`
	RelativePath *string `json:"relativePath"`
	Configured   bool    `json:"configured"`
}

func (api *API) listProjectPDFFiles(ctx context.Context, projectID string) ([]projectPDFFile, error) {
	project, err := api.store.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	repositoryPath := api.storedAbsolutePath(project["repository_path"])
	configuredPath := api.storedAbsolutePath(project["source_pdf_path"])
	configuredPath = canonicalPath(configuredPath)

	root := canonicalPath(repositoryPath)
	if root == "" {
		return []projectPDFFile{}, nil
	}
	stat, err := os.Stat(root)
	if err != nil || !stat.IsDir() {
		return []projectPDFFile{}, nil
	}

	files := make([]projectPDFFile, 0, 16)
	var visit func(string, string, int)
	visit = func(directory, relativeDirectory string, depth int) {
		if depth > maxProjectPDFDepth || len(files) >= maxProjectPDFFiles {
			return
		}
		entries, readErr := os.ReadDir(directory)
		if readErr != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if len(files) >= maxProjectPDFFiles || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			name := entry.Name()
			relative := name
			if relativeDirectory != "" {
				relative = filepath.ToSlash(filepath.Join(relativeDirectory, name))
			}
			absolute := filepath.Join(directory, name)
			if entry.IsDir() {
				if _, ignored := ignoredProjectPDFDirectories[name]; ignored {
					continue
				}
				visit(absolute, relative, depth+1)
				continue
			}
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(name), ".pdf") {
				continue
			}
			canonical := canonicalPath(absolute)
			if canonical == "" || !pathInside(root, canonical) {
				continue
			}
			relativeCopy := relative
			files = append(files, projectPDFFile{
				ID:           "repo:" + relative,
				Label:        relative,
				FileName:     name,
				RelativePath: &relativeCopy,
				Configured:   configuredPath != "" && canonical == configuredPath,
			})
		}
	}
	visit(root, "", 0)

	if configuredPath != "" {
		configuredFound := false
		for _, file := range files {
			if file.Configured {
				configuredFound = true
				break
			}
		}
		if !configuredFound {
			// Keep the configured file visible when it is outside the repository;
			// the archive endpoint will still resolve it through the existing
			// server-side path checks.
			name := filepath.Base(configuredPath)
			files = append(files, projectPDFFile{ID: "configured", Label: "当前配置 · " + name, FileName: name, RelativePath: nil, Configured: true})
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Configured != files[j].Configured {
			return files[i].Configured
		}
		return files[i].Label < files[j].Label
	})
	return files, nil
}

func canonicalPath(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return ""
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return ""
	}
	return filepath.Clean(canonical)
}

func resolveProjectPDFOption(api *API, ctx context.Context, projectID, fileID string) (string, error) {
	project, err := api.store.ProjectByID(ctx, projectID)
	if err != nil {
		return "", err
	}
	if fileID == "configured" {
		candidate := api.storedAbsolutePath(project["source_pdf_path"])
		if candidate == "" {
			return "", domain.NewAPIError(404, "PDF_NOT_FOUND", "未找到已配置的 PDF 文件")
		}
		return candidate, nil
	}
	if !strings.HasPrefix(fileID, "repo:") {
		return "", domain.NewAPIError(400, "PDF_OPTION_INVALID", "PDF 文件选项无效，请重新选择")
	}
	relative := strings.TrimPrefix(fileID, "repo:")
	normalized := filepath.Clean(filepath.FromSlash(relative))
	if normalized == "." || normalized == ".." || filepath.IsAbs(normalized) || strings.HasPrefix(normalized, ".."+string(filepath.Separator)) || !strings.EqualFold(filepath.Ext(normalized), ".pdf") {
		return "", domain.NewAPIError(400, "PDF_OPTION_INVALID", "PDF 文件选项无效，请重新选择")
	}
	root := canonicalPath(api.storedAbsolutePath(project["repository_path"]))
	if root == "" {
		return "", domain.NewAPIError(404, "PDF_NOT_FOUND", "项目仓库不可读取")
	}
	candidate := filepath.Join(root, normalized)
	resolved := canonicalPath(candidate)
	if resolved == "" || !pathInside(root, resolved) {
		return "", domain.NewAPIError(403, "PATH_NOT_ALLOWED", "PDF 文件不在项目仓库内")
	}
	return resolved, nil
}

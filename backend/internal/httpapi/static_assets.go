package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (api *API) editorAsset(response http.ResponseWriter, request *http.Request) {
	name := request.PathValue("path")
	if !safeAssetPath(name) {
		http.NotFound(response, request)
		return
	}
	root := api.editorAssetsDirectory()
	filePath := filepath.Join(root, filepath.FromSlash(name))
	if !pathInside(root, filePath) {
		http.NotFound(response, request)
		return
	}
	contentType := "application/octet-stream"
	switch {
	case strings.HasSuffix(filePath, ".js"):
		contentType = "application/javascript; charset=utf-8"
	case strings.HasSuffix(filePath, ".css"):
		contentType = "text/css; charset=utf-8"
	case strings.HasSuffix(filePath, ".json"):
		contentType = "application/json; charset=utf-8"
	case strings.HasSuffix(filePath, ".svg"):
		contentType = "image/svg+xml"
	case strings.HasSuffix(filePath, ".woff"):
		contentType = "font/woff"
	case strings.HasSuffix(filePath, ".woff2"):
		contentType = "font/woff2"
	}
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	response.Header().Set("Content-Type", contentType)
	http.ServeFile(response, request, filePath)
}

func (api *API) pdfWorker(response http.ResponseWriter, request *http.Request) {
	filePath := api.pdfWorkerPath()
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(response, request, filePath)
}

func (api *API) editorAssetsDirectory() string {
	if configured := strings.TrimSpace(os.Getenv("REVIEW_HUB_EDITOR_ASSETS_DIR")); configured != "" {
		return filepath.Clean(configured)
	}
	if api.staticDirectory != "" {
		return filepath.Join(filepath.Dir(api.staticDirectory), "static", "editor-assets")
	}
	workingDirectory, _ := os.Getwd()
	return filepath.Join(workingDirectory, "static", "editor-assets")
}

func (api *API) pdfWorkerPath() string {
	if configured := strings.TrimSpace(os.Getenv("REVIEW_HUB_PDF_WORKER_PATH")); configured != "" {
		return filepath.Clean(configured)
	}
	if api.staticDirectory != "" {
		return filepath.Join(filepath.Dir(api.staticDirectory), "static", "pdf-worker.js")
	}
	workingDirectory, _ := os.Getwd()
	return filepath.Join(workingDirectory, "static", "pdf-worker.js")
}

func safeAssetPath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

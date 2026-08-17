package httpapi

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const staticProjectPlaceholder = "__review_hub_project__"
const staticSharePlaceholder = "__review_hub_share__"

func (api *API) serveStatic(response http.ResponseWriter, request *http.Request) {
	filePath, ok := api.staticFilePath(request.URL.Path)
	if !ok {
		http.NotFound(response, request)
		return
	}
	if contentType := mime.TypeByExtension(filepath.Ext(filePath)); contentType != "" {
		if strings.HasPrefix(contentType, "text/") || strings.HasSuffix(contentType, "+javascript") {
			contentType += "; charset=utf-8"
		}
		response.Header().Set("Content-Type", contentType)
	}
	if strings.HasPrefix(request.URL.Path, "/_next/") {
		response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		response.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(response, request, filePath)
}

func (api *API) staticFilePath(requestPath string) (string, bool) {
	if api.staticDirectory == "" || !safeStaticPath(requestPath) {
		return "", false
	}
	pathValue := strings.Trim(requestPath, "/")
	if pathValue == "" {
		pathValue = "index.html"
	}
	root := api.staticDirectory
	directPath := filepath.Join(root, filepath.FromSlash(pathValue))
	if pathInside(root, directPath) {
		if info, err := os.Stat(directPath); err == nil && info.Mode().IsRegular() {
			return directPath, true
		}
		if info, err := os.Stat(directPath); err == nil && info.IsDir() {
			indexPath := filepath.Join(directPath, "index.html")
			if pathInside(root, indexPath) {
				if info, err := os.Stat(indexPath); err == nil && info.Mode().IsRegular() {
					return indexPath, true
				}
			}
		}
	}

	segments := strings.Split(pathValue, "/")
	var placeholder []string
	switch {
	case len(segments) >= 2 && segments[0] == "projects" && segments[1] != "":
		if len(segments) == 2 {
			placeholder = []string{"projects", staticProjectPlaceholder, "index.html"}
		} else if len(segments) == 3 && (segments[2] == "compare" || segments[2] == "editor" || segments[2] == "review") {
			placeholder = []string{"projects", staticProjectPlaceholder, segments[2], "index.html"}
		}
	case len(segments) == 2 && segments[0] == "share" && segments[1] != "":
		placeholder = []string{"share", staticSharePlaceholder, "index.html"}
	}
	if len(placeholder) == 0 {
		return "", false
	}
	filePath := filepath.Join(append([]string{root}, placeholder...)...)
	if !pathInside(root, filePath) {
		return "", false
	}
	info, err := os.Stat(filePath)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return filePath, true
}

func safeStaticPath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

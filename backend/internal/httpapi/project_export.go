package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/color"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

var pdfCPUConfigOnce sync.Once

func (api *API) projectExport(response http.ResponseWriter, request *http.Request) {
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
	query := request.URL.Query()
	format := strings.TrimSpace(query.Get("format"))
	snapshotID := strings.TrimSpace(query.Get("snapshotId"))
	if format == "annotated-pdf" {
		if snapshotID == "" {
			writeError(response, domain.NewAPIError(400, "SNAPSHOT_REQUIRED", "请选择要导出的版本"))
			return
		}
		body, version, commentCount, err := api.exportAnnotatedPDF(request.Context(), projectID, snapshotID)
		if err != nil {
			writeError(response, err)
			return
		}
		api.store.Audit(request.Context(), &projectID, &user.ID, "export", snapshotID, "annotated_pdf_created", map[string]any{"version": version, "commentCount": commentCount})
		response.Header().Set("Content-Type", "application/pdf")
		response.Header().Set("Content-Disposition", `attachment; filename="reviewed-snapshot-`+strconv.FormatInt(version, 10)+`.pdf"`)
		response.Header().Set("Cache-Control", "no-store")
		_, _ = response.Write(body)
		return
	}
	if format != "markdown" && format != "json" && format != "csv" && format != "source-pdf" && format != "bundle" {
		writeError(response, domain.NewAPIError(400, "INVALID_EXPORT_FORMAT", "不支持的导出格式"))
		return
	}
	if (format == "source-pdf" || format == "bundle") && snapshotID == "" {
		writeError(response, domain.NewAPIError(400, "SNAPSHOT_REQUIRED", "请选择要导出的版本"))
		return
	}

	if format == "source-pdf" || format == "bundle" {
		snapshot, err := api.store.SnapshotFile(request.Context(), projectID, snapshotID)
		if err != nil {
			writeError(response, err)
			return
		}
		archived, _ := snapshot["archived_pdf_path"].(string)
		filePath, err := api.managedDataPath(archived)
		if err != nil {
			writeError(response, err)
			return
		}
		file, err := os.Open(filePath)
		if os.IsNotExist(err) {
			writeError(response, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在"))
			return
		}
		if err != nil {
			writeError(response, err)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			writeError(response, err)
			return
		}
		if !stat.Mode().IsRegular() {
			writeError(response, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在"))
			return
		}
		version, _ := snapshot["version_number"].(int64)
		if version == 0 {
			if value, ok := snapshot["version_number"].(int); ok {
				version = int64(value)
			}
		}
		name, _ := snapshot["original_file_name"].(string)
		name = safeDownloadName(name, "snapshot.pdf")
		if format == "source-pdf" {
			api.store.Audit(request.Context(), &projectID, &user.ID, "export", snapshotID, "source_pdf_created", map[string]any{"version": version})
			response.Header().Set("Content-Type", "application/pdf")
			response.Header().Set("Content-Disposition", `attachment; filename="source-snapshot-`+strconv.FormatInt(version, 10)+`.pdf"`)
			response.Header().Set("Cache-Control", "no-store")
			http.ServeContent(response, request, name, stat.ModTime(), file)
			return
		}
		payload, err := api.store.ExportPayload(request.Context(), projectID, snapshotID)
		if err != nil {
			writeError(response, err)
			return
		}
		records, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			writeError(response, err)
			return
		}
		var archive bytes.Buffer
		writer := zip.NewWriter(&archive)
		if err := addZipBytes(writer, "review-records.json", records); err != nil {
			_ = writer.Close()
			writeError(response, err)
			return
		}
		if err := addZipReader(writer, "source-snapshot-"+strconv.FormatInt(version, 10)+".pdf", file, stat); err != nil {
			_ = writer.Close()
			writeError(response, err)
			return
		}
		if err := writer.Close(); err != nil {
			writeError(response, err)
			return
		}
		api.store.Audit(request.Context(), &projectID, &user.ID, "export", snapshotID, "bundle_created", map[string]any{"version": version})
		response.Header().Set("Content-Type", "application/zip")
		response.Header().Set("Content-Disposition", `attachment; filename="review-package-`+strconv.FormatInt(version, 10)+`.zip"`)
		response.Header().Set("Cache-Control", "no-store")
		_, _ = response.Write(archive.Bytes())
		return
	}

	payload, err := api.store.ExportPayload(request.Context(), projectID, snapshotID)
	if err != nil {
		writeError(response, err)
		return
	}
	var body []byte
	contentType, extension := "", ""
	switch format {
	case "json":
		body, err = json.MarshalIndent(payload, "", "  ")
		contentType, extension = "application/json", "json"
	case "csv":
		body, err = exportCSV(payload.Comments)
		contentType, extension = "text/csv", "csv"
	case "markdown":
		body = []byte(exportMarkdown(payload))
		contentType, extension = "text/markdown", "md"
	}
	if err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "export", projectID, "records_created", map[string]any{"format": format, "snapshotId": snapshotID})
	response.Header().Set("Content-Type", contentType+"; charset=utf-8")
	response.Header().Set("Content-Disposition", `attachment; filename="review-export.`+extension+`"`)
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write(body)
}

func (api *API) exportAnnotatedPDF(ctx context.Context, projectID, snapshotID string) ([]byte, int64, int, error) {
	snapshot, err := api.store.SnapshotFile(ctx, projectID, snapshotID)
	if err != nil {
		return nil, 0, 0, err
	}
	filePath, err := api.managedDataPath(stringMapValue(snapshot, "archived_pdf_path"))
	if err != nil {
		return nil, 0, 0, err
	}
	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil, 0, 0, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在")
	}
	if err != nil {
		return nil, 0, 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, 0, err
	}
	if !stat.Mode().IsRegular() {
		return nil, 0, 0, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在")
	}
	source, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, 0, err
	}
	configuration := pdfCPUConfiguration()
	dimensions, err := pdfapi.PageDims(bytes.NewReader(source), configuration)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read archived PDF dimensions: %w", err)
	}
	comments, err := api.store.ListSnapshotComments(ctx, projectID, snapshotID)
	if err != nil {
		return nil, 0, 0, err
	}
	annotations := reviewAnnotations(comments, dimensions)
	var annotated bytes.Buffer
	if err := pdfapi.AddAnnotationsMap(bytes.NewReader(source), &annotated, annotations, pdfCPUConfiguration()); err != nil {
		return nil, 0, 0, fmt.Errorf("annotate archived PDF: %w", err)
	}
	return annotated.Bytes(), int64Value(snapshot["version_number"]), len(comments), nil
}

func pdfCPUConfiguration() *model.Configuration {
	// Review exports only use built-in sticky notes, so they need no user fonts
	// or pdfcpu configuration directory outside Review Hub's managed data.
	pdfCPUConfigOnce.Do(pdfapi.DisableConfigDir)
	return model.NewDefaultConfiguration()
}

func reviewAnnotations(comments []map[string]any, dimensions []types.Dim) map[int][]model.AnnotationRenderer {
	annotations := make(map[int][]model.AnnotationRenderer)
	if len(dimensions) == 0 {
		return annotations
	}
	fallbackSlots := make(map[int]int)
	for index, comment := range comments {
		pageNumber := int(int64Value(comment["page_number"]))
		if pageNumber < 1 || pageNumber > len(dimensions) {
			pageNumber = 1
		}
		dimension := dimensions[pageNumber-1]
		rectangle, anchored := anchoredAnnotationRectangle(comment, dimension)
		if !anchored {
			rectangle = pageSideAnnotationRectangle(dimension, fallbackSlots[pageNumber])
			fallbackSlots[pageNumber]++
		}
		id := strings.TrimSpace(stringMapValue(comment, "id"))
		if id == "" {
			id = strconv.Itoa(index + 1)
		}
		annotations[pageNumber] = append(annotations[pageNumber], model.NewTextAnnotation(
			rectangle,
			0,
			reviewAnnotationContents(comment, anchored),
			"review-hub-"+id,
			"",
			0,
			&color.Green,
			"Review Hub",
			nil,
			nil,
			"",
			"Review comment",
			0,
			0,
			1,
			false,
			"Comment",
		))
	}
	if len(comments) == 0 {
		annotations[1] = append(annotations[1], model.NewTextAnnotation(
			pageSideAnnotationRectangle(dimensions[0], 0),
			0,
			"[Review Hub]\nNo active review comments were recorded for this snapshot.\nPage note",
			"review-hub-page-note",
			"",
			0,
			&color.Gray,
			"Review Hub",
			nil,
			nil,
			"",
			"Review export",
			0,
			0,
			1,
			false,
			"Comment",
		))
	}
	return annotations
}

func anchoredAnnotationRectangle(comment map[string]any, dimension types.Dim) (types.Rectangle, bool) {
	if dimension.Width <= 0 || dimension.Height <= 0 {
		return types.Rectangle{}, false
	}
	x, xOK := float64MapValue(comment, "normalized_x")
	y, yOK := float64MapValue(comment, "normalized_y")
	if !xOK || !yOK || x < 0 || x > 1 || y < 0 || y > 1 {
		return types.Rectangle{}, false
	}
	width, widthOK := float64MapValue(comment, "normalized_width")
	height, heightOK := float64MapValue(comment, "normalized_height")
	if !widthOK || width <= 0 {
		width = 18 / dimension.Width
	}
	if !heightOK || height <= 0 {
		height = 18 / dimension.Height
	}
	width = math.Min(width, 1-x)
	height = math.Min(height, 1-y)
	if width <= 0 || height <= 0 {
		return types.Rectangle{}, false
	}
	left := clampFloat(x*dimension.Width, 0, dimension.Width)
	bottom := clampFloat((1-y-height)*dimension.Height, 0, dimension.Height)
	right := clampFloat(left+math.Max(width*dimension.Width, 12), left, dimension.Width)
	top := clampFloat(bottom+math.Max(height*dimension.Height, 12), bottom, dimension.Height)
	if right <= left || top <= bottom {
		return types.Rectangle{}, false
	}
	return *types.NewRectangle(left, bottom, right, top), true
}

func pageSideAnnotationRectangle(dimension types.Dim, slot int) types.Rectangle {
	size := math.Min(18, math.Min(dimension.Width, dimension.Height)/4)
	if size <= 0 {
		size = 1
	}
	margin := math.Min(18, size)
	spacing := size + 6
	rows := int(math.Max(1, math.Floor((dimension.Height-2*margin)/spacing)))
	column := slot / rows
	row := slot % rows
	left := math.Max(0, dimension.Width-margin-size-float64(column)*spacing)
	bottom := math.Max(0, dimension.Height-margin-size-float64(row)*spacing)
	right := math.Min(dimension.Width, left+size)
	top := math.Min(dimension.Height, bottom+size)
	return *types.NewRectangle(left, bottom, right, top)
}

func reviewAnnotationContents(comment map[string]any, anchored bool) string {
	author := strings.TrimSpace(stringMapValue(comment, "author_display_name"))
	if author == "" {
		author = "Unknown reviewer"
	}
	parts := []string{"[Review Hub] " + author}
	for _, key := range []string{"category", "priority", "status"} {
		if value := strings.TrimSpace(stringMapValue(comment, key)); value != "" {
			parts = append(parts, key+": "+value)
		}
	}
	if anchored {
		parts = append(parts, "Anchor: page coordinates")
	} else {
		parts = append(parts, "Page note: anchor unavailable")
	}
	if content := strings.TrimSpace(stringMapValue(comment, "content")); content != "" {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n")
}

func float64MapValue(value map[string]any, key string) (float64, bool) {
	switch item := value[key].(type) {
	case float64:
		return item, true
	case float32:
		return float64(item), true
	case int64:
		return float64(item), true
	case int:
		return float64(item), true
	case json.Number:
		parsed, err := item.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func clampFloat(value, lower, upper float64) float64 {
	return math.Max(lower, math.Min(value, upper))
}

func addZipBytes(writer *zip.Writer, name string, content []byte) error {
	entry, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(content)
	return err
}

func addZipReader(writer *zip.Writer, name string, reader io.Reader, stat os.FileInfo) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetModTime(stat.ModTime())
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, reader)
	return err
}

func exportCSV(comments []map[string]any) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	fields := []string{"id", "snapshot_id", "page_number", "status", "category", "priority", "author_id", "assignee_id", "linked_git_commit_sha", "created_at", "content"}
	if err := writer.Write(fields); err != nil {
		return nil, err
	}
	for _, comment := range comments {
		row := make([]string, len(fields))
		for i, field := range fields {
			row[i] = fmt.Sprint(comment[field])
			if comment[field] == nil {
				row[i] = ""
			}
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

func exportMarkdown(payload repository.ExportPayload) string {
	name, _ := payload.Project["name"].(string)
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 审阅导出：%s\n\n## 快照\n", name)
	for _, snapshot := range payload.Snapshots {
		version := fmt.Sprint(snapshot["version_number"])
		hash := fmt.Sprint(snapshot["sha256"])
		branch := fmt.Sprint(snapshot["git_branch"])
		if branch == "<nil>" || branch == "" {
			branch = "Git 不可用"
		}
		fmt.Fprintf(&builder, "- #%s | %s | %s\n", version, hash, branch)
	}
	builder.WriteString("\n## 评论\n")
	for _, comment := range payload.Comments {
		page := fmt.Sprint(comment["page_number"])
		status := fmt.Sprint(comment["status"])
		content := fmt.Sprint(comment["content"])
		fmt.Fprintf(&builder, "### 第 %s 页 [%s]\n%s\n\n", page, status, content)
	}
	return builder.String()
}

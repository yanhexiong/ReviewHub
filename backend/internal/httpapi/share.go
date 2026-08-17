package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/secretbox"
)

func shareTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return fmtHex(digest[:])
}
func fmtHex(value []byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2], result[index*2+1] = alphabet[item>>4], alphabet[item&15]
	}
	return string(result)
}
func shareCookieName(id string) string { return "review_share_" + id }

func (api *API) activeShare(request *http.Request) (*repository.Share, error) {
	token := request.PathValue("token")
	if token == "" {
		return nil, domain.NewAPIError(404, "SHARE_NOT_FOUND", "分享链接不存在或已关闭")
	}
	share, err := api.store.ShareByTokenHash(request.Context(), shareTokenHash(token))
	if err != nil {
		return nil, err
	}
	if share == nil {
		return nil, domain.NewAPIError(404, "SHARE_NOT_FOUND", "分享链接不存在或已关闭")
	}
	cookie, err := request.Cookie(shareCookieName(share.ID))
	if err != nil || cookie.Value != token {
		return nil, domain.NewAPIError(401, "SHARE_PASSWORD_REQUIRED", "请输入分享访问密码")
	}
	if err := api.store.TouchShare(request.Context(), share.ID); err != nil {
		return nil, err
	}
	return share, nil
}

func (api *API) projectShares(response http.ResponseWriter, request *http.Request) {
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
	rows, err := api.store.ListShareHistory(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{}
		for key, value := range row {
			item[key] = value
		}
		if cipher, ok := row["token_ciphertext"].(string); ok && cipher != "" {
			item["token"], _ = secretbox.Decrypt(api.encryptionKey, cipher)
		}
		if cipher, ok := row["password_ciphertext"].(string); ok && cipher != "" {
			item["password"], _ = secretbox.Decrypt(api.encryptionKey, cipher)
		}
		delete(item, "token_ciphertext")
		delete(item, "password_ciphertext")
		items = append(items, item)
	}
	writeJSON(response, http.StatusOK, map[string]any{"shares": items})
}

func (api *API) createProjectShare(response http.ResponseWriter, request *http.Request) {
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
	var input struct{ SnapshotID, Password, Permission string }
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.SnapshotID, input.Permission = strings.TrimSpace(input.SnapshotID), strings.TrimSpace(input.Permission)
	if _, err := uuid.Parse(input.SnapshotID); err != nil || len(input.Password) < 6 || len(input.Password) > 200 || (input.Permission != "view" && input.Permission != "comment") {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	snapshot, err := api.store.SnapshotByID(request.Context(), projectID, input.SnapshotID)
	if err != nil {
		writeError(response, err)
		return
	}
	if snapshot == nil {
		writeError(response, domain.NewAPIError(404, "SNAPSHOT_NOT_FOUND", "要分享的快照不存在"))
		return
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeError(response, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(response, err)
		return
	}
	guestHash, err := bcrypt.GenerateFromPassword(tokenBytes, bcrypt.DefaultCost)
	if err != nil {
		writeError(response, err)
		return
	}
	id := uuid.NewString()
	tokenCiphertext, err := secretbox.Encrypt(api.encryptionKey, token)
	if err != nil {
		writeError(response, err)
		return
	}
	passwordCiphertext, err := secretbox.Encrypt(api.encryptionKey, input.Password)
	if err != nil {
		writeError(response, err)
		return
	}
	share := repository.Share{ID: id, TokenHash: shareTokenHash(token), TokenCiphertext: tokenCiphertext, ProjectID: projectID, SnapshotID: input.SnapshotID, PasswordHash: string(passwordHash), PasswordCiphertext: passwordCiphertext, Permission: input.Permission, CreatedByUserID: user.ID}
	if err := api.store.CreateShare(request.Context(), share, string(guestHash)); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "share_link", id, "created", map[string]any{"snapshotId": input.SnapshotID, "permission": input.Permission})
	writeJSON(response, http.StatusCreated, map[string]any{"share": map[string]any{"id": id, "token": token, "password": input.Password, "permission": input.Permission}})
}

func (api *API) revokeProjectShare(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID, shareID := request.PathValue("projectID"), request.PathValue("shareID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	share, err := api.store.ShareByID(request.Context(), projectID, shareID)
	if err != nil {
		writeError(response, err)
		return
	}
	if share == nil {
		writeError(response, domain.NewAPIError(404, "SHARE_NOT_FOUND", "分享链接不存在"))
		return
	}
	if err := api.store.RevokeShare(request.Context(), projectID, shareID); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "share_link", shareID, "revoked", nil)
	writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (api *API) shareAccess(response http.ResponseWriter, request *http.Request) {
	token := request.PathValue("token")
	share, err := api.store.ShareByTokenHash(request.Context(), shareTokenHash(token))
	if err != nil {
		writeError(response, err)
		return
	}
	if share == nil {
		writeError(response, domain.NewAPIError(404, "SHARE_NOT_FOUND", "分享链接不存在或已关闭"))
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if len(input.Password) == 0 || len(input.Password) > 200 || bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(input.Password)) != nil {
		writeError(response, domain.NewAPIError(401, "SHARE_PASSWORD_INVALID", "分享访问密码不正确"))
		return
	}
	http.SetCookie(response, &http.Cookie{Name: shareCookieName(share.ID), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400})
	_ = api.store.TouchShare(request.Context(), share.ID)
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) shareOverview(response http.ResponseWriter, request *http.Request) {
	share, err := api.activeShare(request)
	if err != nil {
		writeError(response, err)
		return
	}
	overview, err := api.store.ShareOverview(request.Context(), *share)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, overview)
}

func (api *API) shareComment(response http.ResponseWriter, request *http.Request) {
	share, err := api.activeShare(request)
	if err != nil {
		writeError(response, err)
		return
	}
	if share.Permission != "comment" {
		writeError(response, domain.NewAPIError(403, "SHARE_READ_ONLY", "此分享链接仅允许查看"))
		return
	}
	var input struct {
		PageNumber int64  `json:"pageNumber"`
		Content    string `json:"content"`
		Category   string `json:"category"`
		Priority   string `json:"priority"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	comment, err := api.review.CreateShareComment(request.Context(), *share, repository.NewComment{SnapshotID: share.SnapshotID, PageNumber: input.PageNumber, AnchorType: "page_note", Content: strings.TrimSpace(input.Content), Category: strings.TrimSpace(input.Category), Priority: input.Priority})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"id": comment["id"]})
}

func (api *API) sharePDF(response http.ResponseWriter, request *http.Request) {
	share, err := api.activeShare(request)
	if err != nil {
		writeError(response, err)
		return
	}
	snapshot, err := api.store.ShareSnapshotPath(request.Context(), share.SnapshotID)
	if err != nil {
		writeError(response, err)
		return
	}
	if snapshot == nil {
		writeError(response, domain.NewAPIError(404, "NOT_FOUND", "快照不存在"))
		return
	}
	archived, _ := snapshot["archived_pdf_path"].(string)
	filePath, err := api.managedDataPath(archived)
	if err != nil {
		writeError(response, err)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		writeError(response, domain.NewAPIError(404, "PDF_NOT_FOUND", "快照 PDF 文件不存在"))
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
	response.Header().Set("Content-Type", "application/pdf")
	response.Header().Set("Content-Disposition", "inline")
	response.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(response, request, "snapshot.pdf", stat.ModTime(), file)
}

func (api *API) shareEvents(response http.ResponseWriter, request *http.Request) {
	share, err := api.activeShare(request)
	if err != nil {
		writeError(response, err)
		return
	}
	channel, cancel, revision := api.review.SubscribeShare(share.ProjectID)
	defer cancel()
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, domain.NewAPIError(500, "INTERNAL_ERROR", "服务器处理请求失败"))
		return
	}
	writeSSE(response, "connected", map[string]any{"projectId": share.ProjectID, "revision": revision})
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case event := <-channel:
			if event.SnapshotID != "" && event.SnapshotID != share.SnapshotID {
				continue
			}
			writeSSE(response, event.Type, event)
			flusher.Flush()
		case <-heartbeat.C:
			writeSSE(response, "heartbeat", map[string]any{"projectId": share.ProjectID, "revision": revision})
			flusher.Flush()
		}
	}
}

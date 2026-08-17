package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type PendingProjectPayload struct {
	UserID                     string `json:"userId"`
	Name                       string `json:"name"`
	Slug                       string `json:"slug"`
	Description                string `json:"description"`
	SourceType                 string `json:"sourceType"`
	SourceURI                  string `json:"sourceUri"`
	SourceBranch               string `json:"sourceBranch"`
	SourceCredentialCiphertext string `json:"sourceCredentialCiphertext"`
	RequestedPDFPath           string `json:"requestedPdfPath"`
	GitUserName                string `json:"gitUserName"`
	GitUserEmail               string `json:"gitUserEmail"`
}

type PendingProject struct {
	ID            string
	UserID        string
	Payload       PendingProjectPayload
	WorkspacePath string
}

func (store *Store) CreatePendingProject(ctx context.Context, workspacePath string, payload PendingProjectPayload) (PendingProject, error) {
	if strings.TrimSpace(payload.UserID) == "" || strings.TrimSpace(workspacePath) == "" {
		return PendingProject{}, domain.NewAPIError(400, "VALIDATION_ERROR", "待处理项目参数无效")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return PendingProject{}, err
	}
	item := PendingProject{ID: uuid.NewString(), UserID: payload.UserID, Payload: payload, WorkspacePath: workspacePath}
	_, err = store.database.ExecContext(ctx,
		"INSERT INTO pending_project_creations(id,user_id,payload_json,workspace_path,created_at) VALUES(?,?,?,?,?)",
		item.ID, item.UserID, encoded, workspacePath, time.Now().UnixMilli())
	if err != nil {
		return PendingProject{}, err
	}
	return item, nil
}

func (store *Store) PendingProject(ctx context.Context, id, userID string) (PendingProject, error) {
	var item PendingProject
	var encoded string
	err := store.database.QueryRowContext(ctx,
		"SELECT id,user_id,payload_json,workspace_path FROM pending_project_creations WHERE id=? AND user_id=?",
		id, userID).Scan(&item.ID, &item.UserID, &encoded, &item.WorkspacePath)
	if errors.Is(err, sql.ErrNoRows) {
		return PendingProject{}, domain.NewAPIError(404, "PENDING_NOT_FOUND", "待创建项目不存在或已过期")
	}
	if err != nil {
		return PendingProject{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &item.Payload); err != nil {
		return PendingProject{}, domain.NewAPIError(500, "PENDING_INVALID", "待创建项目记录已损坏")
	}
	return item, nil
}

func (store *Store) DeletePendingProject(ctx context.Context, id, userID string) error {
	result, err := store.database.ExecContext(ctx, "DELETE FROM pending_project_creations WHERE id=? AND user_id=?", id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return domain.NewAPIError(404, "PENDING_NOT_FOUND", "待创建项目不存在或已过期")
	}
	return err
}

package repository

import (
	"context"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type ExportPayload struct {
	Project   map[string]any   `json:"project"`
	Snapshots []map[string]any `json:"snapshots"`
	Comments  []map[string]any `json:"comments"`
	Replies   []map[string]any `json:"replies"`
	Links     []map[string]any `json:"links"`
}

func (store *Store) ExportPayload(ctx context.Context, projectID, snapshotID string) (ExportPayload, error) {
	project, err := store.ProjectByID(ctx, projectID)
	if err != nil {
		return ExportPayload{}, err
	}
	project = publicProject(project)

	args := []any{projectID}
	snapshotQuery := "SELECT * FROM pdf_snapshots WHERE project_id=? ORDER BY version_number"
	if snapshotID != "" {
		snapshotQuery = "SELECT * FROM pdf_snapshots WHERE project_id=? AND id=? ORDER BY version_number"
		args = append(args, snapshotID)
	}
	snapshots, err := store.queryMaps(ctx, snapshotQuery, args...)
	if err != nil {
		return ExportPayload{}, err
	}
	for i := range snapshots {
		snapshots[i] = publicSnapshot(snapshots[i])
	}
	if snapshotID != "" && len(snapshots) == 0 {
		return ExportPayload{}, domain.NewAPIError(404, "SNAPSHOT_NOT_FOUND", "要导出的版本不存在")
	}

	commentArgs := []any{projectID}
	commentQuery := "SELECT review_comments.*, users.display_name AS author_display_name FROM review_comments LEFT JOIN users ON users.id=review_comments.author_id WHERE review_comments.project_id=? AND review_comments.deleted_at IS NULL ORDER BY review_comments.created_at"
	if snapshotID != "" {
		commentQuery = "SELECT review_comments.*, users.display_name AS author_display_name FROM review_comments LEFT JOIN users ON users.id=review_comments.author_id WHERE review_comments.project_id=? AND review_comments.snapshot_id=? AND review_comments.deleted_at IS NULL ORDER BY review_comments.created_at"
		commentArgs = append(commentArgs, snapshotID)
	}
	comments, err := store.queryMaps(ctx, commentQuery, commentArgs...)
	if err != nil {
		return ExportPayload{}, err
	}

	commentIDs := make([]string, 0, len(comments))
	for _, comment := range comments {
		if id, ok := comment["id"].(string); ok && id != "" {
			commentIDs = append(commentIDs, id)
		}
	}
	replies := []map[string]any{}
	links := []map[string]any{}
	if len(commentIDs) > 0 {
		placeholders := make([]byte, 0, len(commentIDs)*2)
		values := make([]any, 0, len(commentIDs))
		for i, id := range commentIDs {
			if i > 0 {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '?')
			values = append(values, id)
		}
		replies, err = store.queryMaps(ctx, "SELECT * FROM comment_replies WHERE comment_id IN ("+string(placeholders)+") ORDER BY created_at", values...)
		if err != nil {
			return ExportPayload{}, err
		}
		links, err = store.queryMaps(ctx, "SELECT * FROM comment_version_links WHERE source_comment_id IN ("+string(placeholders)+")", values...)
		if err != nil {
			return ExportPayload{}, err
		}
	}
	return ExportPayload{Project: project, Snapshots: snapshots, Comments: comments, Replies: replies, Links: links}, nil
}

func (store *Store) SnapshotFile(ctx context.Context, projectID, snapshotID string) (map[string]any, error) {
	snapshot, err := store.SnapshotByID(ctx, projectID, snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, domain.NewAPIError(404, "SNAPSHOT_NOT_FOUND", "要导出的版本不存在")
	}
	return snapshot, nil
}

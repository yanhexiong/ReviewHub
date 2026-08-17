package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CommentFilter struct {
	SnapshotID string
	Status     string
	Category   string
	Priority   string
	Limit      int
	Offset     int
}

type NewComment struct {
	SnapshotID          string   `json:"snapshotId"`
	PageNumber          int64    `json:"pageNumber"`
	AnchorType          string   `json:"anchorType"`
	NormalizedX         *float64 `json:"normalizedX"`
	NormalizedY         *float64 `json:"normalizedY"`
	NormalizedWidth     *float64 `json:"normalizedWidth"`
	NormalizedHeight    *float64 `json:"normalizedHeight"`
	SelectedText        *string  `json:"selectedText"`
	SelectedTextContext *string  `json:"selectedTextContext"`
	Content             string   `json:"content"`
	Category            string   `json:"category"`
	Priority            string   `json:"priority"`
}

func (store *Store) ListComments(ctx context.Context, projectID string, filter CommentFilter) ([]map[string]any, error) {
	clauses := []string{"review_comments.project_id=?", "review_comments.deleted_at IS NULL"}
	values := []any{projectID}
	for _, item := range []struct {
		value  string
		column string
	}{
		{filter.SnapshotID, "snapshot_id"},
		{filter.Status, "status"},
		{filter.Category, "category"},
		{filter.Priority, "priority"},
	} {
		if item.value != "" {
			clauses = append(clauses, "review_comments."+item.column+"=?")
			values = append(values, item.value)
		}
	}
	values = append(values, filter.Limit, filter.Offset)
	rows, err := store.database.QueryContext(ctx,
		`SELECT review_comments.*, users.display_name AS author_display_name,
                (SELECT COUNT(*) FROM comment_replies
                  WHERE comment_replies.comment_id=review_comments.id
                    AND comment_replies.deleted_at IS NULL) AS reply_count
         FROM review_comments JOIN users ON users.id=review_comments.author_id
         WHERE `+strings.Join(clauses, " AND ")+` ORDER BY review_comments.created_at DESC LIMIT ? OFFSET ?`, values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapsFromRows(rows)
}

func (store *Store) CommentByID(ctx context.Context, commentID string) (map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT review_comments.*, users.display_name AS author_display_name,
                (SELECT COUNT(*) FROM comment_replies
                  WHERE comment_replies.comment_id=review_comments.id
                    AND comment_replies.deleted_at IS NULL) AS reply_count
         FROM review_comments JOIN users ON users.id=review_comments.author_id
         WHERE review_comments.id=?`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := mapsFromRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

func (store *Store) CreateComment(ctx context.Context, projectID, authorID string, input NewComment) (map[string]any, error) {
	now := time.Now().UnixMilli()
	id := uuid.NewString()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO review_comments(
          id,project_id,snapshot_id,page_number,anchor_type,
          normalized_x,normalized_y,normalized_width,normalized_height,
          selected_text,selected_text_context,content,category,priority,status,
          author_id,assignee_id,linked_git_commit_sha,linked_pull_request_url,
          created_at,updated_at,resolved_at,resolved_by_user_id,resolution_note,deleted_at
        ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, input.SnapshotID, input.PageNumber, input.AnchorType,
		input.NormalizedX, input.NormalizedY, input.NormalizedWidth, input.NormalizedHeight,
		input.SelectedText, input.SelectedTextContext, input.Content, input.Category, input.Priority, "open",
		authorID, nil, nil, nil, now, now, nil, nil, nil, nil,
	)
	if err != nil {
		return nil, err
	}
	return store.CommentByID(ctx, id)
}

func (store *Store) SnapshotBelongsToProject(ctx context.Context, snapshotID, projectID string) (bool, error) {
	var id string
	err := store.database.QueryRowContext(ctx, "SELECT id FROM pdf_snapshots WHERE id=? AND project_id=?", snapshotID, projectID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (store *Store) SoftDeleteComment(ctx context.Context, commentID string) error {
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx, "UPDATE review_comments SET deleted_at=?,updated_at=? WHERE id=?", now, now, commentID)
	return err
}

func (store *Store) TransitionComment(ctx context.Context, commentID, status string, note *string, actorID string) error {
	now := time.Now().UnixMilli()
	var resolvedAt any
	var resolvedBy any
	if status == "resolved" {
		resolvedAt, resolvedBy = now, actorID
	}
	_, err := store.database.ExecContext(ctx,
		"UPDATE review_comments SET status=?,updated_at=?,resolved_at=?,resolved_by_user_id=?,resolution_note=? WHERE id=?",
		status, now, resolvedAt, resolvedBy, note, commentID,
	)
	return err
}

func (store *Store) ListReplies(ctx context.Context, commentID string) ([]map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT comment_replies.*,users.display_name
         FROM comment_replies JOIN users ON users.id=comment_replies.author_id
         WHERE comment_replies.comment_id=? AND comment_replies.deleted_at IS NULL
         ORDER BY comment_replies.created_at ASC`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapsFromRows(rows)
}

func (store *Store) CreateReply(ctx context.Context, commentID, authorID, content string) (map[string]any, error) {
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		"INSERT INTO comment_replies(id,comment_id,author_id,content,created_at,updated_at,deleted_at) VALUES(?,?,?,?,?,?,NULL)",
		id, commentID, authorID, content, now, now,
	)
	if err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx,
		`SELECT comment_replies.*,users.display_name
         FROM comment_replies JOIN users ON users.id=comment_replies.author_id WHERE comment_replies.id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := mapsFromRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) != 1 {
		return nil, fmt.Errorf("created reply was not found")
	}
	return items[0], nil
}

func (store *Store) ReplyCount(ctx context.Context, commentID string) (int64, error) {
	var count int64
	err := store.database.QueryRowContext(ctx, "SELECT COUNT(*) FROM comment_replies WHERE comment_id=? AND deleted_at IS NULL", commentID).Scan(&count)
	return count, err
}

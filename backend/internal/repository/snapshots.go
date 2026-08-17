package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type NewSnapshot struct {
	ID                         string
	ProjectID                  string
	VersionNumber              int64
	OriginalPDFPath            string
	ArchivedPDFPath            string
	OriginalFileName           string
	FileSizeBytes              int64
	SHA256                     string
	PageCount                  int64
	PageTextHashes             string
	SnapshotLabel              string
	SnapshotNote               string
	SourceKind                 string
	ArchivedByUserID           string
	GitRepositoryRoot          string
	GitBranch                  string
	GitCommitSHA               string
	GitCommitShortSHA          string
	GitCommitMessage           string
	GitCommitAuthor            string
	GitCommitTimestamp         string
	GitWorktreeDirty           *int64
	GitDiffStatSummary         string
	PreviousSnapshotID         string
	SourcePatch                string
	SourceUntrackedArchivePath string
}

// CreateSnapshot is the persistence primitive used by the future Go archive
// adapter. It intentionally does not read or write PDF files itself.
func (store *Store) CreateSnapshot(ctx context.Context, input NewSnapshot) error {
	_, err := store.createSnapshot(ctx, input, false)
	return err
}

// CreateSnapshotAtomic inserts an archive row while allocating the next
// project version under SQLite's write transaction. Keeping version
// allocation beside the unique constraint prevents concurrent uploads from
// producing duplicate version numbers.
func (store *Store) CreateSnapshotAtomic(ctx context.Context, input NewSnapshot) (NewSnapshot, error) {
	return store.createSnapshot(ctx, input, true)
}

func (store *Store) createSnapshot(ctx context.Context, input NewSnapshot, allocateVersion bool) (NewSnapshot, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return input, err
	}
	defer transaction.Rollback()
	if allocateVersion {
		var latestVersion int64
		if err := transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_number),0) FROM pdf_snapshots WHERE project_id=?", input.ProjectID).Scan(&latestVersion); err != nil {
			return input, err
		}
		if input.VersionNumber <= 0 || input.VersionNumber != latestVersion+1 {
			input.VersionNumber = latestVersion + 1
		}
		if input.PreviousSnapshotID == "" && latestVersion > 0 {
			_ = transaction.QueryRowContext(ctx, "SELECT id FROM pdf_snapshots WHERE project_id=? AND version_number=?", input.ProjectID, latestVersion).Scan(&input.PreviousSnapshotID)
		}
		var duplicateVersion int64
		if err := transaction.QueryRowContext(ctx, "SELECT version_number FROM pdf_snapshots WHERE project_id=? AND sha256=? LIMIT 1", input.ProjectID, input.SHA256).Scan(&duplicateVersion); err == nil {
			return input, domain.NewAPIError(409, "DUPLICATE_SNAPSHOT", "当前 PDF 内容与已有快照一致，未创建重复快照")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return input, err
		}
	}
	pageHashes := input.PageTextHashes
	if pageHashes == "" {
		pageHashes = "[]"
	}
	sourceKind := input.SourceKind
	if sourceKind == "" {
		sourceKind = "project"
	}
	now := time.Now().UnixMilli()
	var dirty any
	if input.GitWorktreeDirty != nil {
		dirty = *input.GitWorktreeDirty
	}
	_, err = transaction.ExecContext(ctx,
		`INSERT INTO pdf_snapshots(
          id,project_id,version_number,original_pdf_path,archived_pdf_path,original_file_name,
          file_size_bytes,sha256,page_count,page_text_hashes_json,snapshot_label,snapshot_note,
		  git_repository_root,git_branch,git_commit_sha,git_commit_short_sha,git_commit_message,
		  git_commit_author,git_commit_timestamp,git_worktree_dirty,git_diff_stat_summary,
		  source_kind,previous_snapshot_id,source_patch,source_untracked_archive_path,
		  created_at,archived_at,archived_by_user_id
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.ID, input.ProjectID, input.VersionNumber, input.OriginalPDFPath, input.ArchivedPDFPath,
		input.OriginalFileName, input.FileSizeBytes, input.SHA256, input.PageCount, pageHashes,
		nullableText(input.SnapshotLabel), nullableText(input.SnapshotNote),
		nullableText(input.GitRepositoryRoot), nullableText(input.GitBranch), nullableText(input.GitCommitSHA),
		nullableText(input.GitCommitShortSHA), nullableText(input.GitCommitMessage), nullableText(input.GitCommitAuthor),
		nullableText(input.GitCommitTimestamp), dirty, nullableText(input.GitDiffStatSummary), sourceKind,
		nullableText(input.PreviousSnapshotID), nullableText(input.SourcePatch), nullableText(input.SourceUntrackedArchivePath),
		now, now, input.ArchivedByUserID,
	)
	if err != nil {
		return input, err
	}
	if err := transaction.Commit(); err != nil {
		return input, err
	}
	return input, nil
}

func (store *Store) ListSnapshots(ctx context.Context, projectID string) ([]map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT pdf_snapshots.*,
                (SELECT COUNT(*) FROM review_comments
                 WHERE review_comments.snapshot_id=pdf_snapshots.id
                   AND review_comments.deleted_at IS NULL) AS comment_count
         FROM pdf_snapshots
         WHERE project_id=?
         ORDER BY version_number DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshots, err := mapsFromRows(rows)
	if err != nil {
		return nil, err
	}
	for index := range snapshots {
		snapshots[index] = publicSnapshot(snapshots[index])
	}
	return snapshots, rows.Err()
}

func (store *Store) SnapshotByID(ctx context.Context, projectID, snapshotID string) (map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT pdf_snapshots.*,
                (SELECT COUNT(*) FROM review_comments
                 WHERE review_comments.snapshot_id=pdf_snapshots.id
                   AND review_comments.deleted_at IS NULL) AS comment_count
         FROM pdf_snapshots
         WHERE project_id=? AND id=?`, projectID, snapshotID)
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

// ListSnapshotComments returns all active comments for one snapshot. It is
// separate from the paginated project comments endpoint so a compare response
// cannot silently omit older comments.
func (store *Store) ListSnapshotComments(ctx context.Context, projectID, snapshotID string) ([]map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT review_comments.*, users.display_name AS author_display_name,
                (SELECT COUNT(*) FROM comment_replies
                  WHERE comment_replies.comment_id=review_comments.id
                    AND comment_replies.deleted_at IS NULL) AS reply_count
         FROM review_comments LEFT JOIN users ON users.id=review_comments.author_id
         WHERE review_comments.project_id=? AND review_comments.snapshot_id=?
           AND review_comments.deleted_at IS NULL
         ORDER BY review_comments.created_at ASC`, projectID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapsFromRows(rows)
}

// DeleteSnapshot removes a snapshot and every database record bound to it.
// The caller moves archived files after the transaction commits; the returned
// raw row is kept server-side only.
func (store *Store) DeleteSnapshot(ctx context.Context, projectID, snapshotID string) (map[string]any, error) {
	snapshot, err := store.SnapshotByID(ctx, projectID, snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, domain.NewAPIError(404, "NOT_FOUND", "快照不存在")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback()
	shareRows, err := transaction.QueryContext(ctx, "SELECT id FROM share_links WHERE project_id=? AND snapshot_id=?", projectID, snapshotID)
	if err != nil {
		return nil, err
	}
	shareIDs := []string{}
	for shareRows.Next() {
		var shareID string
		if err := shareRows.Scan(&shareID); err != nil {
			_ = shareRows.Close()
			return nil, err
		}
		shareIDs = append(shareIDs, shareID)
	}
	if err := shareRows.Err(); err != nil {
		_ = shareRows.Close()
		return nil, err
	}
	if err := shareRows.Close(); err != nil {
		return nil, err
	}
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM comment_replies WHERE comment_id IN (SELECT id FROM review_comments WHERE project_id=? AND snapshot_id=?)", []any{projectID, snapshotID}},
		{"DELETE FROM comment_version_links WHERE source_snapshot_id=? OR target_snapshot_id=?", []any{snapshotID, snapshotID}},
		{"DELETE FROM review_comments WHERE project_id=? AND snapshot_id=?", []any{projectID, snapshotID}},
		{"DELETE FROM share_links WHERE project_id=? AND snapshot_id=?", []any{projectID, snapshotID}},
		{"DELETE FROM pdf_snapshots WHERE project_id=? AND id=?", []any{projectID, snapshotID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return nil, err
		}
	}
	for _, shareID := range shareIDs {
		if _, err := transaction.ExecContext(ctx, "DELETE FROM users WHERE id=?", "share-"+shareID); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (store *Store) UpdateSnapshotMetadata(ctx context.Context, snapshotID, label, note string) error {
	_, err := store.database.ExecContext(ctx,
		"UPDATE pdf_snapshots SET snapshot_label=?,snapshot_note=? WHERE id=?",
		nullableText(label), nullableText(note), snapshotID)
	return err
}

func PublicSnapshot(snapshot map[string]any) map[string]any {
	return publicSnapshot(snapshot)
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func publicSnapshot(snapshot map[string]any) map[string]any {
	result := make(map[string]any, len(snapshot)+3)
	for key, value := range snapshot {
		switch key {
		case "original_pdf_path", "archived_pdf_path", "git_repository_root", "source_patch", "source_untracked_archive_path":
			continue
		default:
			result[key] = value
		}
	}
	sourceKind, _ := snapshot["source_kind"].(string)
	if sourceKind != "external" {
		sourceKind = "project"
	}
	result["source_kind"] = sourceKind
	result["has_git_source"] = hasText(snapshot["git_repository_root"])
	result["has_source_patch"] = hasText(snapshot["source_patch"])
	result["has_untracked_source"] = hasText(snapshot["source_untracked_archive_path"])
	return result
}

func hasText(value any) bool {
	text, ok := value.(string)
	return ok && text != ""
}

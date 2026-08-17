package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Share struct {
	ID                 string
	TokenHash          string
	TokenCiphertext    string
	ProjectID          string
	SnapshotID         string
	PasswordHash       string
	PasswordCiphertext string
	Permission         string
	Status             string
	CreatedByUserID    string
	CreatedAt          int64
	LastUsedAt         *int64
}

func (store *Store) ShareByTokenHash(ctx context.Context, tokenHash string) (*Share, error) {
	var share Share
	var tokenCiphertext, passwordCiphertext sql.NullString
	var lastUsed sql.NullInt64
	err := store.database.QueryRowContext(ctx,
		`SELECT id,token_hash,token_ciphertext,project_id,snapshot_id,password_hash,password_ciphertext,
		 permission,status,created_by_user_id,created_at,last_used_at
		 FROM share_links WHERE token_hash=? AND status='active'`, tokenHash,
	).Scan(&share.ID, &share.TokenHash, &tokenCiphertext, &share.ProjectID, &share.SnapshotID, &share.PasswordHash, &passwordCiphertext, &share.Permission, &share.Status, &share.CreatedByUserID, &share.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	share.TokenCiphertext, share.PasswordCiphertext = tokenCiphertext.String, passwordCiphertext.String
	if lastUsed.Valid {
		value := lastUsed.Int64
		share.LastUsedAt = &value
	}
	return &share, nil
}

func (store *Store) ShareByID(ctx context.Context, projectID, shareID string) (*Share, error) {
	var share Share
	var tokenCiphertext, passwordCiphertext sql.NullString
	var lastUsed sql.NullInt64
	err := store.database.QueryRowContext(ctx,
		`SELECT id,token_hash,token_ciphertext,project_id,snapshot_id,password_hash,password_ciphertext,
		 permission,status,created_by_user_id,created_at,last_used_at
		 FROM share_links WHERE id=? AND project_id=?`, shareID, projectID,
	).Scan(&share.ID, &share.TokenHash, &tokenCiphertext, &share.ProjectID, &share.SnapshotID, &share.PasswordHash, &passwordCiphertext, &share.Permission, &share.Status, &share.CreatedByUserID, &share.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	share.TokenCiphertext, share.PasswordCiphertext = tokenCiphertext.String, passwordCiphertext.String
	if lastUsed.Valid {
		value := lastUsed.Int64
		share.LastUsedAt = &value
	}
	return &share, nil
}

func (store *Store) ListShareHistory(ctx context.Context, projectID string) ([]map[string]any, error) {
	return store.queryMaps(ctx,
		`SELECT share_links.id,share_links.project_id,share_links.snapshot_id,
		        share_links.token_ciphertext,share_links.password_ciphertext,
		        share_links.permission,share_links.status,share_links.created_at,
		        share_links.last_used_at,pdf_snapshots.version_number,pdf_snapshots.snapshot_label
		 FROM share_links JOIN pdf_snapshots ON pdf_snapshots.id=share_links.snapshot_id
		 WHERE share_links.project_id=? ORDER BY share_links.created_at DESC`, projectID)
}

func (store *Store) CreateShare(ctx context.Context, share Share, guestPasswordHash string) error {
	now := time.Now().UnixMilli()
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	guestID := "share-" + share.ID
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO users(id,email,display_name,password_hash,role,is_active,created_at,updated_at,last_login_at)
		 VALUES(?,?,?,?,?,1,?,?,NULL)`, guestID, guestID+"@local.invalid", "分享访客", guestPasswordHash, "reviewer", now, now); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO share_links(id,token_hash,token_ciphertext,project_id,snapshot_id,password_hash,password_ciphertext,
		 permission,status,created_by_user_id,created_at,last_used_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL)`, share.ID, share.TokenHash, nullableText(share.TokenCiphertext), share.ProjectID, share.SnapshotID, share.PasswordHash, nullableText(share.PasswordCiphertext), share.Permission, "active", share.CreatedByUserID, now); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) RevokeShare(ctx context.Context, projectID, shareID string) error {
	_, err := store.database.ExecContext(ctx,
		"UPDATE share_links SET status='revoked',token_ciphertext=NULL,password_ciphertext=NULL WHERE id=? AND project_id=?", shareID, projectID)
	return err
}

func (store *Store) TouchShare(ctx context.Context, shareID string) error {
	_, err := store.database.ExecContext(ctx, "UPDATE share_links SET last_used_at=? WHERE id=?", time.Now().UnixMilli(), shareID)
	return err
}

func (store *Store) ShareGuestExists(ctx context.Context, shareID string) (bool, error) {
	var id string
	err := store.database.QueryRowContext(ctx, "SELECT id FROM users WHERE id=?", "share-"+shareID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (store *Store) ShareOverview(ctx context.Context, share Share) (map[string]any, error) {
	project, err := store.queryMaps(ctx, "SELECT id,name,description FROM projects WHERE id=?", share.ProjectID)
	if err != nil {
		return nil, err
	}
	snapshot, err := store.queryMaps(ctx, "SELECT id,version_number,page_count,snapshot_label,snapshot_note FROM pdf_snapshots WHERE id=?", share.SnapshotID)
	if err != nil {
		return nil, err
	}
	comments, err := store.queryMaps(ctx,
		`SELECT review_comments.*,users.display_name AS author_display_name
		 FROM review_comments JOIN users ON users.id=review_comments.author_id
		 WHERE review_comments.snapshot_id=? AND review_comments.deleted_at IS NULL ORDER BY review_comments.created_at`, share.SnapshotID)
	if err != nil {
		return nil, err
	}
	var projectValue, snapshotValue any
	if len(project) > 0 {
		projectValue = project[0]
	}
	if len(snapshot) > 0 {
		snapshotValue = snapshot[0]
	}
	return map[string]any{"project": projectValue, "snapshot": snapshotValue, "permission": share.Permission, "comments": comments}, nil
}

func (store *Store) ShareSnapshotPath(ctx context.Context, snapshotID string) (map[string]any, error) {
	rows, err := store.queryMaps(ctx, "SELECT archived_pdf_path,original_file_name FROM pdf_snapshots WHERE id=?", snapshotID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func NewShareID() string { return uuid.NewString() }

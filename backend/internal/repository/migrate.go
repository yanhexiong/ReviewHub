package repository

import (
	"context"
	"fmt"
)

// Migrate is intentionally owned by the Go service. It preserves the schema
// used by the existing Next application so upgrades can happen in place while
// the HTTP surface is moved endpoint by endpoint.
func (store *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users(
          id TEXT PRIMARY KEY,email TEXT UNIQUE NOT NULL,display_name TEXT NOT NULL,
          password_hash TEXT NOT NULL,role TEXT NOT NULL,is_active INTEGER NOT NULL DEFAULT 1,
          project_limit INTEGER,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,last_login_at INTEGER
        )`,
		`CREATE TABLE IF NOT EXISTS projects(
          id TEXT PRIMARY KEY,name TEXT NOT NULL,slug TEXT UNIQUE NOT NULL,description TEXT,
          repository_path TEXT NOT NULL,source_pdf_path TEXT NOT NULL,source_type TEXT NOT NULL DEFAULT 'local',
          source_uri TEXT,source_branch TEXT,workspace_path TEXT,source_credential_ciphertext TEXT,
          created_by_user_id TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER
        )`,
		`CREATE TABLE IF NOT EXISTS pdf_snapshots(
          id TEXT PRIMARY KEY,project_id TEXT NOT NULL,version_number INTEGER NOT NULL,
          original_pdf_path TEXT NOT NULL,archived_pdf_path TEXT NOT NULL,original_file_name TEXT NOT NULL,
          file_size_bytes INTEGER NOT NULL,sha256 TEXT NOT NULL,page_count INTEGER NOT NULL,
          page_text_hashes_json TEXT NOT NULL,snapshot_label TEXT,snapshot_note TEXT,
          git_repository_root TEXT,git_branch TEXT,git_commit_sha TEXT,git_commit_short_sha TEXT,
          git_commit_message TEXT,git_commit_author TEXT,git_commit_timestamp TEXT,git_worktree_dirty INTEGER,
          git_diff_stat_summary TEXT,source_kind TEXT NOT NULL DEFAULT 'project',previous_snapshot_id TEXT,
          source_patch TEXT,source_untracked_archive_path TEXT,created_at INTEGER NOT NULL,
          archived_at INTEGER NOT NULL,archived_by_user_id TEXT NOT NULL,UNIQUE(project_id,version_number)
        )`,
		`CREATE TABLE IF NOT EXISTS review_comments(
          id TEXT PRIMARY KEY,project_id TEXT NOT NULL,snapshot_id TEXT NOT NULL,page_number INTEGER NOT NULL,
          anchor_type TEXT NOT NULL,normalized_x REAL,normalized_y REAL,normalized_width REAL,
          normalized_height REAL,selected_text TEXT,selected_text_context TEXT,content TEXT NOT NULL,
          category TEXT NOT NULL,priority TEXT NOT NULL,status TEXT NOT NULL,author_id TEXT NOT NULL,
          assignee_id TEXT,linked_git_commit_sha TEXT,linked_pull_request_url TEXT,created_at INTEGER NOT NULL,
          updated_at INTEGER NOT NULL,resolved_at INTEGER,resolved_by_user_id TEXT,resolution_note TEXT,deleted_at INTEGER
        )`,
		`CREATE TABLE IF NOT EXISTS comment_replies(
          id TEXT PRIMARY KEY,comment_id TEXT NOT NULL,author_id TEXT NOT NULL,content TEXT NOT NULL,
          created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,deleted_at INTEGER
        )`,
		`CREATE TABLE IF NOT EXISTS comment_version_links(
          id TEXT PRIMARY KEY,source_comment_id TEXT NOT NULL,source_snapshot_id TEXT NOT NULL,
          target_snapshot_id TEXT NOT NULL,target_comment_id TEXT,relationship TEXT NOT NULL,
          created_by_user_id TEXT NOT NULL,note TEXT,created_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS audit_events(
          id TEXT PRIMARY KEY,project_id TEXT,actor_user_id TEXT,entity_type TEXT NOT NULL,
          entity_id TEXT NOT NULL,action TEXT NOT NULL DEFAULT '',before_json TEXT,after_json TEXT,
          created_at INTEGER NOT NULL,level TEXT NOT NULL DEFAULT 'info'
        )`,
		`CREATE TABLE IF NOT EXISTS app_settings(key TEXT PRIMARY KEY,value TEXT NOT NULL,updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS project_git_settings(
          project_id TEXT PRIMARY KEY NOT NULL,user_name TEXT,user_email TEXT,access_token_ciphertext TEXT,
          ssh_private_key_ciphertext TEXT,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS project_tex_settings(
          project_id TEXT PRIMARY KEY NOT NULL,engine TEXT NOT NULL DEFAULT 'pdflatex',
          build_tool TEXT NOT NULL DEFAULT 'latexmk',output_directory TEXT NOT NULL DEFAULT 'build',
          auto_build TEXT NOT NULL DEFAULT 'off',pdf_preview TEXT NOT NULL DEFAULT 'review-hub',
          sync_tex INTEGER NOT NULL DEFAULT 1,shell_escape INTEGER NOT NULL DEFAULT 0,texlive_bin_path TEXT,
          tex_root_path TEXT,profile_id TEXT,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS texlive_profiles(
          id TEXT PRIMARY KEY NOT NULL,name TEXT UNIQUE NOT NULL,engine TEXT NOT NULL,build_tool TEXT NOT NULL,
          output_directory TEXT NOT NULL,shell_escape INTEGER NOT NULL DEFAULT 0,texlive_bin_path TEXT,
          created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS user_vscode_defaults(
          user_id TEXT PRIMARY KEY NOT NULL,user_name TEXT,user_email TEXT,access_token_ciphertext TEXT,
          ssh_private_key_ciphertext TEXT,engine TEXT NOT NULL DEFAULT 'pdflatex',
          build_tool TEXT NOT NULL DEFAULT 'latexmk',output_directory TEXT NOT NULL DEFAULT 'build',
          auto_build TEXT NOT NULL DEFAULT 'off',shell_escape INTEGER NOT NULL DEFAULT 0,texlive_bin_path TEXT,
          created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS share_links(
          id TEXT PRIMARY KEY,token_hash TEXT UNIQUE NOT NULL,token_ciphertext TEXT,project_id TEXT NOT NULL,
          snapshot_id TEXT NOT NULL,password_hash TEXT NOT NULL,password_ciphertext TEXT,permission TEXT NOT NULL,
          status TEXT NOT NULL,created_by_user_id TEXT NOT NULL,created_at INTEGER NOT NULL,last_used_at INTEGER
        )`,
		`CREATE TABLE IF NOT EXISTS export_records(
          id TEXT PRIMARY KEY,project_id TEXT NOT NULL,format TEXT NOT NULL,file_path TEXT NOT NULL,
          created_by_user_id TEXT NOT NULL,created_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS pending_project_creations(
          id TEXT PRIMARY KEY,user_id TEXT NOT NULL,payload_json TEXT NOT NULL,workspace_path TEXT NOT NULL,
          created_at INTEGER NOT NULL
        )`,
		`CREATE TABLE IF NOT EXISTS project_collaborators(
          project_id TEXT NOT NULL,user_id TEXT NOT NULL,permission TEXT NOT NULL DEFAULT 'review',
          created_by_user_id TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,
          PRIMARY KEY(project_id,user_id)
        )`,
		`CREATE INDEX IF NOT EXISTS idx_collaborators_user ON project_collaborators(user_id,project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_collaborators_project_permission ON project_collaborators(project_id,permission)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_owner_updated ON projects(created_by_user_id,updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_updated ON projects(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_project_version ON pdf_snapshots(project_id,version_number DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_snapshots_project_hash ON pdf_snapshots(project_id,sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_snapshot_active_created ON review_comments(snapshot_id,deleted_at,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_project_active_created ON review_comments(project_id,deleted_at,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_author_active ON review_comments(author_id,deleted_at)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_deleted ON review_comments(deleted_at)`,
		`CREATE INDEX IF NOT EXISTS idx_replies_comment_active_created ON comment_replies(comment_id,deleted_at,created_at ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_project_created ON audit_events(project_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_share_project_status ON share_links(project_id,status,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_share_status ON share_links(status)`,
		`CREATE INDEX IF NOT EXISTS idx_export_project_created ON export_records(project_id,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_pending_created ON pending_project_creations(created_at)`,
	}
	for _, statement := range statements {
		if _, err := store.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}
	}
	for _, addition := range []struct {
		table      string
		column     string
		definition string
	}{
		{"audit_events", "action", "TEXT NOT NULL DEFAULT ''"},
		{"audit_events", "level", "TEXT NOT NULL DEFAULT 'info'"},
		{"users", "is_active", "INTEGER NOT NULL DEFAULT 1"},
		{"users", "project_limit", "INTEGER"},
		{"projects", "source_type", "TEXT NOT NULL DEFAULT 'local'"},
		{"projects", "source_uri", "TEXT"},
		{"projects", "source_branch", "TEXT"},
		{"projects", "workspace_path", "TEXT"},
		{"projects", "source_credential_ciphertext", "TEXT"},
		{"pdf_snapshots", "snapshot_label", "TEXT"},
		{"pdf_snapshots", "source_patch", "TEXT"},
		{"pdf_snapshots", "source_untracked_archive_path", "TEXT"},
		{"pdf_snapshots", "source_kind", "TEXT NOT NULL DEFAULT 'project'"},
		{"project_tex_settings", "tex_root_path", "TEXT"},
		{"project_tex_settings", "profile_id", "TEXT"},
		{"share_links", "token_ciphertext", "TEXT"},
		{"share_links", "password_ciphertext", "TEXT"},
	} {
		if err := store.ensureColumn(ctx, addition.table, addition.column, addition.definition); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_audit_level_created ON audit_events(level,created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_project_tex_profile ON project_tex_settings(profile_id)`,
		`CREATE INDEX IF NOT EXISTS idx_users_active_created ON users(is_active,created_at DESC)`,
	} {
		if _, err := store.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply post-migration index: %w", err)
		}
	}
	_, err := store.database.ExecContext(ctx, "PRAGMA user_version = 12")
	return err
}

func (store *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := store.database.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = store.database.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

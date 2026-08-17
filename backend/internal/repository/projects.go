package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type NewProject struct {
	ID                         string
	Name                       string
	Slug                       string
	Description                string
	RepositoryPath             string
	SourcePDFPath              string
	SourceType                 string
	SourceURI                  string
	SourceBranch               string
	WorkspacePath              string
	SourceCredentialCiphertext string
	CreatedByUserID            string
}

type ProjectUpdate struct {
	ID                         string
	Name                       string
	Slug                       string
	Description                *string
	RepositoryPath             string
	SourcePDFPath              string
	SourceCredentialCiphertext string
}

func (store *Store) UpdateProject(ctx context.Context, input ProjectUpdate) error {
	_, err := store.database.ExecContext(ctx,
		`UPDATE projects SET name=?,slug=?,description=?,repository_path=?,source_pdf_path=?,
		 source_credential_ciphertext=?,updated_at=? WHERE id=?`,
		input.Name, input.Slug, input.Description, input.RepositoryPath, input.SourcePDFPath,
		nullableText(input.SourceCredentialCiphertext), time.Now().UnixMilli(), input.ID,
	)
	return err
}

func (store *Store) DeleteProject(ctx context.Context, projectID string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM comment_replies WHERE comment_id IN (SELECT id FROM review_comments WHERE project_id=?)", []any{projectID}},
		{"DELETE FROM comment_version_links WHERE source_snapshot_id IN (SELECT id FROM pdf_snapshots WHERE project_id=?) OR target_snapshot_id IN (SELECT id FROM pdf_snapshots WHERE project_id=?)", []any{projectID, projectID}},
		{"DELETE FROM review_comments WHERE project_id=?", []any{projectID}},
		{"DELETE FROM export_records WHERE project_id=?", []any{projectID}},
		{"DELETE FROM users WHERE id IN (SELECT 'share-' || id FROM share_links WHERE project_id=?)", []any{projectID}},
		{"DELETE FROM share_links WHERE project_id=?", []any{projectID}},
		{"DELETE FROM pdf_snapshots WHERE project_id=?", []any{projectID}},
		{"DELETE FROM project_git_settings WHERE project_id=?", []any{projectID}},
		{"DELETE FROM project_tex_settings WHERE project_id=?", []any{projectID}},
		{"DELETE FROM project_collaborators WHERE project_id=?", []any{projectID}},
		{"DELETE FROM projects WHERE id=?", []any{projectID}},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (store *Store) CreateProject(ctx context.Context, input NewProject) error {
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "local"
	}
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO projects(
          id,name,slug,description,repository_path,source_pdf_path,source_type,
          source_uri,source_branch,workspace_path,source_credential_ciphertext,created_by_user_id,created_at,updated_at
        ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.ID, input.Name, input.Slug, input.Description, input.RepositoryPath, input.SourcePDFPath,
		sourceType, input.SourceURI, input.SourceBranch, input.WorkspacePath, nullableText(input.SourceCredentialCiphertext), input.CreatedByUserID, now, now,
	)
	return err
}

func (store *Store) ListProjects(ctx context.Context, user domain.User) ([]map[string]any, error) {
	var rows *sql.Rows
	var err error
	if user.Role == "admin" {
		rows, err = store.database.QueryContext(ctx,
			"SELECT projects.*,'owner' AS collaborator_permission FROM projects ORDER BY updated_at DESC",
		)
	} else {
		rows, err = store.database.QueryContext(ctx,
			`SELECT projects.*,
              CASE WHEN projects.created_by_user_id=? THEN 'owner'
                   ELSE project_collaborators.permission END AS collaborator_permission
       FROM projects
       LEFT JOIN project_collaborators
         ON project_collaborators.project_id=projects.id
        AND project_collaborators.user_id=?
       WHERE projects.created_by_user_id=? OR project_collaborators.user_id=?
       ORDER BY projects.updated_at DESC`,
			user.ID, user.ID, user.ID, user.ID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects, err := mapsFromRows(rows)
	if err != nil {
		return nil, err
	}
	for index := range projects {
		projects[index] = publicProject(projects[index])
	}
	return projects, rows.Err()
}

func (store *Store) ProjectByID(ctx context.Context, projectID string) (map[string]any, error) {
	rows, err := store.database.QueryContext(ctx, "SELECT * FROM projects WHERE id=?", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects, err := mapsFromRows(rows)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, domain.NewAPIError(404, "NOT_FOUND", "项目不存在")
	}
	return projects[0], nil
}

func (store *Store) ProjectAccess(ctx context.Context, projectID string, user domain.User, required string) (domain.ProjectAccess, error) {
	project, err := store.ProjectByID(ctx, projectID)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	ownerID, _ := project["created_by_user_id"].(string)
	level := ""
	if user.Role == "admin" {
		level = "admin"
	} else if ownerID == user.ID {
		level = "owner"
	} else {
		var permission string
		err := store.database.QueryRowContext(ctx,
			"SELECT permission FROM project_collaborators WHERE project_id=? AND user_id=?", projectID, user.ID,
		).Scan(&permission)
		if err == nil && (permission == "review" || permission == "manage") {
			level = permission
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return domain.ProjectAccess{}, err
		}
	}
	rank := map[string]int{"review": 1, "manage": 2, "owner": 3, "admin": 4}
	needed := map[string]int{"view": 1, "review": 1, "manage": 2, "owner": 3}
	if level == "" || rank[level] < needed[required] {
		return domain.ProjectAccess{}, domain.NewAPIError(403, "FORBIDDEN", "无权访问此项目")
	}
	return domain.ProjectAccess{
		ProjectID: projectID,
		OwnerID:   ownerID,
		Level:     level,
		CanReview: rank[level] >= rank["review"],
		CanManage: rank[level] >= rank["manage"],
		IsOwner:   level == "owner" || level == "admin",
	}, nil
}

func (store *Store) PublicProject(ctx context.Context, projectID string, user domain.User) (map[string]any, error) {
	project, err := store.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	access, err := store.ProjectAccess(ctx, projectID, user, "view")
	if err != nil {
		return nil, err
	}
	result := publicProject(project)
	result["access"] = access.Client()
	return result, nil
}

func (store *Store) OwnerSummary(ctx context.Context, projectID string) (map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT users.id,users.email,users.display_name,users.role
         FROM projects JOIN users ON users.id=projects.created_by_user_id
         WHERE projects.id=?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := mapsFromRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, domain.NewAPIError(404, "NOT_FOUND", "项目不存在")
	}
	return items[0], nil
}

func (store *Store) ListCollaborators(ctx context.Context, projectID string) ([]map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT project_collaborators.user_id,project_collaborators.permission,
                project_collaborators.created_at,project_collaborators.updated_at,
                users.email,users.display_name,users.role,users.is_active
         FROM project_collaborators
         JOIN users ON users.id=project_collaborators.user_id
         WHERE project_collaborators.project_id=?
         ORDER BY users.display_name COLLATE NOCASE,users.email COLLATE NOCASE`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapsFromRows(rows)
}

func (store *Store) CollaboratorTargetByEmail(ctx context.Context, email string) (domain.User, bool, error) {
	var user domain.User
	var projectLimit sql.NullInt64
	err := store.database.QueryRowContext(ctx,
		`SELECT id,email,display_name,password_hash,role,is_active,project_limit
         FROM users WHERE LOWER(email)=LOWER(?)`, email,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &user.IsActive, &projectLimit)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, err
	}
	if projectLimit.Valid {
		user.ProjectLimit = &projectLimit.Int64
	}
	return user, true, nil
}

func (store *Store) UpsertCollaborator(ctx context.Context, projectID, userID, permission, actorID string) error {
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO project_collaborators(
          project_id,user_id,permission,created_by_user_id,created_at,updated_at
        ) VALUES(?,?,?,?,?,?)
        ON CONFLICT(project_id,user_id) DO UPDATE SET
          permission=excluded.permission,updated_at=excluded.updated_at`,
		projectID, userID, permission, actorID, now, now,
	)
	return err
}

func (store *Store) CollaboratorByID(ctx context.Context, projectID, userID string) (map[string]any, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT project_collaborators.user_id,project_collaborators.permission,users.email
         FROM project_collaborators
         JOIN users ON users.id=project_collaborators.user_id
         WHERE project_collaborators.project_id=? AND project_collaborators.user_id=?`, projectID, userID)
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

func (store *Store) DeleteCollaborator(ctx context.Context, projectID, userID string) error {
	_, err := store.database.ExecContext(ctx,
		"DELETE FROM project_collaborators WHERE project_id=? AND user_id=?", projectID, userID)
	return err
}

func publicProject(project map[string]any) map[string]any {
	result := make(map[string]any, len(project)+4)
	for key, value := range project {
		switch key {
		case "source_credential_ciphertext", "repository_path", "source_pdf_path", "workspace_path":
			continue
		default:
			result[key] = value
		}
	}
	repositoryPath, _ := project["repository_path"].(string)
	pdfPath, _ := project["source_pdf_path"].(string)
	result["has_source_credential"] = project["source_credential_ciphertext"] != nil
	result["repository_configured"] = repositoryPath != ""
	result["pdf_configured"] = pdfPath != ""
	if pdfPath == "" {
		result["source_file_name"] = nil
	} else {
		result["source_file_name"] = filepath.Base(pdfPath)
	}
	if permission, exists := project["collaborator_permission"]; exists {
		value, _ := permission.(string)
		result["access"] = map[string]any{
			"collaborator_permission": value,
			"can_review":              value != "",
			"can_manage":              value == "manage" || value == "owner",
			"is_owner":                value == "owner",
		}
	}
	return result
}

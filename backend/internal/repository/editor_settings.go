package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type GitSettings struct {
	ProjectID             string
	UserName              string
	UserEmail             string
	AccessTokenCiphertext string
	SSHKeyCiphertext      string
}

type TexSettings struct {
	ProjectID       string
	Engine          string
	BuildTool       string
	OutputDirectory string
	AutoBuild       string
	PDFPreview      string
	SyncTex         bool
	ShellEscape     bool
	TexLiveBinPath  string
	TexRootPath     string
	ProfileID       string
}

type TexLiveProfile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Engine          string `json:"engine"`
	BuildTool       string `json:"buildTool"`
	OutputDirectory string `json:"outputDirectory"`
	ShellEscape     bool   `json:"shellEscape"`
	TexLiveBinPath  string `json:"-"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

type UserVscodeDefaults struct {
	UserID                string
	UserName              string
	UserEmail             string
	AccessTokenCiphertext string
	SSHKeyCiphertext      string
	Engine                string
	BuildTool             string
	OutputDirectory       string
	AutoBuild             string
	ShellEscape           bool
	TexLiveBinPath        string
}

func (store *Store) ProjectGitSettings(ctx context.Context, projectID string) (GitSettings, error) {
	var item GitSettings
	var userName, userEmail, accessToken, sshKey sql.NullString
	err := store.database.QueryRowContext(ctx,
		`SELECT project_id,user_name,user_email,access_token_ciphertext,ssh_private_key_ciphertext
		 FROM project_git_settings WHERE project_id=?`, projectID,
	).Scan(&item.ProjectID, &userName, &userEmail, &accessToken, &sshKey)
	if errors.Is(err, sql.ErrNoRows) {
		return GitSettings{ProjectID: projectID}, nil
	}
	if err != nil {
		return GitSettings{}, err
	}
	item.UserName, item.UserEmail = userName.String, userEmail.String
	item.AccessTokenCiphertext, item.SSHKeyCiphertext = accessToken.String, sshKey.String
	return item, nil
}

func (store *Store) SaveProjectGitSettings(ctx context.Context, item GitSettings) error {
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO project_git_settings(
		  project_id,user_name,user_email,access_token_ciphertext,ssh_private_key_ciphertext,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET
		  user_name=excluded.user_name,user_email=excluded.user_email,
		  access_token_ciphertext=excluded.access_token_ciphertext,
		  ssh_private_key_ciphertext=excluded.ssh_private_key_ciphertext,
		  updated_at=excluded.updated_at`,
		item.ProjectID, nullableText(item.UserName), nullableText(item.UserEmail),
		nullableText(item.AccessTokenCiphertext), nullableText(item.SSHKeyCiphertext), now, now,
	)
	return err
}

func (store *Store) ProjectTexSettings(ctx context.Context, projectID string) (TexSettings, error) {
	item := TexSettings{ProjectID: projectID, Engine: "pdflatex", BuildTool: "latexmk", OutputDirectory: "build", AutoBuild: "off", PDFPreview: "review-hub", SyncTex: true}
	var syncTex, shellEscape int
	var outputDirectory, autoBuild, pdfPreview, binPath, rootPath, profileID sql.NullString
	err := store.database.QueryRowContext(ctx,
		`SELECT project_id,engine,build_tool,output_directory,auto_build,pdf_preview,sync_tex,shell_escape,
		        texlive_bin_path,tex_root_path,profile_id
		 FROM project_tex_settings WHERE project_id=?`, projectID,
	).Scan(&item.ProjectID, &item.Engine, &item.BuildTool, &outputDirectory, &autoBuild, &pdfPreview, &syncTex, &shellEscape, &binPath, &rootPath, &profileID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return TexSettings{}, err
	}
	if outputDirectory.Valid {
		item.OutputDirectory = outputDirectory.String
	}
	if autoBuild.Valid {
		item.AutoBuild = autoBuild.String
	}
	if pdfPreview.Valid {
		item.PDFPreview = pdfPreview.String
	}
	item.SyncTex, item.ShellEscape = syncTex != 0, shellEscape != 0
	item.TexLiveBinPath, item.TexRootPath, item.ProfileID = binPath.String, rootPath.String, profileID.String
	return item, nil
}

func (store *Store) SaveProjectTexSettings(ctx context.Context, item TexSettings) error {
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO project_tex_settings(
		 project_id,engine,build_tool,output_directory,auto_build,pdf_preview,sync_tex,shell_escape,
		 texlive_bin_path,tex_root_path,profile_id,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET
		 engine=excluded.engine,build_tool=excluded.build_tool,output_directory=excluded.output_directory,
		 auto_build=excluded.auto_build,pdf_preview=excluded.pdf_preview,sync_tex=excluded.sync_tex,
		 shell_escape=excluded.shell_escape,texlive_bin_path=excluded.texlive_bin_path,
		 tex_root_path=excluded.tex_root_path,profile_id=excluded.profile_id,updated_at=excluded.updated_at`,
		item.ProjectID, item.Engine, item.BuildTool, item.OutputDirectory, item.AutoBuild, item.PDFPreview,
		boolInt(item.SyncTex), boolInt(item.ShellEscape), nullableText(item.TexLiveBinPath),
		nullableText(item.TexRootPath), nullableText(item.ProfileID), now, now,
	)
	return err
}

func (store *Store) ListTexLiveProfiles(ctx context.Context) ([]TexLiveProfile, error) {
	rows, err := store.database.QueryContext(ctx,
		`SELECT id,name,engine,build_tool,output_directory,shell_escape,texlive_bin_path,created_at,updated_at
		 FROM texlive_profiles ORDER BY name COLLATE NOCASE,created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]TexLiveProfile, 0)
	for rows.Next() {
		var item TexLiveProfile
		var shellEscape int
		var path sql.NullString
		if err := rows.Scan(&item.ID, &item.Name, &item.Engine, &item.BuildTool, &item.OutputDirectory, &shellEscape, &path, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ShellEscape, item.TexLiveBinPath = shellEscape != 0, path.String
		profiles = append(profiles, item)
	}
	return profiles, rows.Err()
}

func (store *Store) TexLiveProfile(ctx context.Context, id string) (*TexLiveProfile, error) {
	var item TexLiveProfile
	var shellEscape int
	var path sql.NullString
	err := store.database.QueryRowContext(ctx,
		`SELECT id,name,engine,build_tool,output_directory,shell_escape,texlive_bin_path,created_at,updated_at
		 FROM texlive_profiles WHERE id=?`, strings.TrimSpace(id),
	).Scan(&item.ID, &item.Name, &item.Engine, &item.BuildTool, &item.OutputDirectory, &shellEscape, &path, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.ShellEscape, item.TexLiveBinPath = shellEscape != 0, path.String
	return &item, nil
}

func (store *Store) SaveTexLiveProfile(ctx context.Context, item TexLiveProfile) error {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO texlive_profiles(id,name,engine,build_tool,output_directory,shell_escape,texlive_bin_path,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name,engine=excluded.engine,build_tool=excluded.build_tool,
		 output_directory=excluded.output_directory,shell_escape=excluded.shell_escape,texlive_bin_path=excluded.texlive_bin_path,
		 updated_at=excluded.updated_at`,
		item.ID, item.Name, item.Engine, item.BuildTool, item.OutputDirectory, boolInt(item.ShellEscape), nullableText(item.TexLiveBinPath), now, now,
	)
	return err
}

func (store *Store) DeleteTexLiveProfile(ctx context.Context, id string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "UPDATE project_tex_settings SET profile_id=NULL WHERE profile_id=?", id); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM texlive_profiles WHERE id=?", id); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) UserVscodeDefaults(ctx context.Context, userID string) (UserVscodeDefaults, error) {
	item := UserVscodeDefaults{UserID: userID, Engine: "pdflatex", BuildTool: "latexmk", OutputDirectory: "build", AutoBuild: "off"}
	var name, email, token, key, output, auto, bin sql.NullString
	var shellEscape int
	err := store.database.QueryRowContext(ctx,
		`SELECT user_id,user_name,user_email,access_token_ciphertext,ssh_private_key_ciphertext,
		        engine,build_tool,output_directory,auto_build,shell_escape,texlive_bin_path
		 FROM user_vscode_defaults WHERE user_id=?`, userID,
	).Scan(&item.UserID, &name, &email, &token, &key, &item.Engine, &item.BuildTool, &output, &auto, &shellEscape, &bin)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return UserVscodeDefaults{}, err
	}
	item.UserName, item.UserEmail = name.String, email.String
	item.AccessTokenCiphertext, item.SSHKeyCiphertext = token.String, key.String
	if output.Valid {
		item.OutputDirectory = output.String
	}
	if auto.Valid {
		item.AutoBuild = auto.String
	}
	item.ShellEscape, item.TexLiveBinPath = shellEscape != 0, bin.String
	return item, nil
}

func (store *Store) SaveUserVscodeDefaults(ctx context.Context, item UserVscodeDefaults) error {
	now := time.Now().UnixMilli()
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO user_vscode_defaults(
		 user_id,user_name,user_email,access_token_ciphertext,ssh_private_key_ciphertext,engine,build_tool,
		 output_directory,auto_build,shell_escape,texlive_bin_path,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET user_name=excluded.user_name,user_email=excluded.user_email,
		access_token_ciphertext=excluded.access_token_ciphertext,ssh_private_key_ciphertext=excluded.ssh_private_key_ciphertext,
		engine=excluded.engine,build_tool=excluded.build_tool,output_directory=excluded.output_directory,
		auto_build=excluded.auto_build,shell_escape=excluded.shell_escape,texlive_bin_path=excluded.texlive_bin_path,
		updated_at=excluded.updated_at`,
		item.UserID, nullableText(item.UserName), nullableText(item.UserEmail), nullableText(item.AccessTokenCiphertext), nullableText(item.SSHKeyCiphertext),
		item.Engine, item.BuildTool, item.OutputDirectory, item.AutoBuild, boolInt(item.ShellEscape), nullableText(item.TexLiveBinPath), now, now,
	)
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

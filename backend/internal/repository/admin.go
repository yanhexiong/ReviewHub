package repository

import (
	"context"
	"encoding/json"
)

type AdminOverview struct {
	RegistrationOpen    bool             `json:"registrationOpen"`
	Settings            ResourceSettings `json:"settings"`
	Listener            ListenerSettings `json:"listener"`
	ConfiguredRootCount int              `json:"configuredRootCount"`
	Metrics             map[string]int64 `json:"metrics"`
	Users               []map[string]any `json:"users"`
	Projects            []map[string]any `json:"projects"`
	Shares              []map[string]any `json:"shares"`
	Audit               []map[string]any `json:"audit"`
}

type ListenerSettings struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func (store *Store) AdminOverview(ctx context.Context) (AdminOverview, error) {
	registrationOpen, err := store.RegistrationOpen(ctx)
	if err != nil {
		return AdminOverview{}, err
	}
	settings, err := store.ResourceSettings(ctx)
	if err != nil {
		return AdminOverview{}, err
	}
	roots, err := store.AllowedRoots(ctx)
	if err != nil {
		return AdminOverview{}, err
	}
	listener, err := store.ListenerSettings(ctx)
	if err != nil {
		return AdminOverview{}, err
	}
	metrics := map[string]int64{}
	for key, query := range map[string]string{
		"users":        "SELECT COUNT(*) FROM users WHERE email NOT LIKE 'share-%@local.invalid'",
		"projects":     "SELECT COUNT(*) FROM projects",
		"snapshots":    "SELECT COUNT(*) FROM pdf_snapshots",
		"comments":     "SELECT COUNT(*) FROM review_comments WHERE deleted_at IS NULL",
		"activeShares": "SELECT COUNT(*) FROM share_links WHERE status='active'",
	} {
		var count int64
		if err := store.database.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return AdminOverview{}, err
		}
		metrics[key] = count
	}
	users, err := store.queryMaps(ctx,
		`SELECT users.id,users.email,users.display_name,users.role,
                users.is_active,users.project_limit,users.created_at,
                users.updated_at,users.last_login_at,
                (SELECT COUNT(*) FROM projects WHERE projects.created_by_user_id=users.id) AS project_count
         FROM users
         WHERE users.email NOT LIKE 'share-%@local.invalid'
         ORDER BY users.created_at`)
	if err != nil {
		return AdminOverview{}, err
	}
	projects, err := store.queryMaps(ctx,
		`SELECT projects.id,projects.name,projects.slug,projects.created_at,
                users.email AS owner_email,
                (SELECT COUNT(*) FROM pdf_snapshots WHERE project_id=projects.id) AS snapshot_count,
                (SELECT COUNT(*) FROM review_comments WHERE project_id=projects.id AND deleted_at IS NULL) AS comment_count,
                (SELECT COUNT(*) FROM share_links WHERE project_id=projects.id AND status='active') AS active_share_count
         FROM projects JOIN users ON users.id=projects.created_by_user_id
         ORDER BY projects.updated_at DESC`)
	if err != nil {
		return AdminOverview{}, err
	}
	shares, err := store.queryMaps(ctx,
		`SELECT share_links.id,share_links.project_id,share_links.snapshot_id,
                share_links.permission,share_links.status,share_links.created_at,
                share_links.last_used_at,projects.name AS project_name,
                pdf_snapshots.version_number,pdf_snapshots.snapshot_label
         FROM share_links
         JOIN projects ON projects.id=share_links.project_id
         JOIN pdf_snapshots ON pdf_snapshots.id=share_links.snapshot_id
         ORDER BY share_links.created_at DESC`)
	if err != nil {
		return AdminOverview{}, err
	}
	audit, err := store.queryMaps(ctx,
		`SELECT audit_events.id,audit_events.entity_type,audit_events.entity_id,
                audit_events.action,audit_events.created_at,
                users.display_name AS actor_name,users.email AS actor_email
         FROM audit_events LEFT JOIN users ON users.id=audit_events.actor_user_id
         ORDER BY audit_events.created_at DESC LIMIT 50`)
	if err != nil {
		return AdminOverview{}, err
	}
	return AdminOverview{
		RegistrationOpen: registrationOpen, Settings: settings, Listener: listener,
		ConfiguredRootCount: len(roots), Metrics: metrics, Users: users,
		Projects: projects, Shares: shares, Audit: audit,
	}, nil
}

func (store *Store) AdminShares(ctx context.Context) ([]map[string]any, error) {
	return store.queryMaps(ctx,
		`SELECT share_links.id,share_links.project_id,share_links.snapshot_id,
		        share_links.permission,share_links.status,share_links.created_at,
		        share_links.last_used_at,projects.name AS project_name,
		        pdf_snapshots.version_number,pdf_snapshots.snapshot_label
		 FROM share_links JOIN projects ON projects.id=share_links.project_id
		 JOIN pdf_snapshots ON pdf_snapshots.id=share_links.snapshot_id
		 ORDER BY share_links.created_at DESC`)
}

func (store *Store) ListenerSettings(ctx context.Context) (ListenerSettings, error) {
	settings := ListenerSettings{Host: "0.0.0.0", Port: 3000}
	var raw string
	err := store.database.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key='listener_settings'").Scan(&raw)
	if err != nil {
		if isNoRows(err) {
			return settings, nil
		}
		return ListenerSettings{}, err
	}
	var saved ListenerSettings
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return settings, nil
	}
	if saved.Host != "" {
		settings.Host = saved.Host
	}
	if saved.Port > 0 && saved.Port <= 65_535 {
		settings.Port = saved.Port
	}
	return settings, nil
}

func (store *Store) queryMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := store.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapsFromRows(rows)
}

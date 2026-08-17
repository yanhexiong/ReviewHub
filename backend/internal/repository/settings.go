package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

type ResourceSettings struct {
	MaxPDFBytes        int `json:"maxPdfBytes"`
	MaxImportBytes     int `json:"maxImportBytes"`
	MaxProjectsPerUser int `json:"maxProjectsPerUser"`
	MaxUsers           int `json:"maxUsers"`
}

func (store *Store) ResourceSettings(ctx context.Context) (ResourceSettings, error) {
	settings := ResourceSettings{}
	var err error
	if settings.MaxPDFBytes, err = store.settingInteger(ctx, "max_pdf_bytes"); err != nil {
		return ResourceSettings{}, err
	}
	if settings.MaxImportBytes, err = store.settingInteger(ctx, "max_import_bytes"); err != nil {
		return ResourceSettings{}, err
	}
	if settings.MaxProjectsPerUser, err = store.settingInteger(ctx, "max_projects_per_user"); err != nil {
		return ResourceSettings{}, err
	}
	if settings.MaxUsers, err = store.settingInteger(ctx, "max_users"); err != nil {
		return ResourceSettings{}, err
	}
	return settings, nil
}

func (store *Store) UpdateResourceSettings(ctx context.Context, settings ResourceSettings) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(ctx,
		"INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at",
	)
	if err != nil {
		return err
	}
	defer statement.Close()
	values := map[string]int{
		"max_pdf_bytes":         settings.MaxPDFBytes,
		"max_import_bytes":      settings.MaxImportBytes,
		"max_projects_per_user": settings.MaxProjectsPerUser,
		"max_users":             settings.MaxUsers,
	}
	for key, value := range values {
		if _, err := statement.ExecContext(ctx, key, strconv.Itoa(value), time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (store *Store) SetRegistrationOpen(ctx context.Context, open bool) error {
	value := "false"
	if open {
		value = "true"
	}
	_, err := store.database.ExecContext(ctx,
		"INSERT INTO app_settings(key,value,updated_at) VALUES('registration_open',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at",
		value, time.Now().UnixMilli(),
	)
	return err
}

func (store *Store) AllowedRoots(ctx context.Context) ([]string, error) {
	var value string
	err := store.database.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key='allowed_roots'").Scan(&value)
	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var roots []string
	if err := json.Unmarshal([]byte(value), &roots); err != nil {
		return []string{}, nil
	}
	return roots, nil
}

func (store *Store) SaveAllowedRoots(ctx context.Context, roots []string) error {
	value, err := json.Marshal(roots)
	if err != nil {
		return err
	}
	_, err = store.database.ExecContext(ctx,
		"INSERT INTO app_settings(key,value,updated_at) VALUES('allowed_roots',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at",
		string(value), time.Now().UnixMilli(),
	)
	return err
}

func (store *Store) settingInteger(ctx context.Context, key string) (int, error) {
	var value string
	if err := store.database.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key=?", key).Scan(&value); err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

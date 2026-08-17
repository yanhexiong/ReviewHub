package repository

import (
	"context"
	"strconv"
	"time"
)

type AuditSettings struct {
	RetentionDays int `json:"retentionDays"`
	MaxEntryBytes int `json:"maxEntryBytes"`
}

func (store *Store) AuditSettings(ctx context.Context) (AuditSettings, error) {
	settings := AuditSettings{RetentionDays: 365, MaxEntryBytes: 16_384}
	for key, target := range map[string]*int{"audit_retention_days": &settings.RetentionDays, "audit_max_entry_bytes": &settings.MaxEntryBytes} {
		var value string
		err := store.database.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key=?", key).Scan(&value)
		if err != nil {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil {
			*target = parsed
		}
	}
	if settings.RetentionDays < 1 || settings.RetentionDays > 3650 {
		settings.RetentionDays = 365
	}
	if settings.MaxEntryBytes < 256 || settings.MaxEntryBytes > 1_048_576 {
		settings.MaxEntryBytes = 16_384
	}
	return settings, nil
}

func (store *Store) SaveAuditSettings(ctx context.Context, settings AuditSettings) error {
	now := time.Now().UnixMilli()
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for key, value := range map[string]int{"audit_retention_days": settings.RetentionDays, "audit_max_entry_bytes": settings.MaxEntryBytes} {
		if _, err := transaction.ExecContext(ctx, "INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at", key, strconv.Itoa(value), now); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (store *Store) PurgeAuditEvents(ctx context.Context, retentionDays int) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()
	result, err := store.database.ExecContext(ctx, "DELETE FROM audit_events WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return count, err
}

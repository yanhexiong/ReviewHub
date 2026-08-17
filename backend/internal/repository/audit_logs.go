package repository

import (
	"context"
	"strings"
)

type AuditLogFilter struct {
	From       *int64
	To         *int64
	Level      string
	EntityType string
	Action     string
	Search     string
	Limit      int
	Offset     int
}

type AuditLogResult struct {
	Rows  []map[string]any
	Total int64
}

func (store *Store) AuditLogs(ctx context.Context, filter AuditLogFilter) (AuditLogResult, error) {
	clauses := []string{}
	arguments := []any{}
	if filter.From != nil {
		clauses = append(clauses, "audit_events.created_at >= ?")
		arguments = append(arguments, *filter.From)
	}
	if filter.To != nil {
		clauses = append(clauses, "audit_events.created_at < ?")
		arguments = append(arguments, *filter.To)
	}
	for _, item := range []struct {
		value  string
		column string
	}{
		{filter.Level, "audit_events.level"},
		{filter.EntityType, "audit_events.entity_type"},
		{filter.Action, "audit_events.action"},
	} {
		if item.value != "" {
			clauses = append(clauses, item.column+" = ?")
			arguments = append(arguments, item.value)
		}
	}
	if filter.Search != "" {
		clauses = append(clauses, "(audit_events.entity_id LIKE ? OR audit_events.after_json LIKE ? OR users.email LIKE ? OR users.display_name LIKE ?)")
		pattern := "%" + filter.Search + "%"
		arguments = append(arguments, pattern, pattern, pattern, pattern)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	var total int64
	if err := store.database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_events LEFT JOIN users ON users.id=audit_events.actor_user_id"+where, arguments...).Scan(&total); err != nil {
		return AuditLogResult{}, err
	}
	queryArguments := append(append([]any{}, arguments...), filter.Limit, filter.Offset)
	rows, err := store.queryMaps(ctx,
		`SELECT audit_events.id,audit_events.project_id,audit_events.entity_type,
                audit_events.entity_id,audit_events.action,audit_events.level,
                audit_events.after_json,audit_events.created_at,
                users.display_name AS actor_name,users.email AS actor_email
         FROM audit_events
         LEFT JOIN users ON users.id=audit_events.actor_user_id`+where+`
         ORDER BY audit_events.created_at DESC
         LIMIT ? OFFSET ?`, queryArguments...)
	if err != nil {
		return AuditLogResult{}, err
	}
	return AuditLogResult{Rows: rows, Total: total}, nil
}

func (store *Store) AuditEntityTypes(ctx context.Context) ([]string, error) {
	rows, err := store.database.QueryContext(ctx, "SELECT DISTINCT entity_type FROM audit_events ORDER BY entity_type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

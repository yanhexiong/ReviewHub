package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type ManagedUserUpdate struct {
	DisplayName  string
	Role         string
	IsActive     bool
	ProjectLimit *int64
	PasswordHash string
}

func (store *Store) ManagedUser(ctx context.Context, userID string) (map[string]any, error) {
	rows, err := store.queryMaps(ctx,
		`SELECT users.id,users.email,users.display_name,users.role,users.is_active,users.project_limit,
		        users.created_at,users.updated_at,users.last_login_at,
		        (SELECT COUNT(*) FROM projects WHERE projects.created_by_user_id=users.id) AS project_count
		 FROM users WHERE users.id=? AND users.email NOT LIKE 'share-%@local.invalid'`, userID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.NewAPIError(404, "USER_NOT_FOUND", "账户不存在")
	}
	return rows[0], nil
}

func (store *Store) CreateManagedUser(ctx context.Context, email, displayName, passwordHash, role string, active bool, projectLimit *int64) (map[string]any, error) {
	settings, err := store.ResourceSettings(ctx)
	if err != nil {
		return nil, err
	}
	available, err := store.UserCapacityAvailable(ctx, settings.MaxUsers)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, domain.NewAPIError(403, "USER_LIMIT_REACHED", "账户数量已达到管理员设置的上限")
	}
	now := time.Now().UnixMilli()
	userID := newUserID()
	_, err = store.database.ExecContext(ctx,
		`INSERT INTO users(id,email,display_name,password_hash,role,is_active,project_limit,created_at,updated_at,last_login_at)
		 VALUES(?,?,?,?,?,?,?,?,?,NULL)`, userID, strings.ToLower(strings.TrimSpace(email)), displayName, passwordHash, role, boolInt(active), projectLimit, now, now)
	if err != nil {
		return nil, err
	}
	return store.ManagedUser(ctx, userID)
}

func (store *Store) UpdateManagedUser(ctx context.Context, actorID, userID string, input ManagedUserUpdate) (map[string]any, error) {
	target, err := store.ManagedUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if userID == actorID && (!input.IsActive || input.Role != "admin") {
		return nil, domain.NewAPIError(400, "SELF_LOCKOUT", "不能停用或降级当前登录的管理员账户")
	}
	currentRole, _ := target["role"].(string)
	currentActive := intValue(target["is_active"]) != 0
	if currentRole == "admin" && (input.Role != "admin" || !input.IsActive) {
		var activeAdmins int
		if err := store.database.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role='admin' AND is_active=1 AND email NOT LIKE 'share-%@local.invalid'").Scan(&activeAdmins); err != nil {
			return nil, err
		}
		if activeAdmins <= 1 {
			return nil, domain.NewAPIError(409, "LAST_ADMIN", "系统至少需要保留一个启用的管理员账户")
		}
	}
	query := `UPDATE users SET display_name=?,role=?,is_active=?,project_limit=?,password_hash=COALESCE(NULLIF(?,''),password_hash),updated_at=? WHERE id=?`
	if _, err := store.database.ExecContext(ctx, query, input.DisplayName, input.Role, boolInt(input.IsActive), input.ProjectLimit, input.PasswordHash, time.Now().UnixMilli(), userID); err != nil {
		return nil, err
	}
	_ = currentActive
	return store.ManagedUser(ctx, userID)
}

func newUserID() string { return uuid.NewString() }

func intValue(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case []byte:
		var result int64
		_, _ = fmt.Sscan(string(typed), &result)
		return result
	}
	return 0
}

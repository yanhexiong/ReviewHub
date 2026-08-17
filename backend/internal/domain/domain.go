package domain

import (
	"fmt"
	"net"
	"strings"
)

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewAPIError(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	IsActive     int64  `json:"is_active"`
	ProjectLimit *int64 `json:"project_limit"`
	PasswordHash string `json:"-"`
}

type ProjectAccess struct {
	ProjectID string
	OwnerID   string
	Level     string
	CanReview bool
	CanManage bool
	IsOwner   bool
}

func (access ProjectAccess) Client() map[string]any {
	permission := access.Level
	if permission == "admin" || permission == "owner" {
		permission = "owner"
	}
	return map[string]any{
		"collaborator_permission": permission,
		"can_review":              access.CanReview,
		"can_manage":              access.CanManage,
		"is_owner":                access.IsOwner,
	}
}

// ValidListener accepts the bind addresses supported by the page server:
// literal IPv4/IPv6 addresses, localhost, and ordinary DNS host names.
func ValidListener(host string, port int) bool {
	host = strings.TrimSpace(host)
	if host == "" || port < 1 || port > 65_535 {
		return false
	}
	if host == "localhost" || net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

type ProjectEvent struct {
	Type       string `json:"type"`
	ProjectID  string `json:"projectId"`
	SnapshotID string `json:"snapshotId"`
	CommentID  string `json:"commentId"`
	Revision   int64  `json:"revision"`
	ChangedAt  int64  `json:"changedAt"`
	Comment    any    `json:"comment,omitempty"`
	Reply      any    `json:"reply,omitempty"`
	ReplyCount any    `json:"replyCount,omitempty"`
}

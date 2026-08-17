package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

const sessionCookieName = "review_hub_session"

type AuthService struct {
	store         *repository.Store
	sessionSecret []byte
	production    bool
	maxUsers      int
}

func NewAuthService(store *repository.Store, sessionSecret string, production bool, maxUsers int) *AuthService {
	return &AuthService{
		store:         store,
		sessionSecret: []byte(sessionSecret),
		production:    production,
		maxUsers:      maxUsers,
	}
}

func (service *AuthService) CurrentUser(request *http.Request) (domain.User, error) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return domain.User{}, domain.NewAPIError(401, "AUTH_REQUIRED", "请先登录")
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || parts[0] == "" || !service.validToken(cookie.Value) {
		return domain.User{}, domain.NewAPIError(401, "AUTH_REQUIRED", "会话无效")
	}
	user, err := service.store.UserByID(request.Context(), parts[0])
	if err != nil {
		return domain.User{}, err
	}
	if user.IsActive == 0 {
		return domain.User{}, domain.NewAPIError(401, "AUTH_REQUIRED", "账户已停用或会话无效")
	}
	return user, nil
}

func (service *AuthService) RequireRole(request *http.Request, roles ...string) (domain.User, error) {
	user, err := service.CurrentUser(request)
	if err != nil {
		return domain.User{}, err
	}
	for _, role := range roles {
		if user.Role == role {
			return user, nil
		}
	}
	service.store.Audit(request.Context(), nil, &user.ID, "auth", user.ID, "permission_denied", nil)
	return domain.User{}, domain.NewAPIError(403, "FORBIDDEN", "权限不足")
}

func (service *AuthService) Login(ctx context.Context, email, password string, rememberMe bool) (domain.User, *http.Cookie, error) {
	user, err := service.store.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		if err == nil {
			service.store.Audit(ctx, nil, &user.ID, "auth", user.ID, "login_failed", nil)
		}
		return domain.User{}, nil, domain.NewAPIError(401, "INVALID_CREDENTIALS", "邮箱或密码错误")
	}
	if user.IsActive == 0 {
		service.store.Audit(ctx, nil, &user.ID, "auth", user.ID, "login_blocked_inactive", nil)
		return domain.User{}, nil, domain.NewAPIError(403, "ACCOUNT_DISABLED", "账户已停用，请联系管理员")
	}
	if err := service.store.UpdateLastLogin(ctx, user.ID); err != nil {
		return domain.User{}, nil, err
	}
	service.store.Audit(ctx, nil, &user.ID, "auth", user.ID, "login_success", nil)
	return user, service.sessionCookie(user.ID, rememberMe), nil
}

func (service *AuthService) Register(ctx context.Context, email, password string) (domain.User, *http.Cookie, error) {
	if !validEmail(email) || len(password) < 12 || len(password) > 200 {
		return domain.User{}, nil, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	open, err := service.store.RegistrationOpen(ctx)
	if err != nil {
		return domain.User{}, nil, err
	}
	if !open {
		return domain.User{}, nil, domain.NewAPIError(403, "REGISTRATION_CLOSED", "当前暂未开放注册，请联系管理员")
	}
	available, err := service.store.UserCapacityAvailable(ctx, service.maxUsers)
	if err != nil {
		return domain.User{}, nil, err
	}
	if !available {
		return domain.User{}, nil, domain.NewAPIError(403, "USER_LIMIT_REACHED", "账户数量已达到管理员设置的上限")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	user := domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		DisplayName:  displayNameForEmail(email),
		PasswordHash: string(hash),
		Role:         "reviewer",
		IsActive:     1,
	}
	if err := service.store.CreateUser(ctx, user); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.User{}, nil, domain.NewAPIError(409, "EMAIL_EXISTS", "该邮箱已经注册")
		}
		return domain.User{}, nil, err
	}
	service.store.Audit(ctx, nil, &user.ID, "user", user.ID, "registered", map[string]string{"email": user.Email})
	return service.Login(ctx, email, password, false)
}

func (service *AuthService) Setup(ctx context.Context, email, displayName, password, confirmation, host string, port int) (domain.User, *http.Cookie, error) {
	if !validEmail(email) || strings.TrimSpace(displayName) == "" || len(displayName) > 80 || len(password) < 12 || len(password) > 200 || password != confirmation || !domain.ValidListener(host, port) {
		return domain.User{}, nil, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	required, err := service.store.SetupRequired(ctx)
	if err != nil {
		return domain.User{}, nil, err
	}
	if !required {
		return domain.User{}, nil, domain.NewAPIError(409, "SETUP_COMPLETED", "管理员账户已经存在，请直接登录")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, nil, err
	}
	user := domain.User{
		ID:           uuid.NewString(),
		Email:        strings.ToLower(strings.TrimSpace(email)),
		DisplayName:  strings.TrimSpace(displayName),
		PasswordHash: string(hash),
		Role:         "admin",
		IsActive:     1,
	}
	if err := service.store.CreateUser(ctx, user); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.User{}, nil, domain.NewAPIError(409, "SETUP_COMPLETED", "管理员账户已经存在，请直接登录")
		}
		return domain.User{}, nil, err
	}
	if err := service.store.SaveListenerSettings(ctx, strings.TrimSpace(host), port); err != nil {
		return domain.User{}, nil, err
	}
	service.store.Audit(ctx, nil, &user.ID, "user", user.ID, "initial_admin_created", map[string]string{"email": user.Email})
	return service.Login(ctx, user.Email, password, false)
}

func (service *AuthService) UpdateDisplayName(ctx context.Context, user domain.User, displayName string) (domain.User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 80 {
		return domain.User{}, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	if err := service.store.UpdateDisplayName(ctx, user.ID, displayName); err != nil {
		return domain.User{}, err
	}
	service.store.Audit(ctx, nil, &user.ID, "user", user.ID, "display_name_updated", map[string]string{"before": user.DisplayName, "after": displayName})
	user.DisplayName = displayName
	return user, nil
}

func (service *AuthService) LogoutCookie() *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: service.production, MaxAge: -1, Expires: time.Unix(1, 0)}
}

func (service *AuthService) sessionCookie(userID string, rememberMe bool) *http.Cookie {
	cookie := &http.Cookie{Name: sessionCookieName, Value: service.sign(userID), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: service.production}
	if rememberMe {
		cookie.MaxAge = int((30 * 24 * time.Hour).Seconds())
	}
	return cookie
}

func (service *AuthService) sign(userID string) string {
	mac := hmac.New(sha256.New, service.sessionSecret)
	_, _ = mac.Write([]byte(userID))
	return userID + "." + hex.EncodeToString(mac.Sum(nil))
}

func (service *AuthService) validToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	expected := service.sign(parts[0])
	return len(token) == len(expected) && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func validEmail(value string) bool {
	parsed, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && parsed.Address == strings.TrimSpace(value) && strings.Contains(parsed.Address, "@")
}

func displayNameForEmail(email string) string {
	if local, _, ok := strings.Cut(email, "@"); ok && local != "" {
		return local
	}
	return email
}

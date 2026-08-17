package httpapi

import (
	"net/http"
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

func (api *API) adminRootPatch(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		RegistrationOpen bool `json:"registrationOpen"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if err := api.store.SetRegistrationOpen(request.Context(), input.RegistrationOpen); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "app_setting", "registration_open", "updated", map[string]any{"registrationOpen": input.RegistrationOpen})
	writeJSON(response, http.StatusOK, map[string]bool{"registrationOpen": input.RegistrationOpen})
}

func (api *API) adminRootCreate(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Email, Password, DisplayName, Role string
		IsActive                           *bool  `json:"isActive"`
		ProjectLimit                       *int64 `json:"projectLimit"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.Email, input.DisplayName, input.Role = strings.ToLower(strings.TrimSpace(input.Email)), strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Role)
	if _, err := mail.ParseAddress(input.Email); err != nil || len(input.Email) > 254 || len(input.Password) < 12 || len(input.Password) > 200 || len(input.DisplayName) > 80 || !validManagedRole(input.Role, true) {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	active := true
	if input.IsActive != nil {
		active = *input.IsActive
	}
	if input.DisplayName == "" {
		input.DisplayName = strings.Split(input.Email, "@")[0]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(response, err)
		return
	}
	role := input.Role
	if role == "" {
		role = "admin"
	}
	account, err := api.store.CreateManagedUser(request.Context(), input.Email, input.DisplayName, string(hash), role, active, input.ProjectLimit)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(response, domain.NewAPIError(409, "EMAIL_EXISTS", "该邮箱已经注册"))
		} else {
			writeError(response, err)
		}
		return
	}
	accountID, _ := account["id"].(string)
	api.store.Audit(request.Context(), nil, &actor.ID, "user", accountID, "account_created", map[string]any{"email": input.Email, "role": role, "isActive": active, "projectLimit": input.ProjectLimit})
	writeJSON(response, http.StatusCreated, map[string]any{"user": account})
}

func (api *API) adminUserUpdate(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		DisplayName, Role, NewPassword string
		IsActive                       bool   `json:"isActive"`
		ProjectLimit                   *int64 `json:"projectLimit"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.DisplayName, input.Role, input.NewPassword = strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Role), strings.TrimSpace(input.NewPassword)
	if input.DisplayName == "" || len(input.DisplayName) > 80 || !validManagedRole(input.Role, false) || (input.NewPassword != "" && (len(input.NewPassword) < 12 || len(input.NewPassword) > 200)) {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	hash := ""
	if input.NewPassword != "" {
		encoded, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			writeError(response, err)
			return
		}
		hash = string(encoded)
	}
	account, err := api.store.UpdateManagedUser(request.Context(), actor.ID, request.PathValue("userID"), repository.ManagedUserUpdate{DisplayName: input.DisplayName, Role: input.Role, IsActive: input.IsActive, ProjectLimit: input.ProjectLimit, PasswordHash: hash})
	if err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "user", request.PathValue("userID"), "updated", map[string]any{"displayName": input.DisplayName, "role": input.Role, "isActive": input.IsActive, "projectLimit": input.ProjectLimit, "passwordReset": input.NewPassword != ""})
	writeJSON(response, http.StatusOK, map[string]any{"user": account})
}

func (api *API) adminShares(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.RequireRole(request, "admin"); err != nil {
		writeError(response, err)
		return
	}
	rows, err := api.store.AdminShares(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"shares": rows})
}

func validManagedRole(value string, create bool) bool {
	if create && value == "" {
		return true
	}
	return value == "admin" || value == "author" || value == "reviewer" || value == "viewer"
}

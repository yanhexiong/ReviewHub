package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
	"github.com/yanhexiong/review-hub/backend/internal/secretbox"
)

func (api *API) projectGitSettings(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	project, err := api.store.ProjectByID(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.ProjectGitSettings(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	ownerID, _ := project["created_by_user_id"].(string)
	defaults, err := api.store.UserVscodeDefaults(request.Context(), ownerID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"settings": publicGitSettings(settings, defaults),
		"git":      nil,
	})
}

func publicGitSettings(settings repository.GitSettings, defaults repository.UserVscodeDefaults) map[string]any {
	return map[string]any{
		"userName":             fallbackText(settings.UserName, defaults.UserName),
		"userEmail":            fallbackText(settings.UserEmail, defaults.UserEmail),
		"hasAccessToken":       settings.AccessTokenCiphertext != "",
		"hasSshPrivateKey":     settings.SSHKeyCiphertext != "",
		"projectUserName":      settings.UserName,
		"projectUserEmail":     settings.UserEmail,
		"usingAccountDefaults": settings.UserName == "" && settings.UserEmail == "",
	}
}

func (api *API) saveProjectGitSettings(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	project, err := api.store.ProjectByID(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		UserName           string `json:"userName"`
		UserEmail          string `json:"userEmail"`
		AccessToken        string `json:"accessToken"`
		SSHPrivateKey      string `json:"sshPrivateKey"`
		ClearAccessToken   bool   `json:"clearAccessToken"`
		ClearSSHPrivateKey bool   `json:"clearSshPrivateKey"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.UserName = strings.TrimSpace(strings.ReplaceAll(input.UserName, "\n", " "))
	input.UserEmail = strings.TrimSpace(strings.ReplaceAll(input.UserEmail, "\n", " "))
	input.AccessToken = strings.TrimSpace(input.AccessToken)
	if input.UserName == "" {
		input.UserName = ""
	}
	if len(input.UserName) > 120 || len(input.UserEmail) > 254 || len(input.AccessToken) > 500 || len(input.SSHPrivateKey) > 16000 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	previous, err := api.store.ProjectGitSettings(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	accessToken := previous.AccessTokenCiphertext
	if input.AccessToken != "" {
		accessToken, err = secretbox.Encrypt(api.encryptionKey, input.AccessToken)
		if err != nil {
			writeError(response, err)
			return
		}
	} else if input.ClearAccessToken {
		accessToken = ""
	}
	sshKey := previous.SSHKeyCiphertext
	if input.SSHPrivateKey != "" {
		sshKey, err = secretbox.Encrypt(api.encryptionKey, input.SSHPrivateKey)
		if err != nil {
			writeError(response, err)
			return
		}
	} else if input.ClearSSHPrivateKey {
		sshKey = ""
	}
	if err := api.store.SaveProjectGitSettings(request.Context(), repository.GitSettings{ProjectID: projectID, UserName: input.UserName, UserEmail: input.UserEmail, AccessTokenCiphertext: accessToken, SSHKeyCiphertext: sshKey}); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "git_settings", projectID, "updated", map[string]any{"userName": input.UserName, "userEmail": input.UserEmail, "hasAccessToken": accessToken != "", "hasSshPrivateKey": sshKey != ""})
	ownerID, _ := project["created_by_user_id"].(string)
	defaults, err := api.store.UserVscodeDefaults(request.Context(), ownerID)
	if err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.ProjectGitSettings(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"settings": publicGitSettings(settings, defaults)})
}

func fallbackText(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func (api *API) userVscodeDefaults(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	defaults, err := api.store.UserVscodeDefaults(request.Context(), user.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"defaults": publicUserDefaults(defaults)})
}

func (api *API) saveUserVscodeDefaults(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		UserName            string `json:"userName"`
		UserEmail           string `json:"userEmail"`
		AccessToken         string `json:"accessToken"`
		SSHPrivateKey       string `json:"sshPrivateKey"`
		ClearAccessToken    bool   `json:"clearAccessToken"`
		ClearSSHPrivateKey  bool   `json:"clearSshPrivateKey"`
		Engine              string `json:"engine"`
		BuildTool           string `json:"buildTool"`
		OutputDirectory     string `json:"outputDirectory"`
		AutoBuild           string `json:"autoBuild"`
		ShellEscape         bool   `json:"shellEscape"`
		ClearTexLiveBinPath bool   `json:"clearTexliveBinPath"`
		TexLiveBinPath      string `json:"texliveBinPath"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	input.UserName = strings.TrimSpace(strings.ReplaceAll(input.UserName, "\n", " "))
	input.UserEmail = strings.TrimSpace(strings.ReplaceAll(input.UserEmail, "\n", " "))
	input.AccessToken = strings.TrimSpace(input.AccessToken)
	input.OutputDirectory = strings.TrimSpace(input.OutputDirectory)
	input.TexLiveBinPath = strings.TrimSpace(input.TexLiveBinPath)
	if input.Engine == "" {
		input.Engine = "pdflatex"
	}
	if input.BuildTool == "" {
		input.BuildTool = "latexmk"
	}
	if input.OutputDirectory == "" {
		input.OutputDirectory = "build"
	}
	if input.AutoBuild == "" {
		input.AutoBuild = "off"
	}
	if !validRelativeEditorPath(input.OutputDirectory) || len(input.UserName) > 120 || len(input.UserEmail) > 254 || len(input.AccessToken) > 500 || len(input.SSHPrivateKey) > 16000 || (input.TexLiveBinPath != "" && !strings.HasPrefix(input.TexLiveBinPath, "/")) {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	previous, err := api.store.UserVscodeDefaults(request.Context(), user.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	accessToken := previous.AccessTokenCiphertext
	if input.AccessToken != "" {
		accessToken, err = secretbox.Encrypt(api.encryptionKey, input.AccessToken)
		if err != nil {
			writeError(response, err)
			return
		}
	} else if input.ClearAccessToken {
		accessToken = ""
	}
	sshKey := previous.SSHKeyCiphertext
	if input.SSHPrivateKey != "" {
		sshKey, err = secretbox.Encrypt(api.encryptionKey, input.SSHPrivateKey)
		if err != nil {
			writeError(response, err)
			return
		}
	} else if input.ClearSSHPrivateKey {
		sshKey = ""
	}
	binPath := previous.TexLiveBinPath
	if input.TexLiveBinPath != "" {
		binPath = input.TexLiveBinPath
	} else if input.ClearTexLiveBinPath {
		binPath = ""
	}
	item := repository.UserVscodeDefaults{UserID: user.ID, UserName: input.UserName, UserEmail: input.UserEmail, AccessTokenCiphertext: accessToken, SSHKeyCiphertext: sshKey, Engine: input.Engine, BuildTool: input.BuildTool, OutputDirectory: input.OutputDirectory, AutoBuild: input.AutoBuild, ShellEscape: input.ShellEscape, TexLiveBinPath: binPath}
	if err := api.store.SaveUserVscodeDefaults(request.Context(), item); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &user.ID, "user", user.ID, "vscode_defaults_updated", map[string]any{"userName": input.UserName, "userEmail": input.UserEmail, "hasAccessToken": accessToken != "", "hasSshPrivateKey": sshKey != "", "engine": input.Engine, "buildTool": input.BuildTool})
	item, err = api.store.UserVscodeDefaults(request.Context(), user.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"defaults": publicUserDefaults(item)})
}

func publicUserDefaults(item repository.UserVscodeDefaults) map[string]any {
	return map[string]any{"userId": item.UserID, "userName": item.UserName, "userEmail": item.UserEmail, "hasAccessToken": item.AccessTokenCiphertext != "", "hasSshPrivateKey": item.SSHKeyCiphertext != "", "engine": item.Engine, "buildTool": item.BuildTool, "outputDirectory": item.OutputDirectory, "autoBuild": item.AutoBuild, "shellEscape": item.ShellEscape, "texliveBinPath": "", "texliveBinConfigured": item.TexLiveBinPath != ""}
}

func validRelativeEditorPath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := strings.TrimPrefix(value, "./")
	return clean != "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func (api *API) projectTexSettings(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.ProjectTexSettings(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"config": publicTexSettings(settings), "environment": map[string]any{"available": false, "tools": map[string]any{}}})
}

func (api *API) saveProjectTexSettings(response http.ResponseWriter, request *http.Request) {
	user, err := api.auth.CurrentUser(request)
	if err != nil {
		writeError(response, err)
		return
	}
	projectID := request.PathValue("projectID")
	if _, err := api.store.ProjectAccess(request.Context(), projectID, user, "manage"); err != nil {
		writeError(response, err)
		return
	}
	var input struct {
		Engine, BuildTool, OutputDirectory, AutoBuild, PDFPreview, ProfileID, TexLiveBinPath, TexRootPath string
		SyncTex, ShellEscape                                                                              bool
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	current, err := api.store.ProjectTexSettings(request.Context(), projectID)
	if err != nil {
		writeError(response, err)
		return
	}
	if input.Engine == "" {
		input.Engine = current.Engine
	}
	if input.BuildTool == "" {
		input.BuildTool = current.BuildTool
	}
	if input.OutputDirectory == "" {
		input.OutputDirectory = current.OutputDirectory
	}
	if input.AutoBuild == "" {
		input.AutoBuild = current.AutoBuild
	}
	if input.PDFPreview == "" {
		input.PDFPreview = current.PDFPreview
	}
	if !validRelativeEditorPath(input.OutputDirectory) || len(input.OutputDirectory) > 160 || len(input.TexRootPath) > 500 || (input.TexLiveBinPath != "" && !strings.HasPrefix(input.TexLiveBinPath, "/")) {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if input.ProfileID != "" {
		profile, err := api.store.TexLiveProfile(request.Context(), input.ProfileID)
		if err != nil {
			writeError(response, err)
			return
		}
		if profile == nil {
			writeError(response, domain.NewAPIError(400, "TEX_PROFILE_NOT_FOUND", "所选 TeX Live 配置不存在或已被删除"))
			return
		}
		input.Engine, input.BuildTool, input.OutputDirectory, input.ShellEscape, input.TexLiveBinPath = profile.Engine, profile.BuildTool, profile.OutputDirectory, profile.ShellEscape, profile.TexLiveBinPath
	}
	next := repository.TexSettings{ProjectID: projectID, Engine: input.Engine, BuildTool: input.BuildTool, OutputDirectory: input.OutputDirectory, AutoBuild: input.AutoBuild, PDFPreview: input.PDFPreview, SyncTex: input.SyncTex, ShellEscape: input.ShellEscape, TexLiveBinPath: input.TexLiveBinPath, TexRootPath: input.TexRootPath, ProfileID: input.ProfileID}
	if err := api.store.SaveProjectTexSettings(request.Context(), next); err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), &projectID, &user.ID, "texlive_settings", projectID, "updated", map[string]any{"engine": next.Engine, "buildTool": next.BuildTool, "profileId": next.ProfileID})
	writeJSON(response, http.StatusOK, map[string]any{"config": publicTexSettings(next)})
}

func publicTexSettings(item repository.TexSettings) map[string]any {
	return map[string]any{"engine": item.Engine, "buildTool": item.BuildTool, "outputDirectory": item.OutputDirectory, "autoBuild": item.AutoBuild, "pdfPreview": item.PDFPreview, "syncTex": item.SyncTex, "shellEscape": item.ShellEscape, "profileId": item.ProfileID, "texliveBinPath": "", "texliveBinConfigured": item.TexLiveBinPath != "", "texRootPath": item.TexRootPath}
}

func (api *API) publicTexLiveProfiles(response http.ResponseWriter, request *http.Request) {
	if _, err := api.auth.CurrentUser(request); err != nil {
		writeError(response, err)
		return
	}
	profiles, err := api.store.ListTexLiveProfiles(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, publicTexLiveProfile(profile))
	}
	writeJSON(response, http.StatusOK, map[string]any{"profiles": items})
}

func (api *API) adminTexLiveProfiles(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	profiles, err := api.store.ListTexLiveProfiles(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	if request.Method == http.MethodGet {
		items := make([]map[string]any, 0, len(profiles))
		for _, profile := range profiles {
			items = append(items, publicTexLiveProfile(profile))
		}
		writeJSON(response, http.StatusOK, map[string]any{"profiles": items})
		return
	}
	var input struct {
		Name, Engine, BuildTool, OutputDirectory, TexLiveBinPath string
		ShellEscape                                              bool
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if err := validateTexProfileInput(input.Name, input.Engine, input.BuildTool, input.OutputDirectory, input.TexLiveBinPath); err != nil {
		writeError(response, err)
		return
	}
	profile := repository.TexLiveProfile{ID: uuid.NewString(), Name: strings.TrimSpace(input.Name), Engine: input.Engine, BuildTool: input.BuildTool, OutputDirectory: strings.TrimSpace(input.OutputDirectory), ShellEscape: input.ShellEscape, TexLiveBinPath: strings.TrimSpace(input.TexLiveBinPath)}
	if err := api.store.SaveTexLiveProfile(request.Context(), profile); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(response, domain.NewAPIError(409, "TEX_PROFILE_EXISTS", "TeX Live 配置名称已存在"))
		} else {
			writeError(response, err)
		}
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "texlive_profile", profile.ID, "created", map[string]any{"name": profile.Name, "engine": profile.Engine, "buildTool": profile.BuildTool, "texliveBinConfigured": profile.TexLiveBinPath != ""})
	writeJSON(response, http.StatusCreated, map[string]any{"profile": publicTexLiveProfile(profile)})
}

func (api *API) adminTexLiveProfile(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	id := request.PathValue("profileID")
	if _, err := uuid.Parse(id); err != nil {
		writeError(response, domain.NewAPIError(400, "INVALID_TEX_PROFILE_ID", "TeX Live 配置标识无效"))
		return
	}
	before, err := api.store.TexLiveProfile(request.Context(), id)
	if err != nil {
		writeError(response, err)
		return
	}
	if before == nil {
		writeError(response, domain.NewAPIError(404, "TEX_PROFILE_NOT_FOUND", "TeX Live 配置不存在"))
		return
	}
	if request.Method == http.MethodDelete {
		if err := api.store.DeleteTexLiveProfile(request.Context(), id); err != nil {
			writeError(response, err)
			return
		}
		api.store.Audit(request.Context(), nil, &actor.ID, "texlive_profile", id, "deleted", map[string]any{"name": before.Name})
		writeJSON(response, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	var input struct {
		Name, Engine, BuildTool, OutputDirectory, TexLiveBinPath string
		ShellEscape                                              bool
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if input.TexLiveBinPath == "" {
		input.TexLiveBinPath = before.TexLiveBinPath
	}
	if err := validateTexProfileInput(input.Name, input.Engine, input.BuildTool, input.OutputDirectory, input.TexLiveBinPath); err != nil {
		writeError(response, err)
		return
	}
	profile := repository.TexLiveProfile{ID: id, Name: strings.TrimSpace(input.Name), Engine: input.Engine, BuildTool: input.BuildTool, OutputDirectory: strings.TrimSpace(input.OutputDirectory), ShellEscape: input.ShellEscape, TexLiveBinPath: strings.TrimSpace(input.TexLiveBinPath)}
	if err := api.store.SaveTexLiveProfile(request.Context(), profile); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(response, domain.NewAPIError(409, "TEX_PROFILE_EXISTS", "TeX Live 配置名称已存在"))
		} else {
			writeError(response, err)
		}
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "texlive_profile", id, "updated", map[string]any{"before": map[string]any{"name": before.Name, "engine": before.Engine, "buildTool": before.BuildTool}, "after": map[string]any{"name": profile.Name, "engine": profile.Engine, "buildTool": profile.BuildTool}})
	writeJSON(response, http.StatusOK, map[string]any{"profile": publicTexLiveProfile(profile)})
}

func validateTexProfileInput(name, engine, buildTool, outputDirectory, binPath string) error {
	name = strings.TrimSpace(name)
	outputDirectory = strings.TrimSpace(outputDirectory)
	binPath = strings.TrimSpace(binPath)
	validEngine := engine == "pdflatex" || engine == "xelatex" || engine == "lualatex" || engine == "tectonic"
	validTool := buildTool == "latexmk" || buildTool == "tectonic" || buildTool == "engine"
	if name == "" || len(name) > 80 || !validEngine || !validTool || !validRelativeEditorPath(outputDirectory) || len(outputDirectory) > 160 || (binPath != "" && !strings.HasPrefix(binPath, "/")) || len(binPath) > 500 {
		return domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	return nil
}

func publicTexLiveProfile(profile repository.TexLiveProfile) map[string]any {
	return map[string]any{"id": profile.ID, "name": profile.Name, "engine": profile.Engine, "buildTool": profile.BuildTool, "outputDirectory": profile.OutputDirectory, "shellEscape": profile.ShellEscape, "createdAt": profile.CreatedAt, "updatedAt": profile.UpdatedAt, "texliveBinConfigured": profile.TexLiveBinPath != ""}
}

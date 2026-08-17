package httpapi

import (
	"net/http"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

func (api *API) updateAuditSettings(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var input repository.AuditSettings
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, err)
		return
	}
	if input.RetentionDays < 1 || input.RetentionDays > 3650 || input.MaxEntryBytes < 256 || input.MaxEntryBytes > 1_048_576 {
		writeError(response, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试"))
		return
	}
	if err := api.store.SaveAuditSettings(request.Context(), input); err != nil {
		writeError(response, err)
		return
	}
	purged, err := api.store.PurgeAuditEvents(request.Context(), input.RetentionDays)
	if err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "audit", "settings", "updated", map[string]any{"retentionDays": input.RetentionDays, "maxEntryBytes": input.MaxEntryBytes, "purged": purged})
	writeJSON(response, http.StatusOK, map[string]any{"settings": input, "purged": purged})
}

func (api *API) purgeAuditSettings(response http.ResponseWriter, request *http.Request) {
	actor, err := api.auth.RequireRole(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	settings, err := api.store.AuditSettings(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	purged, err := api.store.PurgeAuditEvents(request.Context(), settings.RetentionDays)
	if err != nil {
		writeError(response, err)
		return
	}
	api.store.Audit(request.Context(), nil, &actor.ID, "audit", "retention", "purged", map[string]any{"purged": purged})
	writeJSON(response, http.StatusOK, map[string]any{"purged": purged})
}

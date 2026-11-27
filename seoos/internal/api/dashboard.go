package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type dashboardRepository interface {
	Dashboard(context.Context, string, domain.SiteScope, bool) (json.RawMessage, error)
}

func (h *handler) serveDashboard(w http.ResponseWriter, r *http.Request, actor domain.Principal) bool {
	if r.URL.Path != "/api/v1/dashboard" {
		return false
	}
	repo, ok := h.repo.(dashboardRepository)
	if !ok {
		return false
	}
	if !contentRead(w, r, "GET, HEAD") || !checkQuery(w, r, "site_id", "site_country", "site_language") {
		return true
	}
	q := r.URL.Query()
	scope := domain.SiteScope{SiteID: q.Get("site_id"), Country: q.Get("site_country"), Language: q.Get("site_language")}
	data, err := repo.Dashboard(r.Context(), actor.OrganizationID, scope, actor.Role == domain.RoleOwner)
	writeRuntimeResult(w, http.StatusOK, data, err)
	return true
}

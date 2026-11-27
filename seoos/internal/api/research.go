package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func (h *handler) serveResearch(w http.ResponseWriter, r *http.Request, principal domain.Principal) bool {
	const prefix = "/api/v1/research/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	repo, ok := h.repo.(domain.ResearchRepository)
	if !ok {
		return false
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	if strings.HasPrefix(path, "snapshots/") {
		viewSnapshot(w, r, repo, principal, strings.TrimPrefix(path, "snapshots/"))
		return true
	}
	switch path {
	case "jobs", "pages", "usage", "keywords", "suggestions", "clusters", "serps", "competitors", "intents", "mappings", "opportunities", "seeds":
	default:
		writeError(w, http.StatusNotFound, "not_found", "Research resource not found.")
		return true
	}
	if r.Method == http.MethodPost && (path == "jobs" || path == "pages") {
		if !principal.Role.CanWrite() {
			forbidden(w)
			return true
		}
		if !checkQuery(w, r) {
			return true
		}
		if path == "jobs" {
			var input *domain.ResearchInput
			if readObject(w, r, &input) {
				job, created, err := repo.EnqueueResearch(r.Context(), principal.OrganizationID, *input)
				status := http.StatusOK
				if err == nil {
					w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
					if created {
						status = http.StatusCreated
					}
				}
				writeRuntimeResult(w, status, map[string]any{"job": job}, err)
			}
			return true
		}
		var input *domain.SitePageInput
		if readObject(w, r, &input) {
			id, err := repo.SaveSitePage(r.Context(), principal.OrganizationID, *input)
			writeRuntimeResult(w, http.StatusOK, map[string]string{"id": id}, err)
		}
		return true
	}
	if !readMethod(r.Method) {
		if path == "jobs" || path == "pages" {
			methodNotAllowed(w, "GET, HEAD, POST")
		} else {
			methodNotAllowed(w, "GET, HEAD")
		}
		return true
	}
	if path == "usage" && principal.Role != domain.RoleOwner {
		forbidden(w)
		return true
	}
	q, valid := readResearchQuery(w, r, path)
	if !valid {
		return true
	}
	if path == "jobs" {
		jobs, err := repo.ResearchJobs(r.Context(), principal.OrganizationID, q)
		writeRuntimeResult(w, http.StatusOK, map[string]any{"items": jobs}, err)
		return true
	}
	rows, err := repo.ListResearch(r.Context(), principal.OrganizationID, path, q)
	writeRuntimeResult(w, http.StatusOK, map[string]any{"items": rows}, err)
	return true
}

func viewSnapshot(w http.ResponseWriter, r *http.Request, repo domain.ResearchRepository, p domain.Principal, id string) {
	if !readMethod(r.Method) {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	if p.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	if !checkQuery(w, r) {
		return
	}
	snapshot, err := repo.ResearchSnapshot(r.Context(), p.OrganizationID, id)
	if err != nil {
		writeRuntimeResult(w, http.StatusOK, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(snapshot)
}

func readResearchQuery(w http.ResponseWriter, r *http.Request, section string) (domain.ResearchQuery, bool) {
	var q domain.ResearchQuery
	keys := []string{"limit", "offset"}
	if section != "usage" {
		keys = append(keys, "site_id")
	}
	marketKeys := []string{"country_code", "location_code", "language_code", "search_engine", "device"}
	if section != "usage" && section != "jobs" {
		keys = append(keys, marketKeys...)
	}
	if section == "keywords" || section == "suggestions" {
		keys = append(keys, "q")
	}
	if !checkQuery(w, r, keys...) {
		return q, false
	}
	var err error
	q.Limit, err = queryInt(r, "limit", 50, 1, 200)
	if err == nil {
		q.Offset, err = queryInt(r, "offset", 0, 0, 100000)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return q, false
	}
	query := r.URL.Query()
	q.SiteID, q.Query = query.Get("site_id"), query.Get("q")
	if section != "usage" && !domain.ValidID(q.SiteID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "site_id is required and must be a valid UUID.")
		return q, false
	}
	if len(q.Query) > 400 || strings.ContainsRune(q.Query, 0) {
		writeError(w, http.StatusBadRequest, "invalid_request", "q must contain at most 400 bytes of search text.")
		return q, false
	}
	marketSelected := false
	for _, key := range marketKeys {
		if query.Has(key) {
			marketSelected = true
			break
		}
	}
	if marketSelected {
		location, _ := strconv.Atoi(query.Get("location_code"))
		market, err := (domain.Market{
			CountryCode: query.Get("country_code"), LocationCode: location,
			LanguageCode: query.Get("language_code"), SearchEngine: query.Get("search_engine"), Device: query.Get("device"),
		}).Normalize()
		if err != nil {
			writeRuntimeResult(w, http.StatusOK, nil, err)
			return q, false
		}
		q.Market = &market
	}
	return q, true
}

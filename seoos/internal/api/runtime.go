package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func (h *handler) serveRuntime(w http.ResponseWriter, r *http.Request, principal domain.Principal) bool {
	const jobs = "/api/v1/jobs"
	const providers = "/api/v1/providers"
	path := r.URL.Path
	switch {
	case h.jobs != nil && path == jobs:
		h.jobsCollection(w, r, principal)
	case h.jobs != nil && strings.HasPrefix(path, jobs+"/"):
		h.jobResource(w, r, principal, strings.TrimPrefix(path, jobs+"/"))
	case h.providers != nil && path == providers:
		switch {
		case readMethod(r.Method):
			limit, offset, ok := readPage(w, r)
			if ok {
				providers, err := h.providers.ListProviders(r.Context(), principal.OrganizationID, limit, offset)
				writeRuntimeResult(w, http.StatusOK, map[string]any{"providers": providers}, err)
			}
		case r.Method == http.MethodPost:
			h.saveProvider(w, r, principal, "")
		default:
			methodNotAllowed(w, "GET, HEAD, POST")
		}
	case h.providers != nil && strings.HasPrefix(path, providers+"/"):
		id := strings.TrimPrefix(path, providers+"/")
		if !domain.ValidID(id) {
			writeError(w, http.StatusNotFound, "not_found", "Provider not found.")
		} else if r.Method != http.MethodPut {
			methodNotAllowed(w, "PUT")
		} else {
			h.saveProvider(w, r, principal, id)
		}
	default:
		return false
	}
	return true
}

func (h *handler) jobsCollection(w http.ResponseWriter, r *http.Request, principal domain.Principal) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		limit, offset, ok := readPage(w, r)
		if ok {
			jobs, err := h.jobs.ListJobs(r.Context(), principal.OrganizationID, limit, offset)
			writeRuntimeResult(w, http.StatusOK, map[string]any{"jobs": jobs}, err)
		}
	case http.MethodPost:
		if !principal.Role.CanWrite() {
			forbidden(w)
			return
		}
		var input *domain.JobInput
		if checkQuery(w, r) && readObject(w, r, &input) {
			job, created, err := h.jobs.EnqueueJob(r.Context(), principal.OrganizationID, *input)
			status := http.StatusOK
			if err == nil {
				w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
				if created {
					status = http.StatusCreated
				}
			}
			writeRuntimeResult(w, status, map[string]any{"job": job}, err)
		}
	default:
		methodNotAllowed(w, "GET, HEAD, POST")
	}
}

func (h *handler) jobResource(w http.ResponseWriter, r *http.Request, principal domain.Principal, id string) {
	if !domain.ValidID(id) {
		writeError(w, http.StatusNotFound, "not_found", "Job not found.")
		return
	}
	if !checkQuery(w, r) {
		return
	}
	var job domain.Job
	var err error
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		job, err = h.jobs.GetJob(r.Context(), principal.OrganizationID, id)
	case http.MethodDelete:
		if !principal.Role.CanWrite() {
			forbidden(w)
			return
		}
		job, err = h.jobs.CancelJob(r.Context(), principal.OrganizationID, id)
	default:
		methodNotAllowed(w, "GET, HEAD, DELETE")
		return
	}
	writeRuntimeResult(w, http.StatusOK, map[string]any{"job": job}, err)
}

func (h *handler) saveProvider(w http.ResponseWriter, r *http.Request, principal domain.Principal, id string) {
	if principal.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	var input *domain.ProviderInput
	if !checkQuery(w, r) || !readObject(w, r, &input) {
		return
	}
	provider, err := h.providers.SaveProvider(r.Context(), principal.OrganizationID, id, *input)
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	writeRuntimeResult(w, status, map[string]any{"provider": provider}, err)
}

func writeRuntimeResult(w http.ResponseWriter, status int, value any, err error) {
	switch {
	case err == nil:
		writeJSON(w, status, value)
	case errors.Is(err, domain.ErrSiteInactive):
		writeError(w, http.StatusConflict, "site_inactive", "The job requires an active site.")
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "This key is already bound to a different job.")
	case errors.Is(err, domain.ErrJobSettled):
		writeError(w, http.StatusConflict, "job_settled", "The job has already finished.")
	case errors.Is(err, domain.ErrProviderDisabled):
		writeError(w, http.StatusConflict, "provider_disabled", "The selected provider is disabled.")
	case errors.Is(err, domain.ErrProviderNameTaken):
		writeError(w, http.StatusConflict, "provider_name_taken", "A provider with this name already exists.")
	case errors.Is(err, domain.ErrProviderNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "credential_store_unavailable", "Provider credential storage is unavailable.")
	case errors.Is(err, domain.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "Provider has changed. Reload it before updating.")
	default:
		writeAccessResult(w, status, value, err)
	}
}

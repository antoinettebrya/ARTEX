package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

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
			limit, offset, ok := readPage(w, r, "category")
			if ok {
				category := r.URL.Query().Get("category")
				providers, err := h.providers.ListProviders(r.Context(), principal.OrganizationID, category, limit, offset)
				writeRuntimeResult(w, http.StatusOK, map[string]any{"providers": providers}, err)
			}
		case r.Method == http.MethodPost:
			h.saveProvider(w, r, principal, "")
		default:
			methodNotAllowed(w, "GET, HEAD, POST")
		}
	case h.providers != nil && path == providers+"/receipts":
		h.providerReceipts(w, r, principal)
	case h.providers != nil && strings.HasPrefix(path, providers+"/"):
		id, operation, _ := strings.Cut(strings.TrimPrefix(path, providers+"/"), "/")
		if !domain.ValidID(id) {
			writeError(w, http.StatusNotFound, "not_found", "Provider not found.")
		} else if operation == "test" {
			h.testProvider(w, r, principal, id)
		} else if operation == "reconcile" {
			h.providerReconciliation(w, r, principal, id)
		} else if operation != "" || strings.HasSuffix(path, "/") {
			writeError(w, http.StatusNotFound, "not_found", "Provider operation not found.")
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

func (h *handler) providerReceipts(w http.ResponseWriter, r *http.Request, actor domain.Principal) {
	if !readMethod(r.Method) {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	if actor.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	if limit, offset, ok := readPage(w, r, "provider_id"); ok {
		items, err := h.providers.ListProviderReceipts(r.Context(), actor.OrganizationID, r.URL.Query().Get("provider_id"), limit, offset)
		writeRuntimeResult(w, http.StatusOK, map[string]any{"items": items}, err)
	}
}

func (h *handler) providerReconciliation(w http.ResponseWriter, r *http.Request, actor domain.Principal, id string) {
	if !readMethod(r.Method) {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	if actor.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	if !checkQuery(w, r, "day", "vendor_micros") {
		return
	}
	params := r.URL.Query()
	day, dayErr := time.Parse(time.DateOnly, params.Get("day"))
	total, totalErr := strconv.ParseInt(params.Get("vendor_micros"), 10, 64)
	if dayErr != nil || totalErr != nil || day.Year() < 2000 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Provide a UTC day and the vendor USD total in micros.")
		return
	}
	row, err := h.providers.ReconcileProvider(r.Context(), actor.OrganizationID, id, day, total)
	writeRuntimeResult(w, http.StatusOK, row, err)
}

func (h *handler) testProvider(w http.ResponseWriter, r *http.Request, principal domain.Principal, id string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if principal.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	if !checkQuery(w, r) {
		return
	}
	access, err := h.providers.ReserveProviderTest(r.Context(), principal.OrganizationID, id)
	if err != nil {
		writeRuntimeResult(w, http.StatusOK, nil, err)
		return
	}
	status, diagnostic := h.providerProbe.Test(r.Context(), access)
	result := domain.ProviderTestResult{
		Status:          status,
		DurationMS:      diagnostic.DurationMS,
		ProviderVersion: access.Provider.Version,
		Diagnostic:      &diagnostic,
	}
	// Complete the audit if the browser closes while the provider is responding.
	auditContext, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()
	err = h.providers.RecordProviderTest(auditContext, principal.OrganizationID, id, result)
	writeRuntimeResult(w, http.StatusOK, result, err)
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
	case errors.Is(err, domain.ErrProviderTestLimited):
		writeError(w, http.StatusTooManyRequests, "provider_test_limited", "The connection test limit has been reached.")
	case errors.Is(err, domain.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "Provider has changed. Reload it before updating.")
	default:
		writeAccessResult(w, status, value, err)
	}
}

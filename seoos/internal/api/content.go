package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type contentRepository interface {
	ListContentProjects(context.Context, string, string, int, int) ([]domain.ContentProject, error)
	CreateContentProject(context.Context, string, domain.ContentProjectInput) (domain.ContentProject, bool, error)
	ContentWorkspace(context.Context, string, string) (domain.ContentWorkspace, error)
	ApplyContentAction(context.Context, string, string, domain.ContentAction) (domain.ContentWorkspace, error)
	ContentInventory(context.Context, string, string, int, int) ([]domain.SitePage, error)
	InventoryPage(context.Context, string, string, string) (domain.SitePage, error)
	CrawlPolicy(context.Context, string, string) (*domain.CrawlPolicy, error)
	SaveCrawlPolicy(context.Context, domain.Site, domain.CrawlPolicy) (domain.CrawlPolicy, error)
	ContentJobs(context.Context, string, string, string, int, int) ([]domain.ContentJobRecord, error)
	EnqueueContent(context.Context, string, domain.ContentJobInput) (domain.Job, bool, error)
	ContentModels(context.Context, string) ([]domain.ContentModelProfile, error)
	SaveContentModel(context.Context, string, domain.ContentModelProfile) (domain.ContentModelProfile, error)
	ContentUsage(context.Context, string) ([]json.RawMessage, error)
}

func (h *handler) serveContent(w http.ResponseWriter, r *http.Request, actor domain.Principal) bool {
	path, found := strings.CutPrefix(r.URL.Path, "/api/v1/content/")
	if !found {
		return false
	}
	repo, ok := h.repo.(contentRepository)
	if !ok {
		return false
	}
	if (r.Method == http.MethodPost || r.Method == http.MethodPut) && !actor.Role.CanWrite() {
		forbidden(w)
		return true
	}
	org, ctx := actor.OrganizationID, r.Context()
	site := r.URL.Query().Get("site_id")
	switch path {
	case "projects":
		if r.Method == http.MethodPost {
			var input *domain.ContentProjectInput
			if checkQuery(w, r) && readObject(w, r, &input) {
				val, created, err := repo.CreateContentProject(ctx, org, *input)
				contentResult(w, contentCreated(created), "project", val, err)
			}
		} else if contentRead(w, r, "GET, HEAD, POST") {
			limit, offset, ok := contentPage(w, r)
			if ok {
				val, err := repo.ListContentProjects(ctx, org, site, limit, offset)
				contentResult(w, 200, "items", val, err)
			}
		}
	case "jobs":
		if r.Method == http.MethodPost {
			var input *domain.ContentJobInput
			if checkQuery(w, r) && readObject(w, r, &input) {
				if input.Kind == domain.ContentVerify && actor.Role != domain.RoleOwner {
					forbidden(w)
					break
				}
				job, created, err := repo.EnqueueContent(ctx, org, *input)
				if err == nil {
					w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
				}
				contentResult(w, contentCreated(created), "job", job, err)
			}
		} else if contentRead(w, r, "GET, HEAD, POST") {
			limit, offset, ok := contentPage(w, r, "project_id")
			if ok {
				val, err := repo.ContentJobs(ctx, org, site, r.URL.Query().Get("project_id"), limit, offset)
				contentResult(w, 200, "items", val, err)
			}
		}
	case "inventory":
		if contentRead(w, r, "GET, HEAD") {
			limit, offset, ok := contentPage(w, r)
			if ok {
				val, err := repo.ContentInventory(ctx, org, site, limit, offset)
				contentResult(w, 200, "items", val, err)
			}
		}
	case "crawl":
		h.contentCrawl(w, r, actor, repo, site)
	case "models":
		if r.Method == http.MethodPut {
			if actor.Role != domain.RoleOwner {
				forbidden(w)
				break
			}
			var input *domain.ContentModelProfile
			if checkQuery(w, r) && readObject(w, r, &input) {
				val, err := repo.SaveContentModel(ctx, org, *input)
				contentResult(w, 200, "profile", val, err)
			}
		} else if contentRead(w, r, "GET, HEAD, PUT") && checkQuery(w, r) {
			val, err := repo.ContentModels(ctx, org)
			contentResult(w, 200, "items", val, err)
		}
	case "usage":
		if actor.Role != domain.RoleOwner {
			forbidden(w)
		} else if contentRead(w, r, "GET, HEAD") && checkQuery(w, r) {
			val, err := repo.ContentUsage(ctx, org)
			contentResult(w, 200, "items", val, err)
		}
	default:
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[0] == "inventory" && domain.ValidID(parts[1]) {
			if contentRead(w, r, "GET, HEAD") && checkQuery(w, r, "site_id") {
				val, err := repo.InventoryPage(ctx, org, site, parts[1])
				contentResult(w, 200, "page", val, err)
			}
		} else if len(parts) >= 2 && parts[0] == "projects" && domain.ValidID(parts[1]) {
			h.contentProject(w, r, actor, repo, parts[1:])
		} else {
			writeError(w, 404, "not_found", "Content resource not found.")
		}
	}
	return true
}

func (h *handler) contentCrawl(w http.ResponseWriter, r *http.Request, actor domain.Principal, repo contentRepository, id string) {
	if !checkQuery(w, r, "site_id") {
		return
	}
	ctx, org := r.Context(), actor.OrganizationID
	if r.Method != http.MethodPut {
		if contentRead(w, r, "GET, HEAD, PUT") {
			policy, err := repo.CrawlPolicy(ctx, org, id)
			contentResult(w, 200, "policy", policy, err)
		}
		return
	}
	if actor.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	var input *domain.CrawlPolicy
	if !readObject(w, r, &input) {
		return
	}
	if id == "" {
		id = input.SiteID
	}
	if input.SiteID != "" && input.SiteID != id {
		writeError(w, 400, "invalid_request", "Site does not match the policy.")
		return
	}
	site, err := h.repo.GetSite(ctx, org, id)
	var policy domain.CrawlPolicy
	if err == nil {
		input.SiteID = site.ID
		policy, err = contentinventory.ValidatePolicy(site, *input)
		if err == nil {
			policy, err = repo.SaveCrawlPolicy(ctx, site, policy)
		}
	}
	contentResult(w, 200, "policy", policy, err)
}

func (h *handler) contentProject(w http.ResponseWriter, r *http.Request, actor domain.Principal, repo contentRepository, path []string) {
	if !checkQuery(w, r) {
		return
	}
	ctx, org, id := r.Context(), actor.OrganizationID, path[0]
	if len(path) == 1 {
		if contentRead(w, r, "GET, HEAD") {
			val, err := repo.ContentWorkspace(ctx, org, id)
			contentResult(w, 200, "workspace", val, err)
		}
		return
	}
	if len(path) != 2 || path[1] != "actions" {
		writeError(w, 404, "not_found", "Content project resource not found.")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var input *domain.ContentAction
	if !readObject(w, r, &input) {
		return
	}
	if input.Action == "add_source" && input.Source != nil {
		workspace, err := repo.ContentWorkspace(ctx, org, id)
		if err == nil {
			var policy *domain.CrawlPolicy
			policy, err = repo.CrawlPolicy(ctx, org, workspace.Project.SiteID)
			if err == nil && policy == nil {
				err = domain.ErrCrawlUnauthorized
			}
			if err == nil {
				*input.Source, err = contentinventory.ValidateSource(*policy, *input.Source)
			}
		}
		if err != nil {
			contentResult(w, 200, "", nil, err)
			return
		}
	}
	val, err := repo.ApplyContentAction(ctx, org, id, *input)
	contentResult(w, 200, "workspace", val, err)
}

func contentCreated(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func contentRead(w http.ResponseWriter, r *http.Request, methods string) bool {
	if !readMethod(r.Method) {
		methodNotAllowed(w, methods)
		return false
	}
	return true
}

func contentPage(w http.ResponseWriter, r *http.Request, extra ...string) (int, int, bool) {
	if !checkQuery(w, r, append([]string{"site_id", "limit", "offset"}, extra...)...) {
		return 0, 0, false
	}
	limit, err := queryInt(r, "limit", 50, 1, 200)
	offset, offsetErr := queryInt(r, "offset", 0, 0, 100000)
	if !domain.ValidID(r.URL.Query().Get("site_id")) || err != nil || offsetErr != nil {
		writeError(w, 400, "invalid_request", "Use a valid site and pagination settings.")
		return 0, 0, false
	}
	return limit, offset, true
}

func contentResult(w http.ResponseWriter, status int, key string, data any, err error) {
	switch {
	case errors.Is(err, domain.ErrContentForbidden), errors.Is(err, domain.ErrCrawlUnauthorized):
		forbidden(w)
	case errors.Is(err, domain.ErrContentPrerequisite):
		writeError(w, 409, "content_prerequisite", "Required research evidence or content version is missing.")
	case errors.Is(err, domain.ErrContentGate):
		writeError(w, 409, "content_gate_blocked", "Pass fact and quality checks and record explicit approval for this version.")
	case errors.Is(err, domain.ErrVersionConflict):
		writeError(w, 409, "version_conflict", "The content project changed. Reload its latest version.")
	default:
		writeRuntimeResult(w, status, map[string]any{key: data}, err)
	}
}

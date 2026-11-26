package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/executionhttp"
	"github.com/Autumn-27/artex/seoos/internal/executionsearch"
)

type executionRepository interface {
	SaveExecutionConnection(context.Context, string, string, domain.ExecutionConnectionSave) (domain.ExecutionConnection, error)
	ExecutionConnectionAccess(context.Context, string, string, string) (domain.ExecutionConnection, domain.ExecutionCredential, error)
	ExecutionRows(context.Context, string, string, domain.ExecutionFilter) ([]json.RawMessage, error)
	ExecutionSitemapOutput(context.Context, string, string, string) (json.RawMessage, error)
	CreateExecutionPublication(context.Context, string, domain.ExecutionPublicationInput) (domain.ExecutionPublication, bool, error)
	ExecutionPublication(context.Context, string, string) (domain.ExecutionPublication, []domain.ExecutionPublicationEvent, error)
	EnqueueExecution(context.Context, string, domain.ExecutionJobInput) (domain.Job, bool, error)
	OAuthState(context.Context, domain.ExecutionConnection) (string, string, error)
	ConsumeOAuthState(context.Context, domain.ExecutionConnection, string) (string, error)
	SaveExecutionTokens(context.Context, domain.ExecutionConnection, domain.ExecutionCredential) error
	UseExecutionRequest(context.Context, string, string, string, int64, *domain.Lease) error
}

func (h *handler) serveExecution(w http.ResponseWriter, r *http.Request, actor domain.Principal) bool {
	path, ok := strings.CutPrefix(r.URL.Path, "/api/v1/execution/")
	if !ok {
		return false
	}
	repo, ok := h.repo.(executionRepository)
	if !ok {
		return false
	}
	org, ctx := actor.OrganizationID, r.Context()
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if !actor.Role.CanWrite() {
			forbidden(w)
			return true
		}
		if !checkQuery(w, r) {
			return true
		}
		switch {
		case path == "connections" && r.Method == http.MethodPost,
			strings.HasPrefix(path, "connections/") && r.Method == http.MethodPut:
			if actor.Role != domain.RoleOwner {
				forbidden(w)
				return true
			}
			id := strings.TrimPrefix(path, "connections/")
			status := http.StatusOK
			if path == "connections" {
				id = ""
				status = http.StatusCreated
			} else if !domain.ValidID(id) {
				executionResult(w, 200, nil, domain.ErrNotFound)
				return true
			}
			var input *domain.ExecutionConnectionSave
			if readObject(w, r, &input) {
				val, err := repo.SaveExecutionConnection(ctx, org, id, *input)
				executionResult(w, status, map[string]any{"connection": val}, err)
			}
		case path == "publications" && r.Method == http.MethodPost:
			if actor.Role != domain.RoleOwner {
				forbidden(w)
				return true
			}
			var input *domain.ExecutionPublicationInput
			if readObject(w, r, &input) {
				val, created, err := repo.CreateExecutionPublication(ctx, org, *input)
				executionResult(w, contentCreated(created), map[string]any{"publication": val}, err)
			}
		case path == "jobs" && r.Method == http.MethodPost:
			var input *domain.ExecutionJobInput
			if !readJSONLimit(w, r, &input, 6<<20) {
				return true
			}
			if input == nil {
				writeError(w, 400, "invalid_request", "Request body must be a JSON object.")
				return true
			}
			job, created, err := repo.EnqueueExecution(ctx, org, *input)
			if err == nil {
				w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
			}
			executionResult(w, contentCreated(created), map[string]any{"job": job}, err)
		case (path == "oauth/start" || path == "oauth/exchange") && r.Method == http.MethodPost:
			h.executionOAuth(w, r, repo, actor, path)
		default:
			methodNotAllowed(w, "GET, HEAD")
		}
		return true
	}
	if !contentRead(w, r, "GET, HEAD") {
		return true
	}
	if id, ok := strings.CutPrefix(path, "publications/"); ok {
		if checkQuery(w, r) && domain.ValidID(id) {
			data, events, err := repo.ExecutionPublication(ctx, org, id)
			executionResult(w, 200, map[string]any{"publication": data, "events": events}, err)
		} else if !domain.ValidID(id) {
			executionResult(w, 200, nil, domain.ErrNotFound)
		}
		return true
	}
	if id, ok := strings.CutPrefix(path, "sitemaps/"); ok {
		if checkQuery(w, r, "site_id") {
			doc, err := repo.ExecutionSitemapOutput(ctx, org, r.URL.Query().Get("site_id"), id)
			executionResult(w, 200, map[string]any{"document": doc}, err)
		}
		return true
	}
	switch path {
	case "connections", "packages", "publications", "jobs", "pages", "media", "properties", "metrics", "index", "sitemaps", "extractions", "usage", "feedback", "submissions":
		if path == "usage" && actor.Role != domain.RoleOwner {
			forbidden(w)
			return true
		}
		limit, offset, ok := contentPage(w, r, "connection_id", "kind", "from", "to", "page", "country")
		if !ok {
			return true
		}
		q := r.URL.Query()
		filter := domain.ExecutionFilter{SiteID: q.Get("site_id"), ConnectionID: q.Get("connection_id"), Kind: q.Get("kind"), From: q.Get("from"),
			To: q.Get("to"), Page: q.Get("page"), Country: q.Get("country"), Limit: limit, Offset: offset}
		if _, _, err := domain.ExecutionDateRange(filter.From, filter.To); err != nil {
			executionResult(w, 200, nil, err)
			return true
		}
		if err := filter.ValidateReportFilters(); err != nil {
			executionResult(w, 200, nil, err)
			return true
		}
		items, err := repo.ExecutionRows(ctx, org, path, filter)
		executionResult(w, 200, map[string]any{"items": items}, err)
	default:
		executionResult(w, 200, nil, domain.ErrNotFound)
	}
	return true
}

func (h *handler) executionOAuth(w http.ResponseWriter, r *http.Request, repo executionRepository, actor domain.Principal, path string) {
	if actor.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	var input *struct {
		SiteID       string `json:"site_id"`
		ConnectionID string `json:"connection_id"`
		State        string `json:"state,omitempty"`
		Code         string `json:"code,omitempty"`
	}
	if !readObject(w, r, &input) {
		return
	}
	ctx := r.Context()
	c, s, err := repo.ExecutionConnectionAccess(ctx, actor.OrganizationID, input.SiteID, input.ConnectionID)
	if err != nil {
		executionResult(w, 200, nil, err)
		return
	}
	if c.Kind != domain.ExecutionGSC && c.Kind != domain.ExecutionGA4 || c.RedirectURI == "" {
		executionResult(w, 200, nil, domain.ErrExecutionUnsupported)
		return
	}
	if path == "oauth/start" {
		state, proof, err := repo.OAuthState(ctx, c)
		var target string
		if err == nil {
			target, err = executionsearch.Authorize(c, s, state, proof)
		}
		executionResult(w, 200, map[string]string{"authorization_url": target, "state": state}, err)
		return
	}
	if input.Code == "" || len(input.Code) > 4096 {
		executionResult(w, 200, nil, domain.ErrExecutionAuthority)
		return
	}
	proof, err := repo.ConsumeOAuthState(ctx, c, input.State)
	if err != nil {
		executionResult(w, 200, nil, err)
		return
	}
	client := executionhttp.Client(h.executionFixture, time.Duration(c.TimeoutSeconds)*time.Second, func(ctx context.Context) error {
		return repo.UseExecutionRequest(ctx, c.OrganizationID, c.SiteID, c.ID, c.Version, nil)
	})
	token, err := executionsearch.New(client).Exchange(ctx, c, s, input.Code, proof)
	if err == nil {
		err = repo.SaveExecutionTokens(ctx, c, token)
	}
	executionResult(w, 200, map[string]bool{"connected": err == nil}, err)
}

func executionResult(w http.ResponseWriter, status int, value any, err error) {
	var data *domain.DataError
	switch {
	case errors.Is(err, domain.ErrExecutionUnsupported):
		writeError(w, 409, "execution_unsupported", "The configured connection does not support this operation.")
	case errors.Is(err, domain.ErrExecutionState):
		writeError(w, 409, "execution_state", "The current execution state does not allow this operation.")
	case errors.Is(err, domain.ErrExecutionAuthority):
		forbidden(w)
	case errors.As(err, &data):
		if data.Code == "execution_budget" {
			writeError(w, 429, data.Code, "The request limit has been reached.")
		} else {
			writeError(w, 502, data.Code, "The provider request was unable to complete.")
		}
	case errors.Is(err, domain.ErrContentGate):
		writeError(w, 409, "content_gate_blocked", "An approved current content package is required.")
	default:
		writeRuntimeResult(w, status, value, err)
	}
}

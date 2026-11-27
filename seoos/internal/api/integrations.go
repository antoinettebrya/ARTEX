package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/executionsearch"
	"github.com/Autumn-27/artex/seoos/internal/store"
	"github.com/google/uuid"
)

type integrationRepository interface {
	executionsearch.AccountLedger
	IntegrationRows(context.Context, string, string, domain.IntegrationFilter) ([]json.RawMessage, error)
	IntegrationSummary(context.Context, string) (json.RawMessage, error)
	IntegrationAccess(context.Context, string, string) (domain.IntegrationAccess, error)
	IntegrationAccount(context.Context, string, string) (domain.IntegrationAccount, error)
	SaveIntegrationApplication(context.Context, string, string, domain.IntegrationApplicationInput) (domain.IntegrationApplication, error)
	RemoveIntegrationApplication(context.Context, string, string) error
	CreateIntegrationAccount(context.Context, string, domain.IntegrationAccountInput) (domain.IntegrationAccount, error)
	StartIntegrationOAuth(context.Context, string, string, []string) (store.IntegrationState, string, error)
	ConsumeIntegrationOAuth(context.Context, string, string, string, string) (store.IntegrationState, error)
	SaveIntegrationGrant(context.Context, string, domain.IntegrationAccess, domain.ExecutionCredential, []string) (domain.IntegrationAccount, error)
	RefreshIntegration(context.Context, string, string, int64, bool, func(context.Context, domain.IntegrationAccess) (domain.ExecutionCredential, []string, error)) (domain.IntegrationAccess, error)
	RevokeIntegrationAccount(context.Context, string, string, bool) (domain.IntegrationAccess, error)
	RotateBingAPIKey(context.Context, string, string, string, int64) error
	IntegrationBindingSettings(context.Context, string, string) (domain.IntegrationBindingInput, error)
	GetSite(context.Context, string, string) (domain.Site, error)
	SaveIntegrationBinding(context.Context, string, string, domain.IntegrationBindingInput, domain.Site, domain.IntegrationResource, int64) (string, error)
	RemoveIntegrationBinding(context.Context, string, string) error
	SaveIndexNowKey(context.Context, string, string, domain.IndexNowKeyInput) (string, error)
	RevealIndexNowKey(context.Context, string, string) (string, string, error)
	EnqueueExecution(context.Context, string, domain.ExecutionJobInput) (domain.Job, bool, error)
}

func (h *handler) serveIntegrations(w http.ResponseWriter, r *http.Request, actor domain.Principal) bool {
	path, ok := strings.CutPrefix(r.URL.Path, "/api/v1/integrations/")
	if !ok {
		return false
	}
	repo, ok := h.repo.(integrationRepository)
	if !ok {
		return false
	}
	org := actor.OrganizationID
	part := strings.Split(path, "/")
	if part[0] == "oauth" {
		h.integrationCallback(w, r, repo, actor, part)
		return true
	}
	if readMethod(r.Method) && h.integrationList(w, r, repo, org, path) {
		return true
	}
	if actor.Role != domain.RoleOwner {
		forbidden(w)
		return true
	}
	if !checkQuery(w, r) {
		return true
	}
	switch part[0] {
	case "applications":
		h.integrationApplication(w, r, repo, org, part)
	case "accounts":
		h.integrationAccount(w, r, repo, org, part)
	case "bindings":
		h.integrationBinding(w, r, repo, org, part)
	case "indexnow":
		h.integrationIndexNow(w, r, repo, org, part)
	default:
		executionResult(w, 200, nil, domain.ErrNotFound)
	}
	return true
}

func (h *handler) integrationList(w http.ResponseWriter, r *http.Request, repo integrationRepository, org, path string) bool {
	if path == "summary" {
		if checkQuery(w, r) {
			rows, err := repo.IntegrationSummary(r.Context(), org)
			executionResult(w, 200, map[string]any{"items": rows}, err)
		}
		return true
	}
	section := path
	switch path {
	case "applications", "accounts", "bindings", "sites", "health", "history", "usage":
	case "indexnow/keys":
		section = "keys"
	default:
		return false
	}
	if !checkQuery(w, r, "site_id", "limit", "offset", "provider", "account_id", "status", "q", "from", "to") {
		return true
	}
	limit, limitErr := queryInt(r, "limit", 50, 1, 200)
	offset, offsetErr := queryInt(r, "offset", 0, 0, 100000)
	if limitErr != nil || offsetErr != nil {
		writeError(w, 400, "invalid_request", "Use valid pagination settings.")
		return true
	}
	q := r.URL.Query()
	items, err := repo.IntegrationRows(r.Context(), org, section, domain.IntegrationFilter{Provider: q.Get("provider"),
		SiteID: q.Get("site_id"), AccountID: q.Get("account_id"), Status: q.Get("status"), Query: q.Get("q"),
		From: q.Get("from"), To: q.Get("to"), Limit: limit, Offset: offset})
	executionResult(w, 200, map[string]any{"items": items}, err)
	return true
}

func (h *handler) integrationApplication(w http.ResponseWriter, r *http.Request, repo integrationRepository, org string, part []string) {
	ctx := r.Context()
	if len(part) == 2 && r.Method == http.MethodDelete {
		err := repo.RemoveIntegrationApplication(ctx, org, part[1])
		executionResult(w, 200, map[string]bool{"removed": err == nil}, err)
		return
	}
	if len(part) == 1 && r.Method == http.MethodPost || len(part) == 2 && r.Method == http.MethodPut {
		var input *domain.IntegrationApplicationInput
		if readObject(w, r, &input) {
			id := ""
			if len(part) == 2 {
				id = part[1]
			}
			value, err := repo.SaveIntegrationApplication(ctx, org, id, *input)
			executionResult(w, contentCreated(id == ""), map[string]any{"application": value}, err)
		}
		return
	}
	methodNotAllowed(w, "GET, HEAD, POST, PUT, DELETE")
}

func (h *handler) integrationAccount(w http.ResponseWriter, r *http.Request, repo integrationRepository, org string, part []string) {
	ctx := r.Context()
	if len(part) == 1 && r.Method == http.MethodPost {
		var input *domain.IntegrationAccountInput
		if readObject(w, r, &input) {
			account, err := repo.CreateIntegrationAccount(ctx, org, *input)
			executionResult(w, 201, map[string]any{"account": account}, err)
		}
		return
	}
	if len(part) < 2 || !domain.ValidID(part[1]) {
		executionResult(w, 200, nil, domain.ErrNotFound)
		return
	}
	id := part[1]
	switch {
	case len(part) == 2 && readMethod(r.Method):
		value, err := repo.IntegrationAccount(ctx, org, id)
		executionResult(w, 200, map[string]any{"account": value}, err)
	case len(part) == 2 && r.Method == http.MethodDelete:
		_, err := repo.RevokeIntegrationAccount(ctx, org, id, true)
		executionResult(w, 200, map[string]bool{"removed": err == nil}, err)
	case len(part) == 3 && r.Method == http.MethodPost:
		h.integrationAccountAction(w, r, repo, org, id, part[2])
	default:
		methodNotAllowed(w, "GET, HEAD, POST, DELETE")
	}
}

func (h *handler) integrationAccountAction(w http.ResponseWriter, r *http.Request, repo integrationRepository, org, id, action string) {
	ctx := r.Context()
	var input *struct {
		Scopes    []string `json:"scopes,omitempty"`
		Provider  string   `json:"provider,omitempty"`
		PageToken string   `json:"page_token,omitempty"`
		APIKey    string   `json:"api_key,omitempty"`
		Version   int64    `json:"credential_version,omitempty"`
	}
	if !readObject(w, r, &input) {
		return
	}
	switch action {
	case "authorize":
		state, key, err := repo.StartIntegrationOAuth(ctx, org, id, input.Scopes)
		var target string
		if err == nil {
			target, err = executionsearch.IntegrationAuthorizationURL(state.Access, state.Scopes, key, state.Verifier)
		}
		executionResult(w, 200, map[string]string{"authorization_url": target, "state": key}, err)
	case "discover":
		access, err := h.integrationReady(ctx, repo, org, id, false)
		if err != nil {
			executionResult(w, 200, nil, err)
			return
		}
		client := executionsearch.AccountClient(h.executionFixture, repo, org, access, false)
		items, next, err := client.DiscoverIntegration(ctx, access, input.Provider, input.PageToken)
		executionResult(w, 200, map[string]any{"items": items, "next_page_token": next}, err)
	case "refresh":
		access, err := h.integrationReady(ctx, repo, org, id, true)
		executionResult(w, 200, map[string]any{"account": access.Account}, err)
	case "revoke":
		access, err := repo.RevokeIntegrationAccount(ctx, org, id, false)
		if err != nil {
			executionResult(w, 200, nil, err)
			return
		}
		access.Account.Version++
		providerResult := "local_only"
		if access.Account.Provider == domain.IntegrationGoogle && access.Credential.RefreshToken != "" {
			providerResult = "revoked"
			if err := executionsearch.AccountClient(h.executionFixture, repo, org, access, true).RevokeGoogleIntegration(ctx, access); err != nil {
				providerResult = "unconfirmed"
			}
		}
		executionResult(w, 200, map[string]any{"revoked": true, "provider_result": providerResult}, nil)
	case "rotate-key":
		err := repo.RotateBingAPIKey(ctx, org, id, input.APIKey, input.Version)
		executionResult(w, 200, map[string]bool{"rotated": err == nil}, err)
	default:
		executionResult(w, 200, nil, domain.ErrNotFound)
	}
}

func (h *handler) integrationReady(ctx context.Context, repo integrationRepository, org, id string, force bool) (domain.IntegrationAccess, error) {
	access, err := repo.IntegrationAccess(ctx, org, id)
	if err != nil {
		return access, err
	}
	return repo.RefreshIntegration(ctx, org, id, access.Account.Version, force,
		func(ctx context.Context, access domain.IntegrationAccess) (domain.ExecutionCredential, []string, error) {
			return executionsearch.AccountClient(h.executionFixture, repo, org, access, false).RefreshIntegrationToken(ctx, access)
		})
}

func (h *handler) integrationCallback(w http.ResponseWriter, r *http.Request, repo integrationRepository, actor domain.Principal, part []string) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if len(part) != 3 || part[1] != "callback" || part[2] != domain.IntegrationGoogle && part[2] != domain.IntegrationBing {
		executionResult(w, 200, nil, domain.ErrNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	if actor.Role != domain.RoleOwner {
		forbidden(w)
		return
	}
	if !checkQuery(w, r, "state", "code", "error", "redirect_uri") {
		return
	}
	q, org, ctx := r.URL.Query(), actor.OrganizationID, r.Context()
	code, failure := q.Get("code"), q.Get("error")
	if (code == "") == (failure == "") || len(code) > 4096 || len(failure) > 64 {
		writeError(w, 400, "invalid_integration_callback", "The OAuth callback parameters are invalid.")
		return
	}
	pending, err := repo.ConsumeIntegrationOAuth(ctx, org, part[2], q.Get("state"), q.Get("redirect_uri"))
	if err != nil {
		executionResult(w, 200, nil, err)
		return
	}
	if failure != "" {
		executionResult(w, 200, map[string]any{"cancelled": true}, nil)
		return
	}
	c, scopes, err := executionsearch.AccountClient(h.executionFixture, repo, org, pending.Access, true).
		ExchangeIntegration(ctx, pending.Access, pending.Scopes, code, pending.Verifier)
	if err != nil {
		executionResult(w, 200, nil, err)
		return
	}
	account, err := repo.SaveIntegrationGrant(ctx, org, pending.Access, c, scopes)
	executionResult(w, 200, map[string]any{"connected": err == nil, "account": account}, err)
}

type bindingDirectoryKey struct {
	account, provider string
	version           int64
}

func (h *handler) verifyAndSaveBinding(ctx context.Context, repo integrationRepository, org, id string, input domain.IntegrationBindingInput, directories map[bindingDirectoryKey][]domain.IntegrationResource) (string, error) {
	var empty string
	site, err := repo.GetSite(ctx, org, input.SiteID)
	if err != nil {
		return empty, err
	}
	if site.Status != "active" {
		return empty, domain.ErrSiteInactive
	}
	access, err := h.integrationReady(ctx, repo, org, input.AccountID, false)
	if err != nil {
		return empty, err
	}
	if !access.Account.Allows(input.Kind, input.AllowWrites) {
		return empty, domain.ErrExecutionAuthority
	}
	client := executionsearch.AccountClient(h.executionFixture, repo, org, access, false)
	var resource domain.IntegrationResource
	if directories != nil && (input.Kind == domain.ExecutionGSC || input.Kind == domain.ExecutionBing) {
		key := bindingDirectoryKey{input.AccountID, input.Kind, access.Account.Version}
		items, ok := directories[key]
		if !ok {
			items, _, err = client.DiscoverIntegration(ctx, access, input.Kind, "")
			if err != nil {
				return empty, err
			}
			directories[key] = items
		}
		resource, err = executionsearch.VerifyListedResource(site, input.Kind, input.ResourceID, items)
	} else {
		resource, err = client.VerifyIntegrationResource(ctx, access, site, input.Kind, input.ResourceID)
	}
	if err != nil {
		return empty, err
	}
	binding, err := repo.SaveIntegrationBinding(ctx, org, id, input, site, resource, access.Account.Version)
	if errors.Is(err, domain.ErrConflict) {
		return empty, domain.ErrIntegrationBindingConflict
	}
	return binding, err
}

func (h *handler) integrationBinding(w http.ResponseWriter, r *http.Request, repo integrationRepository, org string, part []string) {
	ctx := r.Context()
	switch {
	case len(part) == 2 && r.Method == http.MethodDelete:
		err := repo.RemoveIntegrationBinding(ctx, org, part[1])
		executionResult(w, 200, map[string]bool{"removed": err == nil}, err)
	case len(part) == 3 && part[2] == "verify" && r.Method == http.MethodPost:
		var request *struct {
			Enable bool `json:"enable,omitempty"`
		}
		if !readObject(w, r, &request) {
			return
		}
		in, err := repo.IntegrationBindingSettings(ctx, org, part[1])
		if err == nil && in.Kind == domain.ExecutionIndexNow {
			h.enqueueIndexNowVerification(w, r, repo, org, in.SiteID, part[1], "verify-"+uuid.NewString())
			return
		}
		if err == nil {
			in.Enabled = in.Enabled || request.Enable
			_, err = h.verifyAndSaveBinding(ctx, repo, org, part[1], in, nil)
		}
		executionResult(w, 200, map[string]bool{"verified": err == nil}, err)
	case len(part) == 2 && part[1] == "batch" && r.Method == http.MethodPost:
		h.integrationBindingBatch(w, r, repo, org)
	case len(part) == 1 && r.Method == http.MethodPost || len(part) == 2 && r.Method == http.MethodPut:
		var input *domain.IntegrationBindingInput
		if readObject(w, r, &input) {
			id := ""
			if len(part) == 2 {
				id = part[1]
				if !domain.ValidID(id) {
					executionResult(w, 200, nil, domain.ErrNotFound)
					return
				}
			}
			value, err := h.verifyAndSaveBinding(ctx, repo, org, id, *input, nil)
			executionResult(w, contentCreated(id == ""), map[string]string{"id": value}, err)
		}
	default:
		methodNotAllowed(w, "GET, HEAD, POST, PUT, DELETE")
	}
}

func (h *handler) integrationBindingBatch(w http.ResponseWriter, r *http.Request, repo integrationRepository, org string) {
	var input *struct {
		Items []domain.IntegrationBindingInput `json:"items"`
	}
	if !readObject(w, r, &input) {
		return
	}
	if len(input.Items) == 0 || len(input.Items) > 20 {
		writeError(w, 400, "invalid_batch", "Choose 1 to 20 separately verified bindings.")
		return
	}
	type bindingResult struct {
		SiteID string `json:"site_id"`
		ID     string `json:"id,omitempty"`
		Status string `json:"status"`
	}
	results := make([]bindingResult, 0, len(input.Items))
	directories := make(map[bindingDirectoryKey][]domain.IntegrationResource)
	for _, item := range input.Items {
		value, err := h.verifyAndSaveBinding(r.Context(), repo, org, "", item, directories)
		if errors.Is(err, domain.ErrIntegrationBindingConflict) {
			results = append(results, bindingResult{SiteID: item.SiteID, Status: "conflict"})
		} else if err != nil {
			results = append(results, bindingResult{SiteID: item.SiteID, Status: "not_verified"})
		} else {
			results = append(results, bindingResult{SiteID: item.SiteID, ID: value, Status: "bound"})
		}
	}
	executionResult(w, 200, map[string]any{"items": results}, nil)
}

func (h *handler) integrationIndexNow(w http.ResponseWriter, r *http.Request, repo integrationRepository, org string, part []string) {
	if len(part) < 2 || part[1] != "keys" {
		executionResult(w, 200, nil, domain.ErrNotFound)
		return
	}
	ctx := r.Context()
	switch {
	case len(part) == 2 && r.Method == http.MethodPost, len(part) == 4 && part[3] == "rotate" && r.Method == http.MethodPost:
		id := ""
		if len(part) == 4 {
			id = part[2]
			if !domain.ValidID(id) {
				executionResult(w, 200, nil, domain.ErrNotFound)
				return
			}
		}
		var input *domain.IndexNowKeyInput
		if !readObject(w, r, &input) {
			return
		}
		value, err := repo.SaveIndexNowKey(ctx, org, id, *input)
		if err != nil {
			if errors.Is(err, domain.ErrConflict) {
				err = domain.ErrIntegrationBindingConflict
			}
			executionResult(w, 200, nil, err)
			return
		}
		job, _, enqueue := repo.EnqueueExecution(ctx, org, domain.ExecutionJobInput{SiteID: input.SiteID, ConnectionID: value,
			Kind: domain.ExecConnectionTest, IdempotencyKey: "indexnow-key-" + uuid.NewString()})
		executionResult(w, contentCreated(id == ""), map[string]any{"id": value, "verification_job": job, "verification_queued": enqueue == nil}, nil)
	case len(part) == 4 && part[3] == "verify" && r.Method == http.MethodPost:
		var request *struct {
			Key string `json:"idempotency_key,omitempty"`
		}
		if !readObject(w, r, &request) {
			return
		}
		in, err := repo.IntegrationBindingSettings(ctx, org, part[2])
		if err != nil || in.Kind != domain.ExecutionIndexNow {
			executionResult(w, 200, nil, domain.ErrNotFound)
			return
		}
		if request.Key == "" {
			request.Key = "indexnow-verify-" + uuid.NewString()
		}
		h.enqueueIndexNowVerification(w, r, repo, org, in.SiteID, part[2], request.Key)
	case len(part) == 4 && (part[3] == "reveal" || part[3] == "file") && r.Method == http.MethodGet:
		key, location, err := repo.RevealIndexNowKey(ctx, org, part[2])
		if err != nil {
			executionResult(w, 200, nil, err)
			return
		}
		if part[3] == "file" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", key+".txt"))
			_, _ = w.Write([]byte(key))
		} else {
			executionResult(w, 200, map[string]string{"key": key, "verification_url": location, "filename": key + ".txt"}, nil)
		}
	default:
		methodNotAllowed(w, "GET, HEAD, POST")
	}
}

func (h *handler) enqueueIndexNowVerification(w http.ResponseWriter, r *http.Request, repo integrationRepository, org, site, id, key string) {
	job, created, err := repo.EnqueueExecution(r.Context(), org, domain.ExecutionJobInput{SiteID: site, ConnectionID: id,
		Kind: domain.ExecConnectionTest, IdempotencyKey: key})
	executionResult(w, contentCreated(created), map[string]any{"job": job}, err)
}

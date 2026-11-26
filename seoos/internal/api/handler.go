// Package api exposes the SEO service's deliberately small HTTP interface.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	maxBodyBytes   = 64 << 10
	requestTimeout = 10 * time.Second
	readyTimeout   = 2 * time.Second
)

type handler struct {
	repo           domain.Repository
	access         domain.AccessRepository
	jobs           domain.JobRepository
	providers      domain.ProviderRepository
	organizationID string
	tokenHash      [sha256.Size]byte
}

// NewHandler keeps the configured credential scoped to its bootstrap organization.
// Member credentials resolve to their own organization on every API request.
// The caller must provide TLS when exposing the service outside localhost.
func NewHandler(repo domain.Repository, organizationID, bearerToken string) (http.Handler, error) {
	if repo == nil {
		return nil, errors.New("API repository is required")
	}
	if !domain.ValidID(organizationID) {
		return nil, errors.New("API organization ID must be a nonzero canonical UUID")
	}
	if len(bearerToken) < 32 || len(bearerToken) > 512 {
		return nil, errors.New("API bearer token must contain 32 to 512 ASCII characters")
	}
	for _, r := range bearerToken {
		if r < 0x21 || r > 0x7e {
			return nil, errors.New("API bearer token must contain only ASCII characters without whitespace or control characters")
		}
	}
	access, _ := repo.(domain.AccessRepository)
	jobs, _ := repo.(domain.JobRepository)
	providers, _ := repo.(domain.ProviderRepository)
	return &handler{repo: repo, access: access, jobs: jobs, providers: providers, organizationID: organizationID, tokenHash: sha256.Sum256([]byte(bearerToken))}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		w = headWriter{ResponseWriter: w}
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		principal, err := h.authenticate(r)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "A valid bearer token is required.")
			return
		case err != nil:
			writeError(w, http.StatusServiceUnavailable, "authentication_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		r = r.WithContext(domain.WithPrincipal(ctx, principal))
		if h.serveAccess(w, r, principal) || h.serveRuntime(w, r, principal) || h.serveResearch(w, r, principal) || h.serveContent(w, r, principal) {
			return
		}
		if (r.URL.Path == "/api/v1/sites" || strings.HasPrefix(r.URL.Path, "/api/v1/sites/")) && (r.Method == http.MethodPost || r.Method == http.MethodPut) && !principal.Role.CanWrite() {
			forbidden(w)
			return
		}
	}

	switch r.URL.Path {
	case "/healthz":
		if !readMethod(r.Method) {
			methodNotAllowed(w, "GET, HEAD")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/readyz":
		if !readMethod(r.Method) {
			methodNotAllowed(w, "GET, HEAD")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := h.repo.Ping(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Service is not ready.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	case "/api/v1/sites":
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.listSites(w, r)
		case http.MethodPost:
			h.createSite(w, r)
		default:
			methodNotAllowed(w, "GET, HEAD, POST")
		}
	default:
		const prefix = "/api/v1/sites/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			writeError(w, http.StatusNotFound, "not_found", "Resource not found.")
			return
		}
		siteID := strings.TrimPrefix(r.URL.Path, prefix)
		if siteID == "" || strings.Contains(siteID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "Resource not found.")
			return
		}
		if !readMethod(r.Method) && r.Method != http.MethodPut {
			methodNotAllowed(w, "GET, HEAD, PUT")
			return
		}
		if !domain.ValidID(siteID) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Site ID must be a nonzero canonical UUID.")
			return
		}
		if !checkQuery(w, r) {
			return
		}
		if r.Method == http.MethodPut {
			h.updateSite(w, r, siteID)
		} else {
			h.getSite(w, r, siteID)
		}
	}
}

func (h *handler) authenticate(r *http.Request) (domain.Principal, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return domain.Principal{}, domain.ErrNotFound
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || len(token) < 32 || len(token) > 512 {
		return domain.Principal{}, domain.ErrNotFound
	}
	digest := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(digest[:], h.tokenHash[:]) == 1 {
		return domain.Principal{OrganizationID: h.organizationID, Kind: "bootstrap", Role: domain.RoleOwner}, nil
	}
	if h.access == nil || len(token) != 68 || !strings.HasPrefix(token, "seo_") {
		return domain.Principal{}, domain.ErrNotFound
	}
	return h.access.Authenticate(r.Context(), digest)
}

func (h *handler) listSites(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := readPage(w, r)
	if !ok {
		return
	}
	sites, err := h.repo.ListSites(r.Context(), domain.PrincipalFromContext(r.Context()).OrganizationID, limit, offset)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if sites == nil {
		sites = []domain.Site{}
	}
	writeJSON(w, http.StatusOK, struct {
		Sites  []domain.Site `json:"sites"`
		Limit  int           `json:"limit"`
		Offset int           `json:"offset"`
	}{Sites: sites, Limit: limit, Offset: offset})
}

func (h *handler) getSite(w http.ResponseWriter, r *http.Request, siteID string) {
	site, err := h.repo.GetSite(r.Context(), domain.PrincipalFromContext(r.Context()).OrganizationID, siteID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeSite(w, http.StatusOK, site)
}

func (h *handler) createSite(w http.ResponseWriter, r *http.Request) {
	if !checkQuery(w, r) {
		return
	}
	var input *domain.SiteInput
	if !readJSON(w, r, &input) {
		return
	}
	if input == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Request body must be a JSON object.")
		return
	}
	normalized, err := domain.NormalizeSiteInput(*input)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	site, err := h.repo.CreateSite(r.Context(), domain.PrincipalFromContext(r.Context()).OrganizationID, normalized)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/sites/"+site.ID)
	writeSite(w, http.StatusCreated, site)
}

func (h *handler) updateSite(w http.ResponseWriter, r *http.Request, siteID string) {
	var input *struct {
		domain.SiteInput
		Version int64 `json:"version"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input == nil || input.Version < 1 {
		writeError(w, http.StatusBadRequest, "invalid_request", "A positive version is required for updates.")
		return
	}
	normalized, err := domain.NormalizeSiteInput(input.SiteInput)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	site, err := h.repo.UpdateSite(r.Context(), domain.PrincipalFromContext(r.Context()).OrganizationID, siteID, input.Version, normalized)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeSite(w, http.StatusOK, site)
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSONError(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSONError(w, err)
		return false
	}
	return true
}

func writeJSONError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds the 64 KiB limit.")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "Request body must contain one valid JSON object with supported fields.")
}

func checkQuery(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Query parameters must use valid URL encoding.")
		return false
	}
	for key, values := range query {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Query parameters must be supported and occur only once.")
			return false
		}
	}
	return true
}

func queryInt(r *http.Request, key string, fallback, minimum, maximum int) (int, error) {
	values, exists := r.URL.Query()[key]
	if !exists {
		return fallback, nil
	}
	n, err := strconv.Atoi(values[0])
	if err != nil || n < minimum || n > maximum {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", key, minimum, maximum)
	}
	return n, nil
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, "invalid_request", validation.Field+": "+validation.Message)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Site not found.")
	case errors.Is(err, domain.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "Site has changed. Reload it before updating.")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "A site with this base URL already exists in the organization.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func readMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
}

func writeSite(w http.ResponseWriter, status int, site domain.Site) {
	writeJSON(w, status, struct {
		Site domain.Site `json:"site"`
	}{Site: site})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Preserve normal HEAD semantics with both net/http and in-process callers.
type headWriter struct{ http.ResponseWriter }

func (w headWriter) Write(body []byte) (int, error) { return len(body), nil }

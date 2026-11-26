package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	testToken = "local-test-token-with-at-least-thirty-two-bytes"
	testOrgA  = "39ab2742-e317-4fb8-9e8d-b5c8b6b1edac"
	testOrgB  = "3ece6800-c4e5-4830-b18b-e83e090512c2"
	testSite  = "180ca823-d13a-443f-a136-d77b81bf947b"
	validSite = `{"name":"Example","base_url":"https://example.com/","cms":"wordpress","country_code":"US","language_tag":"en-US","status":"active"}`
)

type repositoryCall struct {
	method    string
	org       string
	id        string
	input     domain.SiteInput
	version   int64
	limit     int
	offset    int
	deadline  time.Time
	boundTime bool
}

type stubRepository struct {
	calls   []repositoryCall
	err     error
	pingErr error
	ping    func(context.Context) error
	sites   []domain.Site
}

func (s *stubRepository) record(ctx context.Context, c repositoryCall) {
	c.deadline, c.boundTime = ctx.Deadline()
	s.calls = append(s.calls, c)
}

func (s *stubRepository) Ping(ctx context.Context) error {
	s.record(ctx, repositoryCall{method: "ping"})
	if s.ping != nil {
		return s.ping(ctx)
	}
	return s.pingErr
}

func (s *stubRepository) CreateSite(ctx context.Context, org string, input domain.SiteInput) (domain.Site, error) {
	s.record(ctx, repositoryCall{method: "create", org: org, input: input})
	return domain.Site{ID: testSite, OrganizationID: org, Name: input.Name, BaseURL: input.BaseURL, Version: 1}, s.err
}

func (s *stubRepository) GetSite(ctx context.Context, org, id string) (domain.Site, error) {
	s.record(ctx, repositoryCall{method: "get", org: org, id: id})
	return domain.Site{ID: id, OrganizationID: org, Version: 1}, s.err
}

func (s *stubRepository) ListSites(ctx context.Context, org string, limit, offset int) ([]domain.Site, error) {
	s.record(ctx, repositoryCall{method: "list", org: org, limit: limit, offset: offset})
	return s.sites, s.err
}

func (s *stubRepository) UpdateSite(ctx context.Context, org, id string, version int64, input domain.SiteInput) (domain.Site, error) {
	s.record(ctx, repositoryCall{method: "update", org: org, id: id, version: version, input: input})
	return domain.Site{ID: id, OrganizationID: org, Name: input.Name, BaseURL: input.BaseURL, Version: version + 1}, s.err
}

func newTestHandler(t *testing.T, repo domain.Repository, org string) http.Handler {
	t.Helper()
	h, err := NewHandler(repo, org, testToken)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func request(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, expected, w.Body.String())
	}
}

func requireErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var response struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid error JSON: %v; %s", err, w.Body.String())
	}
	if response.Error.Code != code {
		t.Fatalf("error code = %q, want %q", response.Error.Code, code)
	}
}

func TestConstructorRejectsInvalidConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name  string
		repo  domain.Repository
		org   string
		token string
	}{
		{"missing repository", nil, testOrgA, testToken},
		{"invalid organization", &stubRepository{}, "any-tenant", testToken},
		{"zero organization", &stubRepository{}, "00000000-0000-0000-0000-000000000000", testToken},
		{"noncanonical organization", &stubRepository{}, strings.ToUpper(testOrgA), testToken},
		{"short token", &stubRepository{}, testOrgA, "short"},
		{"overlong token", &stubRepository{}, testOrgA, strings.Repeat("x", 513)},
		{"token whitespace", &stubRepository{}, testOrgA, testToken + " "},
		{"token control", &stubRepository{}, testOrgA, testToken + "\x00"},
		{"token unicode whitespace", &stubRepository{}, testOrgA, testToken + "\u2003"},
		{"token nonascii", &stubRepository{}, testOrgA, testToken + "中"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewHandler(tt.repo, tt.org, tt.token); err == nil {
				t.Fatal("expected invalid configuration to be rejected")
			}
		})
	}
}

func TestAuthenticationOnlyAcceptsSingleBearerHeader(t *testing.T) {
	for _, tt := range []struct {
		name    string
		headers []string
		path    string
		cookie  string
		status  int
	}{
		{"missing", nil, "/api/v1/sites", "", 401},
		{"wrong token", []string{"Bearer incorrect"}, "/api/v1/sites", "", 401},
		{"wrong scheme", []string{"Basic " + testToken}, "/api/v1/sites", "", 401},
		{"query token", nil, "/api/v1/sites?token=" + testToken, "", 401},
		{"cookie token", nil, "/api/v1/sites", "token=" + testToken, 401},
		{"duplicate headers", []string{"Bearer " + testToken, "Bearer " + testToken}, "/api/v1/sites", "", 401},
		{"extra whitespace", []string{"Bearer  " + testToken}, "/api/v1/sites", "", 401},
		{"valid", []string{"Bearer " + testToken}, "/api/v1/sites", "", 200},
		{"case insensitive scheme", []string{"bearer " + testToken}, "/api/v1/sites", "", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubRepository{}
			h := newTestHandler(t, repo, testOrgA)
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			for _, value := range tt.headers {
				r.Header.Add("Authorization", value)
			}
			r.Header.Set("Cookie", tt.cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			requireStatus(t, w, tt.status)
			if tt.status == 401 {
				requireErrorCode(t, w, "unauthorized")
				if len(repo.calls) != 0 {
					t.Fatal("unauthenticated request reached repository")
				}
				if w.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatal("missing bearer challenge")
				}
			}
			if strings.Contains(w.Body.String(), testToken) {
				t.Fatal("token was exposed in response")
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing response protection headers")
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("API unexpectedly enables cross-origin access")
			}
		})
	}
}

func TestCredentialOrganizationScopesEveryOperation(t *testing.T) {
	repo := &stubRepository{}
	for _, org := range []string{testOrgA, testOrgB} {
		h := newTestHandler(t, repo, org)
		for _, tt := range []struct {
			method string
			path   string
			body   string
			status int
		}{
			{"GET", "/api/v1/sites?limit=20&offset=3", "", 200},
			{"POST", "/api/v1/sites", validSite, 201},
			{"GET", "/api/v1/sites/" + testSite, "", 200},
			{"PUT", "/api/v1/sites/" + testSite, strings.TrimSuffix(validSite, "}") + `,"version":7}`, 200},
		} {
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Organization-ID", "another-organization")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			requireStatus(t, w, tt.status)
			call := repo.calls[len(repo.calls)-1]
			if call.org != org {
				t.Fatalf("%s organization = %q, want %q", tt.method, call.org, org)
			}
			if !call.boundTime || time.Until(call.deadline) > requestTimeout {
				t.Fatal("repository operation has no bounded request context")
			}
			if tt.method == "POST" && w.Header().Get("Location") != "/api/v1/sites/"+testSite {
				t.Fatal("create response has incorrect Location")
			}
			if tt.method == "PUT" && (call.id != testSite || call.version != 7) {
				t.Fatalf("update lost ID/version: %+v", call)
			}
			if call.method == "list" && (call.limit != 20 || call.offset != 3) {
				t.Fatalf("list pagination = %d, %d", call.limit, call.offset)
			}
		}
	}
}

func TestRequestValidationDoesNotReachRepository(t *testing.T) {
	for _, tt := range []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		status      int
	}{
		{"missing content type", "POST", "/api/v1/sites", validSite, "", 415},
		{"wrong content type", "POST", "/api/v1/sites", validSite, "text/plain", 415},
		{"empty body", "POST", "/api/v1/sites", "", "application/json", 400},
		{"invalid JSON", "POST", "/api/v1/sites", "{", "application/json", 400},
		{"null", "POST", "/api/v1/sites", "null", "application/json", 400},
		{"array", "POST", "/api/v1/sites", "[]", "application/json", 400},
		{"unknown field", "POST", "/api/v1/sites", `{"unrecognized":true}`, "application/json", 400},
		{"organization override", "POST", "/api/v1/sites", strings.TrimSuffix(validSite, "}") + `,"organization_id":"` + testOrgB + `"}`, "application/json", 400},
		{"multiple JSON documents", "POST", "/api/v1/sites", validSite + " {}", "application/json", 400},
		{"oversized body", "POST", "/api/v1/sites", `{"name":"` + strings.Repeat("x", maxBodyBytes) + `"}`, "application/json", 413},
		{"oversized trailing whitespace", "POST", "/api/v1/sites", validSite + strings.Repeat(" ", maxBodyBytes), "application/json", 413},
		{"invalid domain data", "POST", "/api/v1/sites", `{}`, "application/json", 400},
		{"invalid UUID", "GET", "/api/v1/sites/not-a-uuid", "", "", 400},
		{"noncanonical UUID", "GET", "/api/v1/sites/" + strings.ToUpper(testSite), "", "", 400},
		{"zero UUID", "GET", "/api/v1/sites/00000000-0000-0000-0000-000000000000", "", "", 400},
		{"missing update version", "PUT", "/api/v1/sites/" + testSite, validSite, "application/json", 400},
		{"negative update version", "PUT", "/api/v1/sites/" + testSite, strings.TrimSuffix(validSite, "}") + `,"version":-1}`, "application/json", 400},
		{"null update", "PUT", "/api/v1/sites/" + testSite, "null", "application/json", 400},
		{"update organization override", "PUT", "/api/v1/sites/" + testSite, strings.TrimSuffix(validSite, "}") + `,"version":1,"organization_id":"` + testOrgB + `"}`, "application/json", 400},
		{"query organization override", "GET", "/api/v1/sites?organization_id=" + testOrgB, "", "", 400},
		{"duplicate limit", "GET", "/api/v1/sites?limit=1&limit=2", "", "", 400},
		{"invalid query encoding", "GET", "/api/v1/sites?limit=%zz", "", "", 400},
		{"invalid query separator", "GET", "/api/v1/sites?limit=1;organization_id=other", "", "", 400},
		{"limit zero", "GET", "/api/v1/sites?limit=0", "", "", 400},
		{"limit over bound", "GET", "/api/v1/sites?limit=201", "", "", 400},
		{"limit overflow", "GET", "/api/v1/sites?limit=999999999999999999999", "", "", 400},
		{"offset negative", "GET", "/api/v1/sites?offset=-1", "", "", 400},
		{"offset over bound", "GET", "/api/v1/sites?offset=100001", "", "", 400},
		{"empty limit", "GET", "/api/v1/sites?limit=", "", "", 400},
		{"unexpected item query", "GET", "/api/v1/sites/" + testSite + "?limit=1", "", "", 400},
		{"unexpected create query", "POST", "/api/v1/sites?limit=1", validSite, "application/json", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubRepository{}
			h := newTestHandler(t, repo, testOrgA)
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			requireStatus(t, w, tt.status)
			if len(repo.calls) != 0 {
				t.Fatalf("invalid request reached repository: %+v", repo.calls)
			}
		})
	}
}

func TestEmptyListUsesArrayAndDefaultPagination(t *testing.T) {
	repo := &stubRepository{}
	w := request(newTestHandler(t, repo, testOrgA), "GET", "/api/v1/sites", "")
	requireStatus(t, w, 200)
	var response struct {
		Sites  []domain.Site `json:"sites"`
		Limit  int           `json:"limit"`
		Offset int           `json:"offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Sites == nil || len(response.Sites) != 0 || response.Limit != 50 || response.Offset != 0 {
		t.Fatalf("unexpected empty list: %s", w.Body.String())
	}
}

func TestRepositoryErrorsHaveStableCodesAndHideInternals(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"other tenant or missing", domain.ErrNotFound, 404, "not_found"},
		{"duplicate site", fmt.Errorf("wrapped: %w", domain.ErrConflict), 409, "conflict"},
		{"stale update", domain.ErrVersionConflict, 409, "version_conflict"},
		{"validation", &domain.ValidationError{Field: "name", Message: "required"}, 400, "invalid_request"},
		{"database failure", errors.New("postgres://admin:super-secret@internal-db:5432/private"), 500, "internal_error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubRepository{err: tt.err}
			w := request(newTestHandler(t, repo, testOrgA), "PUT", "/api/v1/sites/"+testSite, strings.TrimSuffix(validSite, "}")+`,"version":1}`)
			requireStatus(t, w, tt.status)
			requireErrorCode(t, w, tt.code)
			if strings.Contains(w.Body.String(), "super-secret") || strings.Contains(w.Body.String(), "internal-db") {
				t.Fatal("database details leaked in response")
			}
		})
	}
}

func TestHealthAndReadinessHaveDifferentDatabaseSemantics(t *testing.T) {
	repo := &stubRepository{pingErr: errors.New("database password secret")}
	h := newTestHandler(t, repo, testOrgA)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	requireStatus(t, w, 200)
	if len(repo.calls) != 0 {
		t.Fatal("liveness depends on database")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	requireStatus(t, w, 503)
	requireErrorCode(t, w, "not_ready")
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("readiness exposed database details")
	}
	if len(repo.calls) != 1 || !repo.calls[0].boundTime || time.Until(repo.calls[0].deadline) > readyTimeout {
		t.Fatal("readiness does not use a bounded database check")
	}
	repo.pingErr = nil
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	requireStatus(t, w, 200)

	repo.ping = func(ctx context.Context) error { return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil).WithContext(ctx))
	requireStatus(t, w, 503)
}

func TestMethodAndRouteResponses(t *testing.T) {
	h := newTestHandler(t, &stubRepository{}, testOrgA)
	for _, tt := range []struct {
		method string
		path   string
		status int
		allow  string
	}{
		{"DELETE", "/api/v1/sites/" + testSite, 405, "GET, HEAD, PUT"},
		{"PUT", "/api/v1/sites", 405, "GET, HEAD, POST"},
		{"POST", "/healthz", 405, "GET, HEAD"},
		{"POST", "/readyz", 405, "GET, HEAD"},
		{"GET", "/api/v1/sites/", 404, ""},
		{"GET", "/api/v1/sites/" + testSite + "/extra", 404, ""},
		{"GET", "/api/v1/unknown", 404, ""},
		{"HEAD", "/healthz", 200, ""},
		{"HEAD", "/api/v1/sites", 200, ""},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			w := request(h, tt.method, tt.path, "")
			requireStatus(t, w, tt.status)
			if w.Header().Get("Allow") != tt.allow {
				t.Fatalf("Allow = %q, want %q", w.Header().Get("Allow"), tt.allow)
			}
			if tt.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD response has a body")
			}
		})
	}
}

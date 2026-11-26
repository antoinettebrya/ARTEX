package executionsearch

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const testSite = "https://phase4.example.com/blog/"
const gscProperty = "sc-domain:phase4.example.com"
const sampleDay = "2026-11-06"

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func localClient(t *testing.T, handle http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handle)
	t.Cleanup(server.Close)
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Keep the logical Host header so tests distinguish the official API origins.
	client := server.Client()
	transport := client.Transport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		destination := req.Clone(req.Context())
		destination.URL.Scheme, destination.URL.Host = origin.Scheme, origin.Host
		return transport.RoundTrip(destination)
	})
	return New(client)
}

func response(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestGoogleOAuthConsentCodeExchangeAndRefresh(t *testing.T) {
	callback := "https://dashboard.example.com/seo/execution"
	config := domain.ExecutionConnection{ExecutionConnectionInput: domain.ExecutionConnectionInput{
		Kind: domain.ExecutionGSC, RedirectURI: callback, Enabled: true,
	}}
	secret := domain.ExecutionCredential{ClientID: "local-client.example", ClientSecret: "fixture-credential"}
	state := strings.Repeat("s", 32)
	verifier := strings.Repeat("v", 43)
	code := "fixture/consent+code"
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "oauth2.googleapis.com" || r.URL.Path != "/token" || r.Method != http.MethodPost ||
			r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
			t.Errorf("unexpected token exchange %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			http.Error(w, "bad form", 400)
			return
		}
		if r.Form.Get("client_id") != secret.ClientID || r.Form.Get("client_secret") != secret.ClientSecret {
			t.Error("OAuth client authentication did not reach the token endpoint")
		}
		switch calls.Add(1) {
		case 1:
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != code ||
				r.Form.Get("code_verifier") != verifier || r.Form.Get("redirect_uri") != callback {
				t.Error("OAuth authorization code fields are incomplete")
			}
			response(w, map[string]any{
				"access_token": "fixture-access-1", "refresh_token": "fixture-refresh",
				"expires_in": 3600, "token_type": "Bearer", "scope": gscScope,
			})
		case 2:
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "fixture-refresh" ||
				r.Form.Get("code") != "" || r.Form.Get("code_verifier") != "" {
				t.Error("OAuth refresh fields are incorrect")
			}
			response(w, map[string]any{
				"access_token": "fixture-access-2", "expires_in": 3600, "token_type": "Bearer",
			})
		default:
			t.Error("unexpected extra token request")
			http.Error(w, "extra", 500)
		}
	})

	auth, err := Authorize(config, secret, state, verifier)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(auth)
	if err != nil {
		t.Fatal(err)
	}
	challenge := sha256.Sum256([]byte(verifier))
	query := target.Query()
	if target.Scheme != "https" || target.Host != "accounts.google.com" || target.Path != "/o/oauth2/v2/auth" ||
		query.Get("state") != state || query.Get("scope") != gscScope || query.Get("redirect_uri") != callback ||
		query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) ||
		query.Get("code_challenge_method") != "S256" || query.Get("access_type") != "offline" ||
		strings.Contains(auth, secret.ClientSecret) || strings.Contains(auth, verifier) {
		t.Error("consent URL did not contain the expected public parameters")
	}
	config.Kind = domain.ExecutionGA4
	auth, err = Authorize(config, secret, state, verifier)
	target, _ = url.Parse(auth)
	if err != nil || target.Query().Get("scope") != gaScope {
		t.Error("GA4 did not request its read-only scope")
	}
	config.Kind = domain.ExecutionGSC
	if _, err := Authorize(config, secret, "too-short", verifier); err == nil {
		t.Error("accepted a short callback state")
	}
	if _, err := client.Exchange(t.Context(), config, secret, code, "too-short"); err == nil {
		t.Error("accepted a short PKCE verifier")
	}
	token, err := client.Exchange(t.Context(), config, secret, code, verifier)
	if err != nil || token.AccessToken != "fixture-access-1" || token.RefreshToken != "fixture-refresh" ||
		token.ExpiresAt == nil || time.Until(*token.ExpiresAt) < 59*time.Minute {
		t.Fatalf("invalid exchanged token, error = %v", err)
	}
	refreshed, err := client.Refresh(t.Context(), config, token)
	if err != nil || refreshed.AccessToken != "fixture-access-2" || refreshed.RefreshToken != token.RefreshToken {
		t.Fatalf("invalid refreshed token, error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("token requests = %d", calls.Load())
	}
}

func mockGSC(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	authenticated(t, r)
	if r.Host != "www.googleapis.com" {
		t.Errorf("unexpected Search Console host %q", r.Host)
	}
	scopePath := "/webmasters/v3/sites/" + url.PathEscape(gscProperty)
	switch r.URL.EscapedPath() {
	case "/webmasters/v3/sites":
		response(w, map[string]any{"siteEntry": []map[string]any{
			{"siteUrl": gscProperty, "permissionLevel": "siteOwner"},
			{"siteUrl": "sc-domain:foreign.example.com", "permissionLevel": "siteOwner"},
		}})
	case scopePath + "/sitemaps":
		response(w, map[string]any{"sitemap": []map[string]any{
			{"path": testSite + "sitemap.xml", "isPending": false,
				"lastDownloaded": "2026-11-05T14:20:00Z", "warnings": "2", "errors": 1},
			{"path": "https://foreign.example.com/sitemap.xml", "isPending": true},
		}})
	case scopePath + "/searchAnalytics/query":
		var request struct {
			From       string   `json:"startDate"`
			To         string   `json:"endDate"`
			Dimensions []string `json:"dimensions"`
			Type       string   `json:"type"`
			State      string   `json:"dataState"`
			Limit      int      `json:"rowLimit"`
			Offset     int      `json:"startRow"`
			Filters    []struct {
				Filters []struct {
					Dimension  string `json:"dimension"`
					Operator   string `json:"operator"`
					Expression string `json:"expression"`
				} `json:"filters"`
			} `json:"dimensionFilterGroups"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "request", 400)
			return
		}
		if request.From != sampleDay || request.To != sampleDay || request.Type != "web" ||
			request.State != "final" || request.Limit != reportPageSize || !slices.Equal(request.Dimensions, gscDimensions) ||
			len(request.Filters) != 1 || len(request.Filters[0].Filters) != 1 {
			t.Error("Search Console request did not preserve its data dimensions and scope")
		} else {
			filter := request.Filters[0].Filters[0]
			scope, err := regexp.Compile(filter.Expression)
			if err != nil || filter.Dimension != "page" || filter.Operator != "includingRegex" ||
				!scope.MatchString(testSite+"article") || scope.MatchString("https://phase4.example.com/blogx") ||
				scope.MatchString("https://foreign.example.com/blog/article") {
				t.Error("Search Console page regex did not enforce the registered path")
			}
		}
		makeRow := func(page string) map[string]any {
			return map[string]any{
				"keys":   []string{sampleDay, page, "approved keyword", "usa", "MOBILE"},
				"clicks": 3.0, "impressions": 10.0, "ctr": 0.3, "position": 2.5,
			}
		}
		switch request.Offset {
		case 0:
			rows := make([]map[string]any, reportPageSize)
			rows[0] = makeRow("https://foreign.example.com/blog/other")
			for i := 1; i < reportPageSize; i++ {
				rows[i] = makeRow(testSite + "article-" + strconv.Itoa(i))
			}
			response(w, map[string]any{"rows": rows})
		case reportPageSize:
			response(w, map[string]any{"rows": []any{makeRow(testSite + "last")}})
		default:
			t.Errorf("unexpected Search Console offset %d", request.Offset)
			http.Error(w, "offset", 400)
		}
	default:
		t.Errorf("unexpected Search Console path %q", r.URL.EscapedPath())
		http.NotFound(w, r)
	}
}

func TestGSCPaginationScopeAndLocalBudget(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) { mockGSC(t, w, r) })
	config := connection(domain.ExecutionGSC, gscProperty)
	full, err := client.Sync(t.Context(), config, googleCredential(), sampleDay, sampleDay, 4)
	if err != nil || full.Requests != 4 || full.Truncated || len(full.Metrics) != reportPageSize {
		t.Fatalf("GSC sync = (%d calls, %d metrics, truncated %t), error = %v", full.Requests, len(full.Metrics), full.Truncated, err)
	}
	if len(full.Properties) != 1 || full.Properties[0].ID != gscProperty ||
		len(full.Sitemaps) != 1 || full.Sitemaps[0].Warnings == nil || *full.Sitemaps[0].Warnings != 2 ||
		full.Sitemaps[0].Errors == nil || *full.Sitemaps[0].Errors != 1 {
		t.Fatalf("unexpected sitemap or property evidence: %#v %#v", full.Sitemaps, full.Properties)
	}
	last := full.Metrics[len(full.Metrics)-1]
	if last.Page != testSite+"last" || last.Values["position"] != 2.5 || last.Values["ctr"] != 0.3 {
		t.Fatalf("unexpected final Search Console row: %#v", last)
	}
	partial, err := client.Sync(t.Context(), config, googleCredential(), sampleDay, sampleDay, 3)
	if err != nil || partial.Requests != 3 || !partial.Truncated || len(partial.Metrics) != reportPageSize-1 {
		t.Fatalf("GSC budget result = (%d calls, %d rows), error = %v", partial.Requests, len(partial.Metrics), err)
	}
}

func TestGA4OrganicSessionsHostnameAndShortPages(t *testing.T) {
	var dataCalls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		authenticated(t, r)
		switch r.Host {
		case "analyticsadmin.googleapis.com":
			page := r.URL.Query().Get("pageToken")
			if r.URL.Path != "/v1beta/accountSummaries" || r.URL.Query().Get("pageSize") != "200" {
				t.Error("unexpected GA4 Admin endpoint")
			}
			items := []any{map[string]any{"propertySummaries": []any{map[string]any{
				"property": "properties/1234", "displayName": "Demo",
			}}}}
			if page == "" {
				otherSites := []any{map[string]any{"propertySummaries": []any{map[string]any{
					"property": "properties/9876", "displayName": "Different site",
				}}}}
				response(w, map[string]any{"accountSummaries": otherSites, "nextPageToken": "fixture-page"})
				return
			}
			if page != "fixture-page" {
				t.Error("unexpected GA4 account pagination token")
			}
			response(w, map[string]any{"accountSummaries": items})
		case "analyticsdata.googleapis.com":
			if r.URL.Path != "/v1beta/properties/1234:runReport" || r.Method != http.MethodPost {
				t.Error("unexpected GA4 Data endpoint")
			}
			var in map[string]any
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				http.Error(w, "request", 400)
				return
			}
			filters, _ := json.Marshal(in["dimensionFilter"])
			want := strconv.Itoa(int(dataCalls.Add(1) - 1))
			if in["offset"] != want || in["limit"] != "1000" || !strings.Contains(string(filters), "Organic Search") ||
				!strings.Contains(string(filters), "phase4.example.com") || !strings.Contains(string(filters), "FULL_REGEXP") {
				t.Errorf("GA4 filter or offset mismatch: %s, offset %v", filters, in["offset"])
			}
			values := []string{"20261106", "/blog/article", "United States", "mobile", "English", "phase4.example.com"}
			if want == "1" {
				values[5] = "foreign.example.com"
			}
			response(w, map[string]any{
				"dimensionHeaders": named(gaDimensions),
				"metricHeaders":    named(gaMetrics),
				"rows": []any{map[string]any{
					"dimensionValues": valued(values),
					"metricValues":    valued([]string{"11", "9", "5", "0.4545", "175.1", "3", "0.1818"}),
				}},
				"rowCount": 2, "metadata": map[string]any{"subjectToThresholding": true},
			})
		default:
			t.Errorf("unexpected GA4 host: %q", r.Host)
			http.NotFound(w, r)
		}
	})
	result, err := client.Sync(t.Context(), connection(domain.ExecutionGA4, "1234"), googleCredential(), sampleDay, sampleDay, 4)
	if err != nil || result.Requests != 4 || !result.Truncated || len(result.Properties) != 1 || len(result.Metrics) != 1 {
		t.Fatalf("invalid GA4 output: %#v, error = %v", result, err)
	}
	row := result.Metrics[0]
	if row.Page != testSite+"article" || row.Language != "English" || row.Date != sampleDay ||
		row.Values["sessions"] != 11 || row.Values["session_key_event_rate"] != 0.1818 {
		t.Fatalf("invalid normalized GA4 row: %#v", row)
	}
}

func TestBingJSONMetricsAndInspectionEvidence(t *testing.T) {
	key := domain.ExecutionCredential{Secret: "fixture-bing-key"}
	target := testSite + "story"
	config := connection(domain.ExecutionBing, testSite)
	day := "/Date(" + strconv.FormatInt(time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC).UnixMilli(), 10) + "-0700)/"
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "ssl.bing.com" || r.URL.Query().Get("apikey") != key.Secret || r.Header.Get("Authorization") != "" {
			t.Error("Bing query key reached an unexpected endpoint")
		}
		if r.URL.Path != "/webmaster/api.svc/json/GetUserSites" && r.Method == http.MethodGet &&
			r.URL.Query().Get("siteUrl") != testSite {
			t.Error("Bing API did not use the selected property")
		}
		var data any
		switch r.URL.Path {
		case "/webmaster/api.svc/json/GetUserSites":
			data = []any{
				map[string]any{"Url": testSite, "IsVerified": true},
				map[string]any{"Url": "https://foreign.example.com/", "IsVerified": true},
			}
		case "/webmaster/api.svc/json/GetRankAndTrafficStats":
			data = []any{map[string]any{"Date": day, "Clicks": 2, "Impressions": 10}}
		case "/webmaster/api.svc/json/GetQueryStats":
			data = []any{map[string]any{"Date": day, "Query": "sample", "Clicks": 4,
				"Impressions": 25, "AvgClickPosition": 1, "AvgImpressionPosition": 3}}
		case "/webmaster/api.svc/json/GetPageStats":
			data = []any{map[string]any{"Date": day, "Query": target, "Clicks": 2, "Impressions": 5}}
		case "/webmaster/api.svc/json/GetFeeds":
			data = []any{map[string]any{"Url": testSite + "sitemap.xml", "LastCrawled": day, "Status": "Success"}}
		case "/webmaster/api.svc/json/GetUrlInfo":
			if r.URL.Query().Get("url") != strconv.Quote(target) {
				t.Error("Bing URL Inspection did not encode the target")
			}
			data = map[string]any{"LastCrawledDate": day, "IsPage": true}
		case "/webmaster/api.svc/json/GetUrlSubmissionQuota":
			data = map[string]int{"DailyQuota": 250, "MonthlyQuota": 500}
		case "/webmaster/api.svc/json/SubmitUrlBatch":
			var input struct {
				Property string   `json:"siteUrl"`
				URLs     []string `json:"urlList"`
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&input) != nil ||
				input.Property != config.Property || !slices.Equal(input.URLs, []string{target}) {
				t.Error("Bing URL submission body mismatch")
			}
		default:
			t.Errorf("unexpected Bing path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		response(w, map[string]any{"d": data})
	})
	result, err := client.Sync(t.Context(), config, key, sampleDay, sampleDay, 5)
	if err != nil || result.Requests != 5 || result.Truncated || len(result.Properties) != 1 ||
		len(result.Metrics) != 3 || len(result.Sitemaps) != 1 || result.Sitemaps[0].Status != "Success" {
		t.Fatalf("invalid Bing output: %#v, error = %v", result, err)
	}
	if result.Metrics[0].Values["ctr"] != 0.2 || result.Metrics[1].Values["avg_impression_position"] != 3 ||
		result.Metrics[1].Query != "sample" || result.Metrics[2].Page != target {
		t.Fatalf("invalid normalized Bing rows: %#v", result.Metrics)
	}
	record, err := client.Inspect(t.Context(), config, key, target)
	if err != nil || record.Status != domain.IndexCrawled || record.LastCrawled == nil {
		t.Fatalf("Bing inspection overstated or missed crawl evidence: %#v, error = %v", record, err)
	}
	if status, err := client.SubmitStatus(t.Context(), config, key, []string{target}); err != nil || status != 200 {
		t.Fatalf("Bing submission status = %d, error = %v", status, err)
	}
}

func TestBingQuotaExhaustionSkipsSubmission(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/webmaster/api.svc/json/GetUrlSubmissionQuota" {
			t.Error("submitted URLs despite exhausted Bing quota")
		}
		response(w, map[string]any{"d": map[string]int{"DailyQuota": 0, "MonthlyQuota": 240}})
	})
	err := client.Submit(t.Context(), connection(domain.ExecutionBing, testSite), domain.ExecutionCredential{Secret: "fixture-bing-key"}, []string{testSite + "one"})
	var quota *domain.DataError
	if !errors.As(err, &quota) || quota.Code != "execution_upstream_quota" || !quota.Retry || quota.RetryAfter <= 0 {
		t.Fatalf("unexpected Bing quota result: %v", err)
	}
}

func TestURLInspectionGoogleVerdicts(t *testing.T) {
	for _, check := range []struct {
		verdict string
		crawl   string
		want    string
	}{
		{"PASS", "2026-11-05T09:00:00Z", domain.IndexIndexed},
		{"NEUTRAL", "", domain.IndexNotIndexed},
		{"VERDICT_UNSPECIFIED", "", domain.IndexUnknown},
		{"VERDICT_UNSPECIFIED", "2026-11-05T09:00:00Z", domain.IndexCrawled},
	} {
		t.Run(check.want, func(t *testing.T) {
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				authenticated(t, r)
				var body map[string]string
				if r.Host != "searchconsole.googleapis.com" || r.URL.Path != "/v1/urlInspection/index:inspect" ||
					r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil ||
					body["inspectionUrl"] != testSite+"article" || body["siteUrl"] != gscProperty {
					t.Error("unexpected Google inspection request")
				}
				response(w, map[string]any{"inspectionResult": map[string]any{"indexStatusResult": map[string]string{
					"verdict": check.verdict, "coverageState": "Fixture status", "lastCrawlTime": check.crawl,
				}}})
			})
			record, err := client.Inspect(t.Context(), connection(domain.ExecutionGSC, gscProperty),
				googleCredential(), testSite+"article")
			if err != nil || record.Status != check.want {
				t.Fatalf("got %#v, error = %v", record, err)
			}
		})
	}
}

func TestIndexNowReceiptScopeAndQuota(t *testing.T) {
	config := connection(domain.ExecutionIndexNow, "")
	config.KeyLocation = testSite + "catalog/fixture-key.txt"
	secret := domain.ExecutionCredential{Secret: "fixture-indexnow-key"}
	var requests atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		var data struct {
			Host string   `json:"host"`
			Key  string   `json:"key"`
			File string   `json:"keyLocation"`
			URLs []string `json:"urlList"`
		}
		if r.Host != "api.indexnow.org" || r.URL.Path != "/indexnow" || r.Method != http.MethodPost ||
			json.NewDecoder(r.Body).Decode(&data) != nil || data.Host != "phase4.example.com" ||
			data.Key != secret.Secret || data.File != config.KeyLocation ||
			!slices.Equal(data.URLs, []string{testSite + "catalog/article"}) {
			t.Error("IndexNow payload did not preserve its authorized scope")
		}
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if err := client.Submit(t.Context(), config, secret, []string{testSite + "different-directory/article"}); err == nil {
		t.Error("accepted a URL outside the key location scope")
	}
	if err := client.Submit(t.Context(), config, secret, []string{"https://phase4.example.com/blog-foreign/article"}); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Error("accepted a URL outside the registered site path")
	}
	if status, err := client.SubmitStatus(t.Context(), config, secret, []string{testSite + "catalog/article"}); err != nil || status != 202 {
		t.Fatalf("IndexNow receipt status = %d, error = %v", status, err)
	}
	err := client.Submit(t.Context(), config, secret, []string{testSite + "catalog/article"})
	var upstream *domain.DataError
	if !errors.As(err, &upstream) || !upstream.Retry || upstream.RetryAfter != 17*time.Second ||
		upstream.Code != "execution_upstream_quota" || requests.Load() != 2 {
		t.Fatalf("IndexNow quota error = %v", err)
	}
}

func TestUpstreamFailuresKeepQuotasAndHideSensitiveURLs(t *testing.T) {
	config := connection(domain.ExecutionGSC, gscProperty)
	t.Run("Google 403 quota", func(t *testing.T) {
		client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "8")
			w.WriteHeader(http.StatusForbidden)
			response(w, map[string]any{"error": map[string]any{"errors": []any{map[string]string{"reason": "dailyLimitExceeded"}}}})
		})
		result, err := client.Sync(t.Context(), config, googleCredential(), sampleDay, sampleDay, 5)
		var upstream *domain.DataError
		if result.Requests != 1 || !errors.As(err, &upstream) || !upstream.Retry || upstream.RetryAfter != 8*time.Second {
			t.Fatalf("Google quota error = %v, calls = %d", err, result.Requests)
		}
	})
	t.Run("Bing HTTP 400 API throttle", func(t *testing.T) {
		client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "12")
			w.WriteHeader(http.StatusBadRequest)
			response(w, map[string]any{"ErrorCode": 4, "Message": "must not reach the caller"})
		})
		result, err := client.Sync(t.Context(), connection(domain.ExecutionBing, testSite),
			domain.ExecutionCredential{Secret: "fixture-bing-key"}, sampleDay, sampleDay, 5)
		var upstream *domain.DataError
		if result.Requests != 1 || !errors.As(err, &upstream) || !upstream.Retry || upstream.RetryAfter != 12*time.Second ||
			upstream.Code != "execution_upstream_quota" {
			t.Fatalf("Bing quota error = %v, calls = %d", err, result.Requests)
		}
	})

	t.Run("metered transport", func(t *testing.T) {
		budget := &domain.DataError{Code: "execution_budget", Retry: true, RetryAfter: 2 * time.Minute}
		client := New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, budget
		})})
		_, err := client.Sync(t.Context(), config, googleCredential(), sampleDay, sampleDay, 5)
		if !errors.Is(err, budget) {
			t.Fatalf("metered error was lost: %v", err)
		}
	})
	t.Run("Bing hides query key", func(t *testing.T) {
		key := "fixture-private-bing-key"
		client := New(&http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, &url.Error{Op: "GET", URL: r.URL.String(), Err: errors.New("upstream failure")}
		})})
		_, err := client.Sync(t.Context(), connection(domain.ExecutionBing, testSite),
			domain.ExecutionCredential{Secret: key}, sampleDay, sampleDay, 1)
		if err == nil || strings.Contains(err.Error(), key) || err.Error() != "execution_upstream_transport" {
			t.Fatal("Bing error exposed transport internals or key")
		}
	})
	t.Run("redirect refused", func(t *testing.T) {
		var count atomic.Int32
		client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			http.Redirect(w, r, "https://foreign.example.com/path", http.StatusTemporaryRedirect)
		})
		_, err := client.Sync(t.Context(), config, googleCredential(), sampleDay, sampleDay, 5)
		if err == nil || count.Load() != 1 {
			t.Fatalf("redirect did not fail closed, error = %v", err)
		}
	})
}

func authenticated(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer "+googleCredential().AccessToken {
		t.Error("Google request is missing its scoped bearer credential")
	}
}

func connection(kind, property string) domain.ExecutionConnection {
	return domain.ExecutionConnection{ExecutionConnectionInput: domain.ExecutionConnectionInput{
		Kind: kind, BaseURL: testSite, Property: property, Enabled: true, AllowWrites: true,
	}}
}

func googleCredential() domain.ExecutionCredential {
	return domain.ExecutionCredential{AccessToken: "fixture-google-access"}
}

func named(items []string) []map[string]string  { return fields("name", items) }
func valued(items []string) []map[string]string { return fields("value", items) }

func fields(key string, values []string) []map[string]string {
	out := make([]map[string]string, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]string{key: value})
	}
	return out
}

package researchprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	login     = "fixture-reader@example.com"
	password  = "fixture-password-659"
	requestID = "45009b66-d112-44d4-92f2-459efea5b0fd"
)

var (
	usaEN = domain.Market{
		CountryCode: "US", LocationCode: 2840, LanguageCode: "en",
		SearchEngine: "google", Device: "desktop",
	}
	bingUS = domain.Market{
		CountryCode: "US", LocationCode: 2840, LanguageCode: "en",
		SearchEngine: "bing", Device: "desktop",
	}
	japanJA = domain.Market{
		CountryCode: "JP", LocationCode: 2392, LanguageCode: "ja",
		SearchEngine: "google", Device: "mobile",
	}
	franceFR = domain.Market{
		CountryCode: "FR", LocationCode: 2250, LanguageCode: "fr",
		SearchEngine: "bing", Device: "mobile",
	}
)

func TestDataForSEOKeywordHTTPContracts(t *testing.T) {
	cases := []struct {
		name, path, keyword string
		market              domain.Market
		op                  domain.DataOperation
		params              map[string]any
		want                []domain.KeywordMetric
		cost                int64
	}{
		{
			name: "google_related", path: "/v3/dataforseo_labs/google/related_keywords/live",
			market: usaEN, op: domain.DataRelated, keyword: "phone",
			params: map[string]any{"keyword": "phone", "depth": 1, "limit": 30, "include_seed_keyword": false},
			want: []domain.KeywordMetric{
				{Keyword: "android phone", SearchVolume: ptr(int64(1300)), CPCUSD: ptr(1.25), Competition: ptr(0.7), KeywordDifficulty: ptr(42), ProviderIntent: "commercial"},
				{Keyword: "phone dimensions"},
			},
			cost: 10200,
		},
		{
			name: "google_suggestions", path: "/v3/dataforseo_labs/google/keyword_suggestions/live",
			market: japanJA, op: domain.DataSuggestions, keyword: "携帯",
			params: map[string]any{"keyword": "携帯", "limit": 30},
			want: []domain.KeywordMetric{
				{Keyword: "携帯 比較", SearchVolume: ptr(int64(1600)), CPCUSD: ptr(0.0), Competition: ptr(0.0), ProviderIntent: "commercial"},
			},
			cost: 10100,
		},
		{
			name: "google_overview", path: "/v3/dataforseo_labs/google/keyword_overview/live",
			market: japanJA, op: domain.DataOverview, keyword: "携帯",
			params: map[string]any{"keywords": []string{"携帯"}},
			want: []domain.KeywordMetric{
				{Keyword: "携帯", SearchVolume: ptr(int64(0)), KeywordDifficulty: ptr(0), ProviderIntent: "informational"},
			},
			cost: 10100,
		},
		{
			name: "google_volume", path: "/v3/keywords_data/google_ads/search_volume/live",
			market: usaEN, op: domain.DataVolume, keyword: "buy laptop",
			params: map[string]any{"keywords": []string{"buy laptop"}, "search_partners": false},
			want: []domain.KeywordMetric{
				{Keyword: "buy laptop", SearchVolume: ptr(int64(2900)), CPCUSD: ptr(0.85), CompetitionIndex: ptr(100)},
			},
			cost: 90000,
		},
		{
			name: "bing_volume", path: "/v3/keywords_data/bing/search_volume/live",
			market: franceFR, op: domain.DataVolume, keyword: "café",
			params: map[string]any{"keywords": []string{"café"}, "device": "mobile", "search_partners": false},
			want: []domain.KeywordMetric{
				{Keyword: "café", SearchVolume: ptr(int64(1800)), Competition: ptr(0.5)},
			},
			cost: 90000,
		},
		{
			name: "bing_related", path: "/v3/dataforseo_labs/bing/related_keywords/live",
			market: bingUS, op: domain.DataRelated, keyword: "phone",
			params: map[string]any{"keyword": "phone", "depth": 1, "limit": 30, "include_seed_keyword": false},
			want: []domain.KeywordMetric{
				{Keyword: "android phone", SearchVolume: ptr(int64(220)), Competition: ptr(0.1), KeywordDifficulty: ptr(17)},
			},
			cost: 12120,
		},
		{
			name: "bing_difficulty", path: "/v3/dataforseo_labs/bing/bulk_keyword_difficulty/live",
			market: bingUS, op: domain.DataOverview, keyword: "dentist new york",
			params: map[string]any{"keywords": []string{"dentist new york"}},
			want: []domain.KeywordMetric{
				{Keyword: "dentist new york", KeywordDifficulty: ptr(65)},
			},
			cost: 12120,
		},
		{
			name: "bing_suggestions", path: "/v3/keywords_data/bing/keywords_for_keywords/live",
			market: franceFR, op: domain.DataSuggestions, keyword: "café",
			params: map[string]any{"keywords": []string{"café"}, "device": "mobile", "search_partners": false},
			want: []domain.KeywordMetric{
				{Keyword: "café en grains", SearchVolume: ptr(int64(1100)), CPCUSD: ptr(0.25), Competition: ptr(0.1)},
			},
			cost: 90000,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := domain.DataRequest{Operation: tt.op, Market: tt.market, Keyword: tt.keyword, RequestID: requestID}
			wantParams := withMarket(tt.params, tt.market)
			fixture := fixtureBytes(t, tt.name)
			p := fixtureProvider(t, tt.path, wantParams, fixture)
			plan, err := p.Plan(req)
			if err != nil || plan.ReserveMicros < tt.cost || plan.TTL != 7*24*time.Hour {
				t.Fatalf("bad plan %#v: %v", plan, err)
			}
			got, err := p.Fetch(context.Background(), req, testAccess())
			if err != nil {
				t.Fatal(err)
			}
			assertSnapshot(t, got, fixture, tt.path, tt.cost)
			if !reflect.DeepEqual(got.Keywords, tt.want) {
				t.Errorf("metrics = %#v, want %#v", got.Keywords, tt.want)
			}
		})
	}
}

func TestDataForSEOSERPHTTPContracts(t *testing.T) {
	for _, tt := range []struct {
		name, path, os string
		market         domain.Market
		cost           int64
	}{
		{"google_serp", "/v3/serp/google/organic/live/advanced", "windows", usaEN, 3500},
		{"bing_serp", "/v3/serp/bing/organic/live/advanced", "android", franceFR, 2000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			market := tt.market
			req := domain.DataRequest{
				Operation: domain.DataSERP, Market: market, Keyword: "C++ at 100%", RequestID: requestID,
			}
			wantParams := withMarket(map[string]any{
				"keyword": "C%2B%2B at 100%25", "device": market.Device, "os": tt.os, "depth": 20, "max_crawl_pages": 2,
			}, market)
			fixture := fixtureBytes(t, tt.name)
			p := fixtureProvider(t, tt.path, wantParams, fixture)
			plan, err := p.Plan(req)
			if err != nil || plan.ReserveMicros != 5000 || plan.TTL != 24*time.Hour {
				t.Fatalf("bad plan %#v: %v", plan, err)
			}
			got, err := p.Fetch(context.Background(), req, testAccess())
			if err != nil {
				t.Fatal(err)
			}
			assertSnapshot(t, got, fixture, tt.path, tt.cost)
			at := time.Date(2024, time.October, 1, 10, 11, 12, 0, time.UTC)
			if !got.CollectedAt.Equal(at) {
				t.Errorf("collected_at = %s, want %s", got.CollectedAt, at)
			}
			want := []domain.SERPEntry{
				{Rank: 1, URL: "https://www.example.com/learn", Domain: "www.example.com", Title: "Learn", Description: "A search result"},
				{Rank: 10, URL: "https://sample.org/topics", Domain: "sample.org", Title: "Topics"},
			}
			if !reflect.DeepEqual(got.Results, want) {
				t.Errorf("results = %#v, want %#v", got.Results, want)
			}
		})
	}
}

func TestDataForSEOOnPageHTTPContract(t *testing.T) {
	const pageURL = "https://competitor.example.com/guide"
	const path = "/v3/on_page/instant_pages"
	req := domain.DataRequest{
		Operation: domain.DataPage, Market: usaEN, Keyword: "phone", TargetURL: pageURL, RequestID: requestID,
	}
	fixture := fixtureBytes(t, "onpage_instant")
	p := fixtureProvider(t, path, map[string]any{"url": pageURL, "accept_language": "en", "validate_micromarkup": true}, fixture)
	got, err := p.Fetch(context.Background(), req, testAccess())
	if err != nil {
		t.Fatal(err)
	}
	assertSnapshot(t, got, fixture, path, 126)
	modification := time.Date(2025, time.March, 12, 10, 30, 22, 0, time.UTC)
	want := &domain.PageEvidence{
		URL: pageURL, Title: "Guide", Description: "Overview", CanonicalURL: pageURL, LastModified: &modification,
		Headings: []domain.HeadingEvidence{
			{Level: 1, Text: "Guide"}, {Level: 2, Text: "Coverage"}, {Level: 3, Text: "Details"},
			{Level: 4, Text: "Example"}, {Level: 5, Text: "Section"}, {Level: 6, Text: "Tip"},
		},
		Topics: []string{"phone", "battery"}, MicrodataObserved: true,
		Source: domain.ProviderDataForSEO + path,
	}
	if !reflect.DeepEqual(got.Page, want) {
		t.Errorf("page = %#v, want %#v", got.Page, want)
	}
	for _, resourceType := range []string{"redirect", "broken"} {
		t.Run(resourceType, func(t *testing.T) {
			captured := fmt.Sprintf(`{"status_code":20000,"cost":0.000125,"tasks":[{"id":"fixture-1","status_code":20000,"cost":0.000125,"result":[{"items":[{"resource_type":%q,"status_code":302,"url":%q}]}]}]}`, resourceType, pageURL)
			p := fixtureProvider(t, path, map[string]any{"url": pageURL, "accept_language": "en", "validate_micromarkup": true}, []byte(captured))
			response, err := p.Fetch(context.Background(), req, testAccess())
			requireDataError(t, err, "provider_page_unavailable", false, false, 0)
			if response.Page != nil || response.CostMicros == nil || *response.CostMicros != 125 {
				t.Fatalf("wrong unavailable page response %#v", response)
			}
		})
	}
}

func TestDataForSEOEndpointFailures(t *testing.T) {
	const rateLimited = `{"status_code":20000,"cost":0,"tasks":[{"id":"fixture-1","status_code":40202,"cost":0,"result":null}]}`
	const paidTimeout = `{"status_code":20000,"cost":0.0000001,"tasks":[{"id":"fixture-1","status_code":50401,"cost":0.0000011,"result":null}]}`
	for _, tt := range []struct {
		name, body, after, code string
		status                  int
		retry, unbilled         bool
		delay                   time.Duration
		cost                    *int64
	}{
		{"HTTP 429", `{"status_code":40202,"cost":0,"tasks":null}`, "3", "provider_http_429", 429, true, true, 3 * time.Second, ptr(int64(0))},
		{"HTTP 429 no receipt", "", "", "provider_http_429", 429, true, false, time.Minute, nil},
		{"live task 429", rateLimited, "", "provider_status_40202", 200, true, true, time.Minute, ptr(int64(0))},
		{"provider account cost limit", `{"status_code":40203,"cost":0,"tasks":null}`, "", "provider_status_40203", 200, false, true, 0, ptr(int64(0))},
		{"paid provider timeout", paidTimeout, "2", "provider_status_50401", 200, true, false, 2 * time.Second, ptr(int64(2))},
		{"invalid JSON", "invalid", "", "invalid_provider_response", 200, false, false, 0, nil},
		{"bad paid envelope", `{"status_code":20000,"cost":0.002,"tasks":[{"id":"fixture-1","status_code":20000,"result":{}}]}`, "", "invalid_provider_response", 200, false, false, 0, ptr(int64(2000))},
		{"wrong market", `{"status_code":20000,"cost":0.01,"tasks":[{"id":"fixture-1","status_code":20000,"data":{"location_code":2250},"result":null}]}`, "", "provider_market_mismatch", 200, false, false, 0, ptr(int64(10000))},
		{"body too big", strings.Repeat("x", domain.MaxProviderRawBytes+1), "", "provider_response_too_large", 200, false, false, 0, nil},
		{"bad credentials", `{"status_code":40100,"cost":0,"tasks":null}`, "", "provider_http_401", 401, false, true, 0, ptr(int64(0))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", tt.after)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			p := NewDataForSEO(server.URL, server.Client())
			got, err := p.Fetch(context.Background(), domain.DataRequest{Operation: domain.DataSERP, Keyword: "laptops", Market: usaEN}, testAccess())
			requireDataError(t, err, tt.code, tt.retry, tt.unbilled, tt.delay)
			if !reflect.DeepEqual(got.CostMicros, tt.cost) {
				t.Errorf("cost = %v, want %v", got.CostMicros, tt.cost)
			}
			if json.Valid([]byte(tt.body)) && !bytes.Equal(got.Raw, []byte(tt.body)) {
				t.Errorf("snapshot dropped on a provider error")
			}
		})
	}
}

func TestDataForSEOKeepsEmptyAndPartialSERPResults(t *testing.T) {
	const path = "/v3/serp/google/organic/live/advanced"
	for _, tt := range []struct {
		status int
		result string
		want   int
	}{
		{40102, "null", 0},
		{40106, `[{"items":[{"type":"organic","rank_group":3,"url":"https://www.example.org/"}]}]`, 1},
	} {
		t.Run(fmt.Sprint(tt.status), func(t *testing.T) {
			body := fmt.Sprintf(`{"status_code":20000,"cost":0.002,"tasks":[{"id":"fixture-1","status_code":%d,"result":%s}]}`, tt.status, tt.result)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			p := NewDataForSEO(server.URL, server.Client())
			got, err := p.Fetch(context.Background(), domain.DataRequest{Operation: domain.DataSERP, Keyword: "laptops", Market: usaEN}, testAccess())
			if err != nil || len(got.Results) != tt.want {
				t.Fatalf("result count = %d, err = %v", len(got.Results), err)
			}
			assertSnapshot(t, got, []byte(body), path, 2000)
		})
	}
}

func TestDataForSEOValidatesBeforeSending(t *testing.T) {
	for _, tt := range []struct {
		name string
		req  domain.DataRequest
		code string
	}{
		{"supplier does not offer Japanese Bing Ads", domain.DataRequest{Operation: domain.DataVolume, Keyword: "携帯", Market: domain.Market{
			CountryCode: "JP", LocationCode: 2392, LanguageCode: "ja", SearchEngine: "bing", Device: "mobile",
		}}, "unsupported_market"},
		{"Bing difficulty is US only", domain.DataRequest{Operation: domain.DataOverview, Keyword: "café", Market: franceFR}, "unsupported_market"},
		{"Bing related is US only", domain.DataRequest{Operation: domain.DataRelated, Keyword: "café", Market: franceFR}, "unsupported_market"},
		{"Google volume word limit", domain.DataRequest{Operation: domain.DataVolume, Keyword: strings.Repeat("p ", 11), Market: usaEN}, "invalid_keyword"},
		{"invalid market", domain.DataRequest{Operation: domain.DataSERP, Keyword: "phone"}, "invalid_market"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDataForSEO("http://127.0.0.1:1", nil)
			_, err := p.Plan(tt.req)
			requireDataError(t, err, tt.code, false, true, 0)
			_, err = p.Fetch(context.Background(), tt.req, testAccess())
			requireDataError(t, err, tt.code, false, true, 0)
		})
	}
	p := NewDataForSEO("https://credentials.example.com", nil)
	_, err := p.Fetch(context.Background(), domain.DataRequest{Operation: domain.DataSERP, Keyword: "phone", Market: usaEN}, testAccess())
	requireDataError(t, err, "invalid_provider_endpoint", false, true, 0)

	p = NewDataForSEO("", nil)
	for _, destination := range []string{
		"file:///document", "https://localhost/health", "https://intranet.local/guide",
		"https://127.0.0.1/", "https://[::1]/", "https://0177.0.0.1/",
		"https://public.example.com:8080/", "https://user@public.example.com/",
	} {
		req := domain.DataRequest{Operation: domain.DataPage, Market: usaEN, TargetURL: destination}
		_, err := p.Plan(req)
		requireDataError(t, err, "invalid_target_url", false, true, 0)
	}
}

func TestDataForSEODoesNotFollowRedirectOrPersistReflectedCredentials(t *testing.T) {
	var redirects atomic.Int32
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer redirect.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	req := domain.DataRequest{Operation: domain.DataSERP, Keyword: "phone", Market: usaEN}
	p := NewDataForSEO(server.URL, server.Client())
	_, err := p.Fetch(context.Background(), req, testAccess())
	requireDataError(t, err, "provider_http_307", false, false, 0)
	if redirects.Load() != 0 {
		t.Fatalf("redirected %d times", redirects.Load())
	}

	reflection := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"status_code":40100,"cost":0,"status_message":%q}`, r.Header.Get("Authorization"))
	}))
	defer reflection.Close()
	p = NewDataForSEO(reflection.URL, reflection.Client())
	got, err := p.Fetch(context.Background(), req, testAccess())
	requireDataError(t, err, "provider_reflected_credentials", false, true, 0)
	if len(got.Raw) > 0 {
		t.Fatal("raw snapshot may contain credential material")
	}
}

func TestDataForSEORequestTimeoutLeavesBillingUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "waiting")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	access := testAccess()
	access.Timeout = 80 * time.Millisecond
	p := NewDataForSEO(server.URL, server.Client())
	got, err := p.Fetch(context.Background(), domain.DataRequest{Operation: domain.DataSERP, Keyword: "phone", Market: usaEN}, access)
	var failure *domain.DataError
	if !errors.As(err, &failure) || failure.KnownUnbilled || !failure.Retry || got.CostMicros != nil {
		t.Fatalf("timeout must remain retryable with unknown billing: %v", err)
	}
}

func TestDataForSEOReserveAndDecimalCost(t *testing.T) {
	p := NewDataForSEO("", nil)
	plan, err := p.Plan(domain.DataRequest{Operation: domain.DataSERP, Market: usaEN, Keyword: "SITE:example.com product"})
	if err != nil || plan.ReserveMicros != 25000 {
		t.Fatalf("priced operators plan = %#v, err = %v", plan, err)
	}
	for _, tt := range []struct {
		amount json.Number
		want   *int64
	}{
		{"0", ptr(int64(0))}, {"0.075", ptr(int64(75000))}, {"1e-7", ptr(int64(1))},
		{"0.0201000000001", ptr(int64(20101))}, {"-0.1", nil}, {"1e2000", nil},
		{"1000000000000000", nil},
	} {
		if got := decimalMicros(&tt.amount); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("bad decimal conversion for %s", tt.amount)
		}
	}
	now := time.Date(2026, time.September, 1, 2, 3, 4, 0, time.UTC)
	if got := retryDelay(now.Add(11*time.Second).Format(http.TimeFormat), now); got != 11*time.Second {
		t.Errorf("Retry-After HTTP date = %s", got)
	}
}

func fixtureProvider(t *testing.T, path string, wantParams map[string]any, payload []byte) *DataForSEO {
	t.Helper()
	want, err := json.Marshal([]map[string]any{wantParams})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != path {
			t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, path)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != login || pass != password {
			t.Error("incorrect Basic credentials")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected JSON Content-Type")
		}
		var data []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Errorf("could not decode request JSON: %v", err)
		}
		got, err := json.Marshal(data)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("request = %s, want %s", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	return NewDataForSEO(server.URL, server.Client())
}

func withMarket(params map[string]any, market domain.Market) map[string]any {
	params["location_code"] = market.LocationCode
	params["language_code"] = market.LanguageCode
	params["tag"] = requestID
	return params
}

func assertSnapshot(t *testing.T, got domain.DataResponse, fixture []byte, path string, cost int64) {
	t.Helper()
	if !bytes.Equal(got.Raw, fixture) || got.TaskID != "fixture-1" ||
		got.Source != domain.ProviderDataForSEO+path || got.CollectedAt.IsZero() ||
		got.CostMicros == nil || *got.CostMicros != cost {
		t.Fatalf("invalid snapshot metadata: source %q, task %q, collected %s", got.Source, got.TaskID, got.CollectedAt)
	}
}

func requireDataError(t *testing.T, err error, code string, retry, unbilled bool, delay time.Duration) {
	t.Helper()
	var failure *domain.DataError
	if !errors.As(err, &failure) || failure.Code != code || failure.Retry != retry ||
		failure.KnownUnbilled != unbilled || failure.RetryAfter != delay {
		t.Fatalf("error = %#v, want code=%s retry=%t unbilled=%t delay=%s", err, code, retry, unbilled, delay)
	}
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testAccess() domain.DataAccess {
	return domain.DataAccess{Login: login, Password: password}
}

func ptr[T any](value T) *T { return &value }

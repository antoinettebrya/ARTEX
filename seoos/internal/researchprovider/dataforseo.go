package researchprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/providerhttp"
)

const (
	dataForSEOEndpoint = "https://api.dataforseo.com"
	keywordLimit       = 30
)

type DataForSEO struct {
	endpoint string
	client   http.Client
}

var _ domain.SEODataProvider = (*DataForSEO)(nil)

// NewDataForSEO uses the public API endpoint by default and only accepts
// loopback overrides for local contract tests.
func NewDataForSEO(endpoint string, client *http.Client) *DataForSEO {
	if endpoint == "" {
		endpoint = dataForSEOEndpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint != dataForSEOEndpoint && !localEndpoint(endpoint) {
		endpoint = ""
	}
	if client == nil {
		client = http.DefaultClient
	}
	isolated := *client
	isolated.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &DataForSEO{endpoint: endpoint, client: isolated}
}

func localEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

type dataCall struct {
	path   string
	params map[string]any
	plan   domain.DataPlan
	market domain.Market
}

func (p *DataForSEO) Plan(req domain.DataRequest) (domain.DataPlan, error) {
	if p.endpoint == "" {
		return domain.DataPlan{}, requestError("invalid_provider_endpoint")
	}
	call, err := prepareCall(req)
	return call.plan, err
}

func prepareCall(req domain.DataRequest) (dataCall, error) {
	market, err := req.Market.Normalize()
	if err != nil {
		return dataCall{}, requestError("invalid_market")
	}

	const keywordTTL = 7 * 24 * time.Hour
	call := dataCall{
		market: market,
		params: map[string]any{
			"location_code": market.LocationCode,
			"language_code": market.LanguageCode,
		},
	}
	pageOperation := isPageOperation(req.Operation)
	term := strings.TrimSpace(req.Keyword)
	if !pageOperation && (term == "" || !utf8.ValidString(term) ||
		strings.IndexFunc(term, unicode.IsControl) >= 0 || utf8.RuneCountInString(term) > 700) {
		return dataCall{}, requestError("invalid_keyword")
	}
	// The optional tag lets operators correlate a response with the job. Live
	// requests are billable calls and the tag does not deduplicate them upstream.
	if req.RequestID != "" && !pageOperation {
		if len(req.RequestID) > 255 || !utf8.ValidString(req.RequestID) || strings.IndexFunc(req.RequestID, unicode.IsControl) >= 0 {
			return dataCall{}, requestError("invalid_request_id")
		}
		call.params["tag"] = req.RequestID
	}

	switch req.Operation {
	case domain.DataRelated, domain.DataSuggestions, domain.DataOverview:
		method := string(req.Operation)
		if market.SearchEngine == "bing" {
			if req.Operation == domain.DataSuggestions {
				return bingKeywordCall(call, term, "keywords_for_keywords")
			}
			if market.CountryCode != "US" || market.LocationCode != 2840 || !strings.EqualFold(market.LanguageCode, "en") {
				return dataCall{}, requestError("unsupported_market")
			}
			call.params["language_code"] = "en"
			if req.Operation == domain.DataOverview {
				method = "bulk_keyword_difficulty"
			}
		}
		call.path = "/v3/dataforseo_labs/" + market.SearchEngine + "/" + method + "/live"
		call.plan = domain.DataPlan{ReserveMicros: 20000, TTL: keywordTTL}
		if req.Operation == domain.DataOverview {
			if utf8.RuneCountInString(term) > 80 || len(strings.Fields(term)) > 10 {
				return dataCall{}, requestError("invalid_keyword")
			}
			call.params["keywords"] = []string{term}
			call.plan.ReserveMicros = 30000
			return call, nil
		}
		call.params["keyword"] = term
		call.params["limit"] = keywordLimit
		if req.Operation == domain.DataRelated {
			call.params["depth"] = 1
			call.params["include_seed_keyword"] = false
		}
	case domain.DataVolume:
		if market.SearchEngine == "bing" {
			return bingKeywordCall(call, term, "search_volume")
		}
		if utf8.RuneCountInString(term) > 80 || len(strings.Fields(term)) > 10 {
			return dataCall{}, requestError("invalid_keyword")
		}
		call.path = "/v3/keywords_data/google_ads/search_volume/live"
		call.params["keywords"] = []string{term}
		call.params["search_partners"] = false
		call.plan = domain.DataPlan{ReserveMicros: 100000, TTL: keywordTTL}
	case domain.DataSERP:
		call.path = "/v3/serp/" + market.SearchEngine + "/organic/live/advanced"
		call.params["keyword"] = strings.NewReplacer("%", "%25", "+", "%2B").Replace(term)
		call.params["device"] = market.Device
		call.params["os"] = "windows"
		if market.Device == "mobile" {
			call.params["os"] = "android"
		}
		call.params["depth"] = 20
		call.params["max_crawl_pages"] = 2
		call.plan = domain.DataPlan{ReserveMicros: 5000, TTL: 24 * time.Hour}
		query := strings.ToLower(term)
		if strings.Contains(query, "cache:") {
			return dataCall{}, requestError("invalid_keyword")
		}
		for _, operator := range strings.Fields("allinanchor allintext allintitle allinurl define definition filetype id inanchor info intext intitle inurl link site") {
			if strings.Contains(query, operator+":") {
				call.plan.ReserveMicros = 25000
				break
			}
		}
	case domain.DataPage, domain.DataPageContent, domain.DataPageMicrodata:
		if _, ok := publicPageURL(req.TargetURL); !ok {
			return dataCall{}, requestError("invalid_target_url")
		}
		call.params = map[string]any{"url": req.TargetURL}
		call.plan = domain.DataPlan{ReserveMicros: 1000, TTL: 3 * 24 * time.Hour}
		switch req.Operation {
		case domain.DataPage:
			call.path = "/v3/on_page/instant_pages"
			call.params["accept_language"] = strings.ReplaceAll(market.LanguageCode, "_", "-")
			call.params["validate_micromarkup"] = true
		case domain.DataPageContent:
			call.path = "/v3/on_page/content_parsing/live"
			call.params["accept_language"] = strings.ReplaceAll(market.LanguageCode, "_", "-")
			call.params["markdown_view"] = false
		case domain.DataPageMicrodata:
			if !domain.ValidID(req.TargetTaskID) {
				return dataCall{}, requestError("invalid_provider_task_id")
			}
			call.path = "/v3/on_page/microdata"
			call.params["id"] = req.TargetTaskID
			// The provider documents free retrieval. Keep a minimal ledger
			// reservation so this request uses the same accounting path.
			call.plan.ReserveMicros = 1
		}
	default:
		return dataCall{}, requestError("unsupported_operation")
	}
	return call, nil
}

func isPageOperation(operation domain.DataOperation) bool {
	switch operation {
	case domain.DataPage, domain.DataPageContent, domain.DataPageMicrodata:
		return true
	default:
		return false
	}
}

func bingKeywordCall(call dataCall, term, method string) (dataCall, error) {
	lang := strings.ToLower(call.market.LanguageCode)
	if lang != "en" && lang != "fr" && lang != "de" {
		return dataCall{}, requestError("unsupported_market")
	}
	if utf8.RuneCountInString(term) > 100 {
		return dataCall{}, requestError("invalid_keyword")
	}
	call.path = "/v3/keywords_data/bing/" + method + "/live"
	call.params["keywords"] = []string{term}
	call.params["language_code"] = lang
	call.params["device"] = call.market.Device
	call.params["search_partners"] = false
	call.plan = domain.DataPlan{ReserveMicros: 100000, TTL: 7 * 24 * time.Hour}
	return call, nil
}

func publicPageURL(raw string) (*url.URL, bool) {
	u, err := domain.EvidenceURL(raw)
	if err != nil {
		return nil, false
	}
	if port := u.Port(); port != "" && !(port == "443" && u.Scheme == "https" || port == "80" && u.Scheme == "http") {
		return nil, false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	labels := strings.Split(host, ".")
	if len(labels) < 2 || len(host) > 253 {
		return nil, false
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return nil, false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return nil, false
			}
		}
	}
	tld := labels[len(labels)-1]
	if tld[0] < 'a' || tld[0] > 'z' {
		return nil, false
	}
	switch tld {
	case "localhost", "local", "localdomain", "internal", "lan", "intranet", "invalid", "test", "example", "onion", "arpa", "home", "corp":
		return nil, false
	}
	return u, true
}

type envelope struct {
	StatusCode int          `json:"status_code"`
	Cost       *json.Number `json:"cost"`
	Tasks      []apiTask    `json:"tasks"`
}

type apiTask struct {
	ID         string            `json:"id"`
	StatusCode int               `json:"status_code"`
	Cost       *json.Number      `json:"cost"`
	Data       marketEcho        `json:"data"`
	Result     []json.RawMessage `json:"result"`
}

func (p *DataForSEO) Fetch(ctx context.Context, req domain.DataRequest, access domain.DataAccess) (response domain.DataResponse, problem error) {
	start := time.Now()
	diagnostic := domain.ProviderReceipt{Provider: domain.ProviderDataForSEO, Operation: string(req.Operation), CostSource: "unknown"}
	defer func() {
		diagnostic.DurationMS = time.Since(start).Milliseconds()
		diagnostic.Category = providerhttp.HTTPCategory(diagnostic.HTTPStatus)
		diagnostic.MayBeCharged = true
		if response.CostMicros != nil {
			diagnostic.CostSource, diagnostic.CostMicros = "provider", response.CostMicros
			diagnostic.MayBeCharged = *response.CostMicros > 0
		}
		var typed *domain.DataError
		if errors.As(problem, &typed) {
			diagnostic.Code, diagnostic.Retryable = typed.Code, typed.Retry
			if typed.KnownUnbilled {
				diagnostic.MayBeCharged = false
			}
			switch {
			case strings.Contains(typed.Code, "timeout"):
				diagnostic.Category = "timeout"
			case strings.Contains(typed.Code, "transport"):
				diagnostic.Category = "transport"
			case diagnostic.HTTPStatus >= 200 && diagnostic.HTTPStatus < 300:
				diagnostic.Category = dataStatusCategory(diagnostic.UpstreamCode)
			}
		}
		response.Diagnostic = diagnostic
	}()
	call, err := prepareCall(req)
	if err != nil {
		return domain.DataResponse{}, err
	}
	if p.endpoint == "" {
		return domain.DataResponse{}, requestError("invalid_provider_endpoint")
	}
	if strings.TrimSpace(access.Login) == "" || strings.TrimSpace(access.Password) == "" ||
		strings.Contains(access.Login, ":") || strings.IndexFunc(access.Login+access.Password, unicode.IsControl) >= 0 {
		return domain.DataResponse{}, requestError("invalid_provider_credentials")
	}
	if ctx.Err() != nil {
		return domain.DataResponse{}, requestError("request_canceled")
	}
	timeout := access.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	timeout = min(timeout, 30*time.Second)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, err := json.Marshal([]map[string]any{call.params})
	if err != nil {
		return domain.DataResponse{}, requestError("invalid_provider_request")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+call.path, bytes.NewReader(payload))
	if err != nil {
		return domain.DataResponse{}, requestError("invalid_provider_request")
	}
	httpReq.SetBasicAuth(access.Login, access.Password)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	response = domain.DataResponse{Source: domain.ProviderDataForSEO + call.path}
	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		code := "provider_transport"
		if ctx.Err() != nil {
			code = "provider_timeout"
		}
		return response, &domain.DataError{Code: code, Retry: true}
	}
	defer httpResp.Body.Close()
	providerhttp.Response(&diagnostic, httpResp, access.Login, access.Password, httpReq.Header.Get("Authorization"))
	response.CollectedAt = time.Now().UTC()
	retryAfter := retryDelay(httpResp.Header.Get("Retry-After"), response.CollectedAt)
	raw, readErr := io.ReadAll(io.LimitReader(httpResp.Body, domain.MaxProviderRawBytes+1))
	if readErr != nil || len(raw) > domain.MaxProviderRawBytes {
		if err := httpFailure(httpResp.StatusCode, retryAfter, nil); err != nil {
			return response, err
		}
		if readErr != nil {
			return response, &domain.DataError{Code: "provider_response_read", Retry: true}
		}
		return response, &domain.DataError{Code: "provider_response_too_large"}
	}

	var body envelope
	decodeErr := json.Unmarshal(raw, &body)
	if body.StatusCode != 0 {
		diagnostic.UpstreamCode = strconv.Itoa(body.StatusCode)
	}
	response.CostMicros = costMicros(body.Cost, body.Tasks)
	if decodeErr != nil && response.CostMicros != nil && *response.CostMicros == 0 {
		response.CostMicros = nil
	}
	if len(body.Tasks) == 1 {
		response.TaskID = providerhttp.Identifier(body.Tasks[0].ID, access.Login, access.Password)
		if diagnostic.RequestID == "" {
			diagnostic.RequestID = response.TaskID
		}
		if body.StatusCode == 20000 && body.Tasks[0].StatusCode != 0 {
			diagnostic.UpstreamCode = strconv.Itoa(body.Tasks[0].StatusCode)
		}
	}
	if credentialsReflected(raw, access) {
		response.TaskID = ""
		return response, billedError("provider_reflected_credentials", response.CostMicros)
	}
	if json.Valid(raw) {
		response.Raw = raw
	}
	if err := httpFailure(httpResp.StatusCode, retryAfter, response.CostMicros); err != nil {
		return response, err
	}
	if decodeErr != nil || body.StatusCode == 0 {
		return response, billedError("invalid_provider_response", response.CostMicros)
	}
	if err := upstreamFailure(body.StatusCode, retryAfter, response.CostMicros); err != nil {
		return response, err
	}
	if body.StatusCode == 40102 && len(body.Tasks) == 0 {
		if isPageOperation(req.Operation) {
			return response, billedError("provider_page_unavailable", response.CostMicros)
		}
		return response, nil
	}
	if len(body.Tasks) != 1 || response.TaskID == "" || body.Tasks[0].StatusCode == 0 {
		return response, billedError("invalid_provider_response", response.CostMicros)
	}

	task := body.Tasks[0]
	if err := upstreamFailure(task.StatusCode, retryAfter, response.CostMicros); err != nil {
		return response, err
	}
	if !task.Data.matches(call) {
		return response, billedError("provider_market_mismatch", response.CostMicros)
	}
	if task.StatusCode == 40102 {
		if isPageOperation(req.Operation) {
			return response, billedError("provider_page_unavailable", response.CostMicros)
		}
		return response, nil
	}
	if err := readResult(&response, task, call, req); err != nil {
		response.Keywords, response.Results, response.Page = nil, nil, nil
		failure := billedError(err.Error(), response.CostMicros)
		if err == errCrawlPending {
			failure.Retry = true
			failure.RetryAfter = max(15*time.Second, retryAfter)
		}
		return response, failure
	}
	return response, nil
}

func credentialsReflected(raw []byte, access domain.DataAccess) bool {
	basic := base64.StdEncoding.EncodeToString([]byte(access.Login + ":" + access.Password))
	for _, secret := range []string{access.Login, access.Password, basic} {
		quoted, _ := json.Marshal(secret)
		if bytes.Contains(raw, quoted) || (len(secret) >= 8 && bytes.Contains(raw, quoted[1:len(quoted)-1])) {
			return true
		}
	}
	return false
}

func costMicros(total *json.Number, tasks []apiTask) *int64 {
	largest := decimalMicros(total)
	for _, task := range tasks {
		micros := decimalMicros(task.Cost)
		if micros != nil && (largest == nil || *micros > *largest) {
			largest = micros
		}
	}
	return largest
}

func decimalMicros(amount *json.Number) *int64 {
	if amount == nil || len(*amount) > 40 {
		return nil
	}
	s := amount.String()
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(s[i+1:])
		if err != nil || exponent < -30 || exponent > 30 {
			return nil
		}
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok || v.Sign() < 0 {
		return nil
	}
	v.Mul(v, big.NewRat(1000000, 1))
	units, remainder := new(big.Int), new(big.Int)
	units.QuoRem(v.Num(), v.Denom(), remainder)
	if remainder.Sign() > 0 {
		units.Add(units, big.NewInt(1))
	}
	if !units.IsInt64() {
		return nil
	}
	result := units.Int64()
	return &result
}

func requestError(code string) *domain.DataError {
	return &domain.DataError{Code: code, KnownUnbilled: true}
}

func billedError(code string, cost *int64) *domain.DataError {
	return &domain.DataError{Code: code, KnownUnbilled: cost != nil && *cost == 0}
}

func retryDelay(value string, now time.Time) time.Duration {
	const maxDelay = 24 * time.Hour
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(min(seconds, int64(maxDelay/time.Second))) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return min(retryAt.Sub(now), maxDelay)
	}
	return 0
}

func httpFailure(status int, retryAfter time.Duration, cost *int64) *domain.DataError {
	if cost == nil && status >= 400 && status <= 405 {
		zero := int64(0)
		cost = &zero
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusTooManyRequests:
		return throttled("provider_http_429", retryAfter, cost)
	case status == http.StatusRequestTimeout || status >= 500:
		err := billedError("provider_http_"+strconv.Itoa(status), cost)
		err.Retry, err.RetryAfter = true, retryAfter
		return err
	default:
		return billedError("provider_http_"+strconv.Itoa(status), cost)
	}
}

func dataStatusCategory(code string) string {
	switch code {
	case "40100":
		return "authentication"
	case "40104", "40201", "40207":
		return "permission"
	case "40202", "40209":
		return "rate_limit"
	case "40200", "40203", "40210":
		return "billing"
	case "50401", "50402":
		return "timeout"
	}
	if strings.HasPrefix(code, "5") {
		return "upstream"
	}
	return "response"
}

func upstreamFailure(status int, retryAfter time.Duration, cost *int64) *domain.DataError {
	switch status {
	case 20000, 40102, 40106:
		return nil
	case 40202, 40209:
		return throttled("provider_status_"+strconv.Itoa(status), retryAfter, cost)
	case 40203, 40205, 40206:
		// Account-level limits need intervention or a new billing window.
		return billedError("provider_status_"+strconv.Itoa(status), cost)
	case 40101, 40103, 50000, 50001, 50301, 50303, 50304, 50401, 50402:
		err := billedError("provider_status_"+strconv.Itoa(status), cost)
		err.Retry, err.RetryAfter = true, retryAfter
		return err
	default:
		return billedError("provider_status_"+strconv.Itoa(status), cost)
	}
}

func throttled(code string, delay time.Duration, cost *int64) *domain.DataError {
	err := billedError(code, cost)
	err.Retry = true
	if delay == 0 {
		delay = time.Minute
	}
	err.RetryAfter = delay
	return err
}

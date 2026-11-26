package executionsearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const bingHost = "ssl.bing.com"

var bingJSONDate = regexp.MustCompile("^/Date\\((-?[0-9]+)([+-][0-9]{4})?\\)/$")

func (s *report) bing(key, from, to string) error {
	base, _ := siteURL(s.config.BaseURL)
	property := s.config.Property
	if property != "" {
		if _, err := siteURL(property); err != nil || !targetWithin(base, property) {
			// Bing's query and site aggregates cannot be filtered by a path prefix.
			return failure("execution_property_scope_mismatch")
		}
	}
	var sites []struct {
		URL      string `json:"Url"`
		Verified *bool  `json:"IsVerified"`
	}
	if err := s.bingRequest(key, "GetUserSites", nil, &sites); err != nil {
		return err
	}
	found := false
	for _, site := range sites {
		if !targetWithin(base, site.URL) {
			continue
		}
		permission := ""
		if site.Verified != nil {
			permission = "unverified"
			if *site.Verified {
				permission = "verified"
			}
		}
		s.output.Properties = append(s.output.Properties, domain.ExecutionProperty{
			ID: site.URL, Name: site.URL, Permission: permission,
		})
		if targetWithin(base, site.URL) && strings.TrimRight(site.URL, "/") == strings.TrimRight(property, "/") &&
			(site.Verified == nil || *site.Verified) {
			found = true
		}
	}
	if property == "" {
		return nil
	}
	if !found {
		return failure("execution_bing_property_unavailable")
	}

	var feeds []struct {
		URL      string `json:"Url"`
		LastRead string `json:"LastCrawled"`
		Status   string `json:"Status"`
	}
	if err := s.bingRequest(key, "GetFeeds", url.Values{"siteUrl": {property}}, &feeds); err != nil {
		return err
	}
	for _, feed := range feeds {
		if targetWithin(base, feed.URL) {
			s.output.Sitemaps = append(s.output.Sitemaps, domain.ExecutionSitemap{
				URL: feed.URL, Provider: domain.ExecutionBing, LastRead: bingTime(feed.LastRead),
				Status: feed.Status,
			})
		}
	}
	if from == "" {
		return nil
	}

	for _, method := range []string{"GetRankAndTrafficStats", "GetQueryStats", "GetPageStats"} {
		if err := s.bingMetrics(base, key, property, method, from, to); err != nil {
			return err
		}
	}
	return nil
}

func (s *report) bingRequest(key, method string, params url.Values, output any) error {
	var data struct {
		Data json.RawMessage `json:"d"`
	}
	if err := s.request(http.MethodGet, bingEndpoint(key, method, params), nil, &data); err != nil {
		return err
	}
	return decodeBing(data.Data, output)
}

func (s *report) bingMetrics(base *url.URL, key, property, method, from, to string) error {
	var stats []struct {
		Date                  string   `json:"Date"`
		Query                 string   `json:"Query"`
		Clicks                *float64 `json:"Clicks"`
		Impressions           *float64 `json:"Impressions"`
		AvgClickPosition      *float64 `json:"AvgClickPosition"`
		AvgImpressionPosition *float64 `json:"AvgImpressionPosition"`
	}
	if err := s.bingRequest(key, method, url.Values{"siteUrl": {property}}, &stats); err != nil {
		return err
	}
	for _, row := range stats {
		sourceDate := bingTime(row.Date)
		if sourceDate == nil {
			return failure("execution_invalid_bing_date")
		}
		day := sourceDate.Format(time.DateOnly)
		if day < from || day > to {
			continue
		}
		metric := domain.ExecutionMetric{Date: day, Values: numericValues(map[string]*float64{
			"clicks": row.Clicks, "impressions": row.Impressions,
			"avg_click_position": row.AvgClickPosition, "avg_impression_position": row.AvgImpressionPosition,
		})}
		if row.Clicks != nil && row.Impressions != nil && *row.Impressions > 0 {
			metric.Values["ctr"] = *row.Clicks / *row.Impressions
		}
		switch method {
		case "GetQueryStats":
			if row.Query == "" {
				continue
			}
			metric.Query = row.Query
		case "GetPageStats":
			// Microsoft's REST schema puts the page URL in Query for this endpoint.
			if !targetWithin(base, row.Query) {
				continue
			}
			metric.Page = row.Query
		}
		s.addMetric(metric)
	}
	return nil
}

func (c *Client) inspectBing(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, target string) (domain.ExecutionIndexRecord, error) {
	base, _ := siteURL(config.BaseURL)
	property, err := siteURL(config.Property)
	if err != nil || !targetWithin(base, config.Property) || !targetWithin(property, target) {
		return domain.ExecutionIndexRecord{}, failure("execution_property_scope_mismatch")
	}
	if !validSecret(secret.Secret) {
		return domain.ExecutionIndexRecord{}, failure("execution_bing_key_required")
	}
	params := url.Values{
		"siteUrl": {config.Property},
		"url":     {strconv.Quote(target)},
	}
	var wire struct {
		Data json.RawMessage `json:"d"`
	}
	_, err = c.requestJSON(ctx, config, http.MethodGet, bingEndpoint(secret.Secret, "GetUrlInfo", params), "", nil, &wire)
	if err != nil {
		return domain.ExecutionIndexRecord{}, err
	}
	var result struct {
		LastCrawled string `json:"LastCrawledDate"`
		Discovery   string `json:"DiscoveryDate"`
	}
	if err := decodeBing(wire.Data, &result); err != nil {
		return domain.ExecutionIndexRecord{}, err
	}
	out := domain.ExecutionIndexRecord{
		URL: target, Provider: domain.ExecutionBing, Status: domain.IndexUnknown, ObservedAt: time.Now().UTC(),
	}
	if crawled := bingTime(result.LastCrawled); crawled != nil {
		date := crawled.UTC()
		out.LastCrawled = &date
		out.Status = domain.IndexCrawled
		out.Evidence = "Bing GetUrlInfo crawl timestamp"
	} else if bingTime(result.Discovery) != nil {
		out.Status = domain.IndexDiscovered
		out.Evidence = "Bing GetUrlInfo discovery timestamp"
	}
	return out, nil
}

func (c *Client) submitBing(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, base *url.URL, urls []string) (int, error) {
	if len(urls) > 500 {
		return 0, failure("execution_submission_limit")
	}
	property, err := siteURL(config.Property)
	if err != nil || !targetWithin(base, config.Property) {
		return 0, failure("execution_property_scope_mismatch")
	}
	for _, target := range urls {
		if !targetWithin(property, target) {
			return 0, failure("execution_property_scope_mismatch")
		}
	}
	if !validSecret(secret.Secret) {
		return 0, failure("execution_bing_key_required")
	}
	if err := c.checkBingSubmissionQuota(ctx, config, secret.Secret, len(urls)); err != nil {
		return 0, err
	}

	var reply struct {
		Data json.RawMessage `json:"d"`
	}
	status, err := c.requestJSON(ctx, config, http.MethodPost, bingEndpoint(secret.Secret, "SubmitUrlBatch", nil), "", map[string]any{
		"siteUrl": config.Property, "urlList": urls,
	}, &reply)
	if err != nil {
		return status, err
	}
	if string(reply.Data) != "null" {
		return status, failure("execution_invalid_bing_submission_receipt")
	}
	return status, nil
}

func (c *Client) checkBingSubmissionQuota(ctx context.Context, config domain.ExecutionConnection, key string, size int) error {
	endpoint := bingEndpoint(key, "GetUrlSubmissionQuota", url.Values{"siteUrl": {config.Property}})
	var data struct {
		Data json.RawMessage `json:"d"`
	}
	if _, err := c.requestJSON(ctx, config, http.MethodGet, endpoint, "", nil, &data); err != nil {
		return err
	}
	var quota struct {
		Daily *int `json:"DailyQuota"`
	}
	if err := decodeBing(data.Data, &quota); err != nil {
		return err
	}
	if quota.Daily == nil || *quota.Daily < 0 {
		return failure("execution_invalid_bing_quota_response")
	}
	if *quota.Daily < size {
		tomorrow := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		return quotaError(time.Until(tomorrow))
	}
	return nil
}

func bingAPIError(body io.Reader, delay time.Duration) error {
	var fault struct {
		Code int `json:"ErrorCode"`
	}
	if json.NewDecoder(io.LimitReader(body, 32<<10)).Decode(&fault) == nil {
		// Bing returns HTTP 400 for these ApiErrorCode values, including throttles.
		switch fault.Code {
		case 1:
			return &domain.DataError{Code: "execution_upstream_unavailable", Retry: true, RetryAfter: delay}
		case 3, 10:
			return failure("execution_upstream_unauthorized")
		case 4, 5:
			return quotaError(delay)
		case 6, 13, 14:
			return failure("execution_upstream_forbidden")
		case 11:
			return failure("execution_upstream_not_found")
		}
	}
	return failure("execution_upstream_rejected")
}

func bingEndpoint(key, method string, params url.Values) string {
	if params == nil {
		params = make(url.Values)
	}
	params.Set("apikey", key)
	return bingAPIURL + "/" + method + "?" + params.Encode()
}

func decodeBing(data json.RawMessage, out any) error {
	if len(data) == 0 || json.Unmarshal(data, out) != nil {
		return failure("execution_invalid_bing_response")
	}
	return nil
}

// The number in the WCF date is Unix milliseconds. Its optional offset is
// used for the report's calendar day, without shifting the underlying instant.
func bingTime(raw string) *time.Time {
	parts := bingJSONDate.FindStringSubmatch(raw)
	if len(parts) == 0 {
		result, err := time.Parse(time.RFC3339, raw)
		if err != nil || result.Year() < 1970 {
			return nil
		}
		return &result
	}
	millis, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || millis <= 0 {
		return nil
	}
	result := time.UnixMilli(millis).UTC()
	if parts[2] != "" {
		hours, _ := strconv.Atoi(parts[2][1:3])
		minutes, _ := strconv.Atoi(parts[2][3:5])
		if hours > 14 || minutes > 59 || (hours == 14 && minutes != 0) {
			return nil
		}
		offset := (hours*60 + minutes) * 60
		if parts[2][0] == '-' {
			offset = -offset
		}
		result = result.In(time.FixedZone(parts[2], offset))
	}
	if result.Year() < 1970 || result.Year() > 9999 {
		return nil
	}
	return &result
}

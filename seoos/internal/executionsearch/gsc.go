package executionsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

var gscDimensions = []string{"date", "page", "query", "country", "device"}

func (s *report) gsc(from, to string) error {
	base, _ := siteURL(s.config.BaseURL)
	property := s.config.Property
	if property != "" && !gscPropertyRelated(base, property) {
		return failure("execution_property_scope_mismatch")
	}

	var sites struct {
		Sites []struct {
			URL        string `json:"siteUrl"`
			Permission string `json:"permissionLevel"`
		} `json:"siteEntry"`
	}
	if err := s.request(http.MethodGet, googleWebmastersURL+"/sites", nil, &sites); err != nil {
		return err
	}
	for _, entry := range sites.Sites {
		if gscPropertyRelated(base, entry.URL) {
			s.output.Properties = append(s.output.Properties, domain.ExecutionProperty{
				ID: entry.URL, Name: entry.URL, Permission: entry.Permission,
			})
		}
	}
	if property == "" {
		return nil
	}
	endpoint := googleWebmastersURL + "/sites/" + url.PathEscape(property)
	if err := s.gscSitemaps(base, endpoint); err != nil {
		return err
	}
	if from == "" {
		return nil
	}

	day, _ := time.Parse(time.DateOnly, from)
	end, _ := time.Parse(time.DateOnly, to)
	for !day.After(end) {
		if err := s.gscDay(base, endpoint, day.Format(time.DateOnly)); err != nil {
			return err
		}
		day = day.AddDate(0, 0, 1)
	}
	return nil
}

func (s *report) gscDay(base *url.URL, endpoint, day string) error {
	for offset := 0; ; offset += reportPageSize {
		query := map[string]any{
			"startDate":       day,
			"endDate":         day,
			"dimensions":      gscDimensions,
			"aggregationType": "byPage",
			"type":            "web",
			"dataState":       "final",
			"rowLimit":        reportPageSize,
			"startRow":        offset,
			"dimensionFilterGroups": []any{map[string]any{
				"groupType": "and",
				"filters": []any{map[string]any{
					"dimension":  "page",
					"operator":   "includingRegex",
					"expression": siteRegexp(base),
				}},
			}},
		}
		var page struct {
			Aggregation string `json:"responseAggregationType"`
			Rows        []struct {
				Keys        []string `json:"keys"`
				Clicks      *float64 `json:"clicks"`
				Impressions *float64 `json:"impressions"`
				CTR         *float64 `json:"ctr"`
				Position    *float64 `json:"position"`
			} `json:"rows"`
		}
		if err := s.request(http.MethodPost, endpoint+"/searchAnalytics/query", query, &page); err != nil {
			return err
		}
		if page.Aggregation != "" && page.Aggregation != "byPage" {
			return failure("execution_invalid_gsc_aggregation")
		}
		for _, row := range page.Rows {
			if len(row.Keys) != len(gscDimensions) || !validWindow(row.Keys[0], row.Keys[0]) || row.Keys[0] == "" {
				return failure("execution_invalid_gsc_row")
			}
			if row.Keys[0] != day || !targetWithin(base, row.Keys[1]) {
				continue
			}
			values := numericValues(map[string]*float64{
				"clicks": row.Clicks, "impressions": row.Impressions, "ctr": row.CTR, "position": row.Position,
			})
			s.addMetric(domain.ExecutionMetric{
				Date: row.Keys[0], Page: row.Keys[1], Query: row.Keys[2],
				Country: row.Keys[3], Device: row.Keys[4], Values: values,
			})
		}
		if len(page.Rows) < reportPageSize {
			return nil
		}
		if offset+reportPageSize >= 50000 {
			s.output.Truncated = true
			return nil
		}
	}
}

func (s *report) gscSitemaps(base *url.URL, endpoint string) error {
	var data struct {
		Sitemaps []struct {
			URL      string       `json:"path"`
			Warnings *json.Number `json:"warnings"`
			Errors   *json.Number `json:"errors"`
			Pending  *bool        `json:"isPending"`
			LastRead string       `json:"lastDownloaded"`
		} `json:"sitemap"`
	}
	if err := s.request(http.MethodGet, endpoint+"/sitemaps", nil, &data); err != nil {
		return err
	}
	for _, entry := range data.Sitemaps {
		if !targetWithin(base, entry.URL) {
			continue
		}
		lastRead := rfc3339(entry.LastRead)
		status := "unknown"
		if entry.Pending != nil {
			status = "processed"
			if *entry.Pending {
				status = "pending"
			}
		} else if lastRead != nil {
			status = "processed"
		}
		s.output.Sitemaps = append(s.output.Sitemaps, domain.ExecutionSitemap{
			URL: entry.URL, Provider: domain.ExecutionGSC, Status: status,
			LastRead: lastRead, Warnings: count(entry.Warnings), Errors: count(entry.Errors),
		})
	}
	return nil
}

func (c *Client) inspectGoogle(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, target string) (domain.ExecutionIndexRecord, error) {
	var result struct {
		Inspection struct {
			Index *struct {
				Verdict   string `json:"verdict"`
				Coverage  string `json:"coverageState"`
				LastCrawl string `json:"lastCrawlTime"`
			} `json:"indexStatusResult"`
		} `json:"inspectionResult"`
	}
	if !validSecret(secret.AccessToken) {
		return domain.ExecutionIndexRecord{}, failure("execution_google_token_required")
	}
	u, _ := domain.EvidenceURL(target)
	if !gscPropertyContains(config.Property, u) {
		return domain.ExecutionIndexRecord{}, failure("execution_property_scope_mismatch")
	}
	_, err := c.requestJSON(ctx, config, http.MethodPost, googleInspectionURL, secret.AccessToken, map[string]string{
		"inspectionUrl": target, "siteUrl": config.Property, "languageCode": "en-US",
	}, &result)
	if err != nil {
		return domain.ExecutionIndexRecord{}, err
	}
	out := domain.ExecutionIndexRecord{
		URL: target, Provider: domain.ExecutionGSC, Status: domain.IndexUnknown, ObservedAt: time.Now().UTC(),
	}
	if evidence := result.Inspection.Index; evidence != nil {
		out.LastCrawled = rfc3339(evidence.LastCrawl)
		out.Evidence = evidence.Coverage
		switch evidence.Verdict {
		case "PASS":
			out.Status = domain.IndexIndexed
		case "FAIL", "NEUTRAL":
			out.Status = domain.IndexNotIndexed
		default:
			if out.LastCrawled != nil {
				out.Status = domain.IndexCrawled
			}
		}
	}
	return out, nil
}

func siteRegexp(base *url.URL) string {
	prefix := base.Scheme + "://" + base.Host + strings.TrimRight(base.EscapedPath(), "/")
	return "^" + regexp.QuoteMeta(prefix) + "([/?]|$)"
}

func gscPropertyRelated(base *url.URL, property string) bool {
	if gscPropertyContains(property, base) {
		return true
	}
	parsed, err := siteURL(property)
	return err == nil && targetWithin(base, parsed.String())
}

func gscPropertyContains(property string, target *url.URL) bool {
	if target == nil {
		return false
	}
	if host, ok := strings.CutPrefix(property, "sc-domain:"); ok {
		if !validDomain(host) {
			return false
		}
		actual := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
		want := strings.ToLower(host)
		return actual == want || strings.HasSuffix(actual, "."+want)
	}
	parsed, err := siteURL(property)
	return err == nil && targetWithin(parsed, target.String())
}

func validDomain(name string) bool {
	if len(name) > 253 || !strings.Contains(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range strings.ToLower(label) {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func rfc3339(raw string) *time.Time {
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil || value.Year() < 1970 {
		return nil
	}
	value = value.UTC()
	return &value
}

func count(raw *json.Number) *int64 {
	if raw == nil {
		return nil
	}
	n, err := raw.Int64()
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

func numericValues(values map[string]*float64) map[string]float64 {
	out := make(map[string]float64, len(values))
	for name, value := range values {
		if value != nil {
			out[name] = *value
		}
	}
	return out
}

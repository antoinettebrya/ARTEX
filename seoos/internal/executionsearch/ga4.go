package executionsearch

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

var (
	gaPropertyID  = regexp.MustCompile("^properties/[1-9][0-9]{0,18}$")
	gaWebStreamID = regexp.MustCompile("^[1-9][0-9]{0,18}$")
	gaDimensions  = []string{"date", "landingPage", "country", "deviceCategory", "language", "hostName"}
	gaMetrics     = []string{"sessions", "activeUsers", "engagedSessions", "engagementRate", "userEngagementDuration", "keyEvents", "sessionKeyEventRate"}
	gaMetricKeys  = map[string]string{
		"sessions": "sessions", "activeUsers": "active_users", "engagedSessions": "engaged_sessions",
		"engagementRate": "engagement_rate", "userEngagementDuration": "user_engagement_seconds",
		"keyEvents": "key_events", "sessionKeyEventRate": "session_key_event_rate",
	}
)

func (s *report) ga4(from, to string) error {
	property := s.config.Property
	if property != "" && !strings.HasPrefix(property, "properties/") {
		property = "properties/" + property
	}
	if property != "" && !gaPropertyID.MatchString(property) {
		return failure("execution_invalid_ga4_property")
	}
	if err := s.gaProperties(property); err != nil {
		return err
	}
	if property != "" && len(s.output.Properties) == 0 {
		return failure("execution_ga4_property_unavailable")
	}
	if property == "" || from == "" {
		return nil
	}
	base, _ := siteURL(s.config.BaseURL)
	var expectedCount *int
	quality := &domain.AnalyticsReportQuality{TimeZone: s.config.PropertyTimeZone, Empty: true}
	s.output.Quality = quality
	for offset := 0; ; {
		var data gaReport
		query := gaQuery(base, from, to, offset, s.config.PropertyWebStreamIDs)
		if s.config.Managed {
			query["returnPropertyQuota"] = true
		}
		if err := s.request(http.MethodPost, gaDataURL+"/"+property+":runReport", query, &data); err != nil {
			return err
		}
		if data.Metadata.TimeZone != "" {
			if _, err := time.LoadLocation(data.Metadata.TimeZone); err != nil || quality.TimeZone != "" && quality.TimeZone != data.Metadata.TimeZone {
				return failure("execution_ga4_timezone_changed")
			}
			quality.TimeZone = data.Metadata.TimeZone
		}
		if location, err := time.LoadLocation(quality.TimeZone); err == nil && quality.TimeZone != "" {
			quality.RecentWindow = to >= time.Now().In(location).AddDate(0, 0, -2).Format(time.DateOnly)
		}
		quality.Sampled = quality.Sampled || len(data.Metadata.Sampling) > 0
		quality.Thresholded = quality.Thresholded || data.Metadata.Threshold
		quality.DataLossFromOtherRow = quality.DataLossFromOtherRow || data.Metadata.Other
		quality.ProviderTruncated = quality.ProviderTruncated || len(data.Metadata.Truncation) > 0
		quality.Empty = quality.Empty && len(data.Rows) == 0
		if quality.Empty && data.Metadata.EmptyReason != "" {
			quality.Reason = "provider_report_empty"
		}
		if len(data.Quota) > 0 {
			quality.Quota = data.Quota
		}
		s.output.Truncated = s.output.Truncated || quality.Thresholded || quality.DataLossFromOtherRow || quality.Sampled || quality.ProviderTruncated
		if data.RowCount != nil && (*data.RowCount < 0 || expectedCount != nil && *expectedCount != *data.RowCount) {
			s.output.Truncated = true
		}
		if expectedCount == nil {
			expectedCount = data.RowCount
		}
		if err := s.recordGAReport(base, data, from, to); err != nil {
			return err
		}
		offset += len(data.Rows)
		if expectedCount != nil {
			if offset >= *expectedCount {
				return nil
			}
			if len(data.Rows) == 0 {
				s.output.Truncated = true
				return nil
			}
		} else if len(data.Rows) < reportPageSize {
			return nil
		}
	}
}

func (s *report) gaProperties(selected string) error {
	token := ""
	tokens := make(map[string]bool)
	properties := make(map[string]bool)
	for {
		params := url.Values{"pageSize": {"200"}}
		if token != "" {
			params.Set("pageToken", token)
		}
		var page struct {
			Accounts []struct {
				Properties []struct {
					Property string `json:"property"`
					Name     string `json:"displayName"`
				} `json:"propertySummaries"`
			} `json:"accountSummaries"`
			Next string `json:"nextPageToken"`
		}
		err := s.request(http.MethodGet, gaAdminURL+"/accountSummaries?"+params.Encode(), nil, &page)
		if err != nil {
			return err
		}
		for _, account := range page.Accounts {
			for _, property := range account.Properties {
				if !gaPropertyID.MatchString(property.Property) || properties[property.Property] ||
					(selected != "" && property.Property != selected) {
					continue
				}
				properties[property.Property] = true
				s.output.Properties = append(s.output.Properties, domain.ExecutionProperty{
					ID: property.Property, Name: property.Name,
				})
				if selected != "" {
					return nil
				}
			}
		}
		if page.Next == "" {
			return nil
		}
		if tokens[page.Next] || len(page.Next) > 8192 {
			return failure("execution_invalid_ga4_page_token")
		}
		tokens[page.Next] = true
		token = page.Next
	}
}

func gaQuery(base *url.URL, from, to string, offset int, webStreamIDs []string) map[string]any {
	dimensions, orders := make([]any, 0, len(gaDimensions)), make([]any, 0, len(gaDimensions))
	for _, name := range gaDimensions {
		dimensions = append(dimensions, map[string]string{"name": name})
		orders = append(orders, map[string]any{"dimension": map[string]string{"dimensionName": name}})
	}
	metrics := make([]any, 0, len(gaMetrics))
	for _, name := range gaMetrics {
		metrics = append(metrics, map[string]string{"name": name})
	}
	filters := []any{
		gaStringFilter("sessionDefaultChannelGroup", "EXACT", "Organic Search", true),
		gaStringFilter("hostName", "EXACT", base.Hostname(), false),
	}
	if prefix := strings.TrimRight(base.EscapedPath(), "/"); prefix != "" {
		filters = append(filters, gaStringFilter("landingPage", "FULL_REGEXP", regexp.QuoteMeta(prefix)+"(/.*)?", true))
	}
	if len(webStreamIDs) > 0 {
		filters = append(filters, map[string]any{"filter": map[string]any{
			"fieldName": "streamId", "inListFilter": map[string]any{"values": webStreamIDs},
		}})
	}
	return map[string]any{
		"dateRanges":      []map[string]string{{"startDate": from, "endDate": to}},
		"dimensions":      dimensions,
		"metrics":         metrics,
		"orderBys":        orders,
		"limit":           strconv.Itoa(reportPageSize),
		"offset":          strconv.Itoa(offset),
		"dimensionFilter": map[string]any{"andGroup": map[string]any{"expressions": filters}},
	}
}

func gaStringFilter(field, match, value string, caseSensitive bool) map[string]any {
	return map[string]any{"filter": map[string]any{
		"fieldName": field,
		"stringFilter": map[string]any{
			"matchType": match, "value": value, "caseSensitive": caseSensitive,
		},
	}}
}

type gaHeader struct {
	Name string `json:"name"`
}

type gaValue struct {
	Value string `json:"value"`
}

type gaReport struct {
	DimensionHeaders []gaHeader `json:"dimensionHeaders"`
	MetricHeaders    []gaHeader `json:"metricHeaders"`
	Rows             []struct {
		Dimensions []gaValue `json:"dimensionValues"`
		Metrics    []gaValue `json:"metricValues"`
	} `json:"rows"`
	RowCount *int            `json:"rowCount"`
	Quota    json.RawMessage `json:"propertyQuota"`
	Metadata struct {
		TimeZone    string            `json:"timeZone"`
		EmptyReason string            `json:"emptyReason"`
		Threshold   bool              `json:"subjectToThresholding"`
		Other       bool              `json:"dataLossFromOtherRow"`
		Sampling    []map[string]any  `json:"samplingMetadatas"`
		Truncation  []json.RawMessage `json:"dataTruncationReasons"`
	} `json:"metadata"`
}

func (s *report) recordGAReport(base *url.URL, data gaReport, from, to string) error {
	if len(data.Rows) == 0 {
		return nil
	}
	fields := make(map[string]int, len(data.DimensionHeaders))
	for index, header := range data.DimensionHeaders {
		fields[header.Name] = index
	}
	for _, name := range gaDimensions {
		if _, ok := fields[name]; !ok {
			return failure("execution_invalid_ga4_dimensions")
		}
	}
	for _, row := range data.Rows {
		if len(row.Dimensions) != len(data.DimensionHeaders) || len(row.Metrics) != len(data.MetricHeaders) {
			return failure("execution_invalid_ga4_row")
		}
		get := func(key string) string { return row.Dimensions[fields[key]].Value }
		if !strings.EqualFold(get("hostName"), base.Hostname()) {
			continue
		}
		date, err := time.Parse("20060102", get("date"))
		if err != nil {
			return failure("execution_invalid_ga4_date")
		}
		day := date.Format(time.DateOnly)
		if day < from || day > to {
			continue
		}
		landing := ""
		rawLanding := get("landingPage")
		if rawLanding != "" && rawLanding != "(not set)" && rawLanding != "(other)" {
			mapped, ok := gaLandingURL(base, rawLanding)
			if !ok {
				continue
			}
			landing = mapped
		} else if strings.TrimRight(base.Path, "/") != "" {
			continue
		}
		metrics := make(map[string]float64, len(row.Metrics))
		for index, header := range data.MetricHeaders {
			key, known := gaMetricKeys[header.Name]
			if !known || row.Metrics[index].Value == "" {
				continue
			}
			value, err := strconv.ParseFloat(row.Metrics[index].Value, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return failure("execution_invalid_ga4_metric")
			}
			metrics[key] = value
		}
		s.addMetric(domain.ExecutionMetric{
			Date: day, Page: landing, Country: get("country"), Device: get("deviceCategory"),
			Language: get("language"), Values: metrics,
		})
	}
	return nil
}

func gaLandingURL(base *url.URL, landing string) (string, bool) {
	if !strings.HasPrefix(landing, "/") || strings.HasPrefix(landing, "//") {
		return "", false
	}
	parsed, err := base.Parse(landing)
	if err != nil || !targetWithin(base, parsed.String()) {
		return "", false
	}
	return parsed.String(), true
}

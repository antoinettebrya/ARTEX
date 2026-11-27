package executionsearch

import (
	"context"
	"net/http"
	"net/url"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

// GSCPage reads either the independent daily aggregate or one page of query
// detail. Google's aggregate includes traffic omitted from query detail.
func (c *Client) GSCPage(ctx context.Context, connection domain.ExecutionConnection, secret domain.ExecutionCredential, day string, offset int, totals bool) ([]domain.ExecutionMetric, error) {
	base, err := siteURL(connection.BaseURL)
	if err != nil {
		return nil, err
	}
	if connection.Kind != domain.ExecutionGSC || !connection.Enabled || !gscPropertyRelated(base, connection.Property) {
		return nil, failure("execution_property_scope_mismatch")
	}
	if !validSecret(secret.AccessToken) {
		return nil, failure("execution_google_token_required")
	}
	if day == "" || !validWindow(day, day) || offset < 0 || offset >= domain.GSCDetailLimit || totals && offset != 0 {
		return nil, failure("execution_invalid_gsc_page")
	}
	dimensions, limit := gscDimensions, domain.GSCPageSize
	if totals {
		dimensions, limit = []string{"date"}, 1
	}
	query := map[string]any{
		"startDate": day, "endDate": day, "dimensions": dimensions, "aggregationType": "byPage",
		"type": "web", "dataState": "final", "rowLimit": limit, "startRow": offset,
		"dimensionFilterGroups": []any{map[string]any{
			"groupType": "and",
			"filters": []any{map[string]any{
				"dimension": "page", "operator": "includingRegex", "expression": siteRegexp(base),
			}},
		}},
	}
	var reply struct {
		Aggregation string `json:"responseAggregationType"`
		Rows        []struct {
			Keys        []string `json:"keys"`
			Clicks      *float64 `json:"clicks"`
			Impressions *float64 `json:"impressions"`
			CTR         *float64 `json:"ctr"`
			Position    *float64 `json:"position"`
		} `json:"rows"`
	}
	endpoint := googleWebmastersURL + "/sites/" + url.PathEscape(connection.Property) + "/searchAnalytics/query"
	if _, err := c.requestJSON(ctx, connection, http.MethodPost, endpoint, secret.AccessToken, query, &reply); err != nil {
		return nil, err
	}
	if reply.Aggregation != "byPage" && (reply.Aggregation != "" || len(reply.Rows) != 0) {
		return nil, failure("execution_invalid_gsc_aggregation")
	}
	if len(reply.Rows) > limit {
		return nil, failure("execution_invalid_gsc_page")
	}
	rows := make([]domain.ExecutionMetric, 0, len(reply.Rows))
	for _, row := range reply.Rows {
		if len(row.Keys) != len(dimensions) || row.Keys[0] != day || row.Clicks == nil || row.Impressions == nil ||
			*row.Clicks < 0 || *row.Impressions < *row.Clicks || row.CTR != nil && (*row.CTR < 0 || *row.CTR > 1) ||
			row.Position != nil && *row.Position < 0 {
			return nil, failure("execution_invalid_gsc_row")
		}
		metric := domain.ExecutionMetric{
			Date: day, Values: numericValues(map[string]*float64{
				"clicks": row.Clicks, "impressions": row.Impressions, "ctr": row.CTR, "position": row.Position,
			}),
		}
		if !totals {
			if !targetWithin(base, row.Keys[1]) {
				return nil, failure("execution_invalid_gsc_row")
			}
			metric.Page, metric.Query, metric.Country, metric.Device = row.Keys[1], row.Keys[2], row.Keys[3], row.Keys[4]
		}
		rows = append(rows, metric)
	}
	return rows, nil
}

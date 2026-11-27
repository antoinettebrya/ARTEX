package executionsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const maxDiscoveryRequests = 24

// DiscoverIntegration lists resources from the authorized account. A page token
// belongs to the selected service; Bing and GSC return their complete lists.
func (c *Client) DiscoverIntegration(ctx context.Context, access domain.IntegrationAccess, kind, token string) ([]domain.IntegrationResource, string, error) {
	if !access.Account.Allows(kind, false) || !access.Application.Enabled && access.Account.Mode == domain.IntegrationOAuth {
		return nil, "", domain.ErrExecutionAuthority
	}
	if len(token) > 8192 || token != "" && kind != domain.ExecutionGA4 {
		return nil, "", failure("integration_invalid_resource_page")
	}
	config := domain.ExecutionConnection{ExecutionConnectionInput: domain.ExecutionConnectionInput{Kind: kind}}
	bearer := access.Credential.AccessToken
	if access.Account.Mode == domain.IntegrationOAuth && !validSecret(bearer) {
		return nil, "", domain.ErrIntegrationRevoked
	}
	resources := make([]domain.IntegrationResource, 0)
	switch kind {
	case domain.ExecutionGSC:
		var list struct {
			Sites []struct {
				URL        string `json:"siteUrl"`
				Permission string `json:"permissionLevel"`
			} `json:"siteEntry"`
		}
		if _, err := c.requestJSON(ctx, config, http.MethodGet, googleWebmastersURL+"/sites", bearer, nil, &list); err != nil {
			return nil, "", err
		}
		for _, item := range list.Sites {
			if item.URL != "" {
				resources = append(resources, domain.IntegrationResource{Kind: kind, ID: item.URL, Name: item.URL, Permission: item.Permission})
			}
		}
	case domain.ExecutionGA4:
		params := url.Values{"pageSize": {"100"}}
		if token != "" {
			params.Set("pageToken", token)
		}
		var list struct {
			Accounts []struct {
				ID         string `json:"account"`
				Name       string `json:"displayName"`
				Properties []struct {
					ID   string `json:"property"`
					Name string `json:"displayName"`
				} `json:"propertySummaries"`
			} `json:"accountSummaries"`
			Next string `json:"nextPageToken"`
		}
		if _, err := c.requestJSON(ctx, config, http.MethodGet, gaAdminURL+"/accountSummaries?"+params.Encode(), bearer, nil, &list); err != nil {
			return nil, "", err
		}
		for _, account := range list.Accounts {
			for _, property := range account.Properties {
				if gaPropertyID.MatchString(property.ID) {
					resources = append(resources, domain.IntegrationResource{Kind: kind, ID: property.ID, Name: property.Name,
						ParentID: account.ID, ParentName: account.Name})
				}
			}
		}
		if list.Next == token && token != "" || len(list.Next) > 8192 {
			return nil, "", failure("integration_invalid_resource_page")
		}
		return resources, list.Next, nil
	case domain.ExecutionBing:
		if !hasBingCredential(access.Credential) {
			return nil, "", domain.ErrIntegrationRevoked
		}
		var response struct {
			Data json.RawMessage `json:"d"`
		}
		if _, err := c.requestJSON(ctx, config, http.MethodGet, bingEndpoint(access.Credential.Secret, "GetUserSites", nil), bearer, nil, &response); err != nil {
			return nil, "", err
		}
		var sites []struct {
			URL      string `json:"Url"`
			Verified *bool  `json:"IsVerified"`
		}
		if err := decodeBing(response.Data, &sites); err != nil {
			return nil, "", err
		}
		for _, item := range sites {
			if _, err := siteURL(item.URL); err != nil {
				continue
			}
			permission := "unknown"
			if item.Verified != nil {
				permission = "unverified"
				if *item.Verified {
					permission = "verified"
				}
			}
			resources = append(resources, domain.IntegrationResource{Kind: kind, ID: item.URL, Name: item.URL, URL: item.URL, Permission: permission})
		}
	}
	return resources, "", nil
}

func (c *Client) VerifyIntegrationResource(ctx context.Context, access domain.IntegrationAccess, site domain.Site, kind, id string) (domain.IntegrationResource, error) {
	var empty domain.IntegrationResource
	base, err := siteURL(site.BaseURL)
	if err != nil || !domain.ValidIntegrationText(id, 2048) || !access.Account.Allows(kind, false) {
		return empty, domain.ErrExecutionAuthority
	}
	if kind == domain.ExecutionGA4 {
		return c.verifyGAProperty(ctx, access, base, id)
	}
	resources, _, err := c.DiscoverIntegration(ctx, access, kind, "")
	if err != nil {
		return empty, err
	}
	return VerifyListedResource(site, kind, id, resources)
}

// VerifyListedResource checks one site against a directory fetched by the
// same account. Batch bindings share the provider response, not site scope.
func VerifyListedResource(site domain.Site, kind, id string, resources []domain.IntegrationResource) (domain.IntegrationResource, error) {
	base, err := siteURL(site.BaseURL)
	if err != nil || !domain.ValidIntegrationText(id, 2048) {
		return domain.IntegrationResource{}, domain.ErrExecutionAuthority
	}
	for _, resource := range resources {
		if resource.ID != id {
			continue
		}
		switch kind {
		case domain.ExecutionGSC:
			if gscPropertyRelated(base, id) && (resource.Permission == "siteOwner" || resource.Permission == "siteFullUser" || resource.Permission == "siteRestrictedUser") {
				return resource, nil
			}
		case domain.ExecutionBing:
			if targetWithin(base, id) && resource.Permission == "verified" {
				return resource, nil
			}
		}
	}
	return domain.IntegrationResource{}, failure("integration_resource_unavailable_or_out_of_scope")
}

func (c *Client) verifyGAProperty(ctx context.Context, access domain.IntegrationAccess, base *url.URL, id string) (domain.IntegrationResource, error) {
	var result domain.IntegrationResource
	if !gaPropertyID.MatchString(id) {
		return result, failure("execution_invalid_ga4_property")
	}
	token := access.Credential.AccessToken
	var property struct {
		ID       string `json:"name"`
		Name     string `json:"displayName"`
		Parent   string `json:"account"`
		TimeZone string `json:"timeZone"`
		Currency string `json:"currencyCode"`
	}
	config := domain.ExecutionConnection{}
	endpoint := gaAdminURL + "/" + id
	if _, err := c.requestJSON(ctx, config, http.MethodGet, endpoint, token, nil, &property); err != nil {
		return result, err
	}
	if _, err := time.LoadLocation(property.TimeZone); err != nil || property.ID != id || property.TimeZone == "" || property.TimeZone == "Local" {
		return result, failure("integration_invalid_ga4_property_response")
	}
	result = domain.IntegrationResource{Kind: domain.ExecutionGA4, ID: id, Name: property.Name, TimeZone: property.TimeZone,
		Currency: property.Currency, ParentID: property.Parent}
	seen := make(map[string]bool)
	params := url.Values{"pageSize": {"100"}}
	for range maxDiscoveryRequests {
		var list struct {
			Streams []struct {
				Name string `json:"name"`
				Web  struct {
					URL string `json:"defaultUri"`
				} `json:"webStreamData"`
			} `json:"dataStreams"`
			Next string `json:"nextPageToken"`
		}
		if _, err := c.requestJSON(ctx, config, http.MethodGet, endpoint+"/dataStreams?"+params.Encode(), token, nil, &list); err != nil {
			return result, err
		}
		for _, stream := range list.Streams {
			parsed, err := siteURL(stream.Web.URL)
			if err != nil || !targetWithin(parsed, base.String()) {
				continue
			}
			streamID, found := strings.CutPrefix(stream.Name, id+"/dataStreams/")
			if found && gaWebStreamID.MatchString(streamID) && !slices.Contains(result.WebStreamIDs, streamID) {
				result.StreamHosts = []string{base.Hostname()}
				result.WebStreamIDs = append(result.WebStreamIDs, streamID)
			}
		}
		if list.Next == "" {
			if !slices.Contains(result.StreamHosts, base.Hostname()) {
				return result, failure("integration_ga4_stream_scope_mismatch")
			}
			slices.Sort(result.WebStreamIDs)
			result.URL = strings.ToLower(base.Hostname())
			return result, nil
		}
		if seen[list.Next] || len(list.Next) > 8192 {
			return result, failure("integration_invalid_resource_page")
		}
		seen[list.Next] = true
		params.Set("pageToken", list.Next)
	}
	return result, failure("integration_discovery_request_limit")
}

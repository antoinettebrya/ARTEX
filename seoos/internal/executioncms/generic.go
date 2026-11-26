package executioncms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type generic struct {
	*client
	mu   sync.Mutex
	info *domain.CMSSiteInfo
}

func (g *generic) route(parts ...string) *url.URL {
	return g.endpoint(append([]string{"api", "artex-cms", "v1"}, parts...)...)
}

func (g *generic) GetSiteInfo(ctx context.Context) (domain.CMSSiteInfo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.info != nil {
		return *g.info, nil
	}
	var info domain.CMSSiteInfo
	result, err := g.readJSON(ctx, http.MethodGet, g.route("site"), nil, &info)
	if err != nil {
		return domain.CMSSiteInfo{}, err
	}
	if _, err := g.approved(info.URL); err != nil {
		return domain.CMSSiteInfo{}, err
	}
	info.Raw = result.body
	g.info = &info
	return info, nil
}

func (g *generic) TestConnection(ctx context.Context) (domain.CMSCapabilities, error) {
	info, err := g.GetSiteInfo(ctx)
	return info.Capabilities, err
}

type genericRows struct {
	Items *[]json.RawMessage `json:"items"`
}

func (g *generic) ListPages(ctx context.Context, kind, slug string, limit int) ([]domain.CMSPage, error) {
	if err := checkPageInput(domain.CMSPageInput{Resource: kind, Slug: slug}); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 25
	}
	if limit > maxListing {
		return nil, invalid("limit", "must not exceed 250 pages")
	}
	caps, err := g.TestConnection(ctx)
	if err != nil {
		return nil, err
	}
	if !caps.ReadPages {
		return nil, domain.ErrExecutionUnsupported
	}
	params := url.Values{"resource": {kind}, "limit": {strconv.Itoa(limit)}}
	if slug != "" {
		params.Set("slug", slug)
	}
	var rows genericRows
	result, err := g.readJSON(ctx, http.MethodGet, setQuery(g.route("pages"), params), nil, &rows)
	if err != nil {
		return nil, err
	}
	if rows.Items == nil || len(*rows.Items) > limit {
		return nil, &domain.DataError{Code: "cms_generic_list_shape"}
	}
	list := make([]domain.CMSPage, 0, len(*rows.Items))
	for _, row := range *rows.Items {
		page, err := g.decodePage(row, kind, "", result.status)
		if err != nil {
			return nil, err
		}
		if slug == "" || page.Slug == slug {
			list = append(list, page)
		}
	}
	return list, nil
}

func (g *generic) GetPage(ctx context.Context, kind, id string) (domain.CMSPage, error) {
	if _, err := resourceKind(kind); err != nil {
		return domain.CMSPage{}, err
	}
	if !validRemoteID(id) {
		return domain.CMSPage{}, invalid("remote_id", "invalid CMS item ID")
	}
	caps, err := g.TestConnection(ctx)
	if err != nil {
		return domain.CMSPage{}, err
	}
	if !caps.ReadPages {
		return domain.CMSPage{}, domain.ErrExecutionUnsupported
	}
	target := setQuery(g.route("pages", id), url.Values{"resource": {kind}})
	return g.pageRequest(ctx, http.MethodGet, target, kind, id, nil)
}

func (g *generic) CreatePage(ctx context.Context, in domain.CMSPageInput) (domain.CMSPage, error) {
	if in.Status != "" && in.Status != "draft" {
		return domain.CMSPage{}, invalid("status", "new content starts as a draft")
	}
	in.Status = "draft"
	return g.write(ctx, http.MethodPost, g.route("pages"), "", in, false)
}

func (g *generic) UpdatePage(ctx context.Context, id string, in domain.CMSPageInput) (domain.CMSPage, error) {
	if !validRemoteID(id) {
		return domain.CMSPage{}, invalid("remote_id", "invalid CMS item ID")
	}
	return g.write(ctx, http.MethodPatch, g.route("pages", id), id, in, false)
}

func (g *generic) PublishPage(ctx context.Context, id string, in domain.CMSPageInput) (domain.CMSPage, error) {
	if !validRemoteID(id) {
		return domain.CMSPage{}, invalid("remote_id", "invalid CMS item ID")
	}
	in, err := publishInput(in)
	if err != nil {
		return domain.CMSPage{}, err
	}
	return g.write(ctx, http.MethodPost, g.route("pages", id, "publish"), id, in, true)
}

func (g *generic) write(ctx context.Context, method string, target *url.URL, id string, in domain.CMSPageInput, publish bool) (domain.CMSPage, error) {
	if err := g.allowWrites(); err != nil {
		return domain.CMSPage{}, err
	}
	if err := checkPageInput(in); err != nil {
		return domain.CMSPage{}, err
	}
	caps, err := g.TestConnection(ctx)
	if err != nil {
		return domain.CMSPage{}, err
	}
	if !caps.WritePages || (publish || in.Status == "publish" || in.Status == "future") && !caps.Publish ||
		in.Status == "future" && !caps.Schedule {
		return domain.CMSPage{}, domain.ErrExecutionUnsupported
	}
	payload := map[string]any{"resource": in.Resource}
	if !publish || in.Title != "" || in.Body != "" {
		payload["title"], payload["body"], payload["excerpt"] = in.Title, in.Body, in.Excerpt
	}
	if in.Slug != "" {
		payload["slug"] = in.Slug
	}
	if in.Status != "" {
		payload["status"] = in.Status
	}
	if in.ScheduledAt != nil {
		payload["scheduled_at"] = in.ScheduledAt.UTC()
	}
	for _, field := range caps.SEOFields {
		switch field {
		case "seo_title":
			if !publish || in.SEOTitle != "" {
				payload[field] = in.SEOTitle
			}
		case "description":
			if !publish || in.Description != "" {
				payload[field] = in.Description
			}
		case "canonical":
			if !publish || in.Canonical != "" {
				payload[field] = in.Canonical
			}
		}
	}
	return g.pageRequest(ctx, method, target, in.Resource, id, payload)
}

func (g *generic) pageRequest(ctx context.Context, method string, target *url.URL, kind, id string, value any) (domain.CMSPage, error) {
	var raw json.RawMessage
	result, err := g.readJSON(ctx, method, target, value, &raw)
	if err != nil {
		return domain.CMSPage{}, err
	}
	return g.decodePage(raw, kind, id, result.status)
}

func (g *generic) decodePage(raw json.RawMessage, kind, id string, status int) (domain.CMSPage, error) {
	var page domain.CMSPage
	if json.Unmarshal(raw, &page) != nil || !validRemoteID(page.ID) || page.Resource != kind ||
		page.Status == "" || len(page.Body) > maxPageBody || id != "" && page.ID != id {
		return domain.CMSPage{}, &domain.DataError{Code: "cms_generic_edit_response"}
	}
	if page.URL != "" {
		if _, err := g.approved(page.URL); err != nil {
			return domain.CMSPage{}, err
		}
	}
	page.HTTPStatus, page.Raw = status, raw
	return page, nil
}

func (g *generic) GetMedia(ctx context.Context, limit int) ([]domain.CMSMedia, error) {
	if limit < 1 {
		limit = 25
	}
	limit = min(limit, 100)
	caps, err := g.TestConnection(ctx)
	if err != nil {
		return nil, err
	}
	if !caps.ReadMedia {
		return nil, domain.ErrExecutionUnsupported
	}
	var rows genericRows
	target := setQuery(g.route("media"), url.Values{"limit": {strconv.Itoa(limit)}})
	if _, err := g.readJSON(ctx, http.MethodGet, target, nil, &rows); err != nil {
		return nil, err
	}
	if rows.Items == nil || len(*rows.Items) > limit {
		return nil, &domain.DataError{Code: "cms_generic_list_shape"}
	}
	media := make([]domain.CMSMedia, 0, len(*rows.Items))
	for _, raw := range *rows.Items {
		item, err := genericMedia(raw)
		if err != nil {
			return nil, err
		}
		media = append(media, item)
	}
	return media, nil
}

func genericMedia(raw json.RawMessage) (domain.CMSMedia, error) {
	var media domain.CMSMedia
	if json.Unmarshal(raw, &media) != nil || !validRemoteID(media.ID) {
		return domain.CMSMedia{}, &domain.DataError{Code: "cms_generic_media_response"}
	}
	if _, err := domain.EvidenceURL(media.URL); err != nil {
		return domain.CMSMedia{}, &domain.DataError{Code: "cms_generic_media_response"}
	}
	media.Raw = raw
	return media, nil
}

func (g *generic) UploadMedia(ctx context.Context, filename, mediaType string, data []byte) (domain.CMSMedia, error) {
	if err := g.allowWrites(); err != nil {
		return domain.CMSMedia{}, err
	}
	caps, err := g.TestConnection(ctx)
	if err != nil {
		return domain.CMSMedia{}, err
	}
	if !caps.WriteMedia {
		return domain.CMSMedia{}, domain.ErrExecutionUnsupported
	}
	result, err := g.upload(ctx, g.route("media"), filename, mediaType, data)
	if err != nil {
		return domain.CMSMedia{}, err
	}
	return genericMedia(result.body)
}

func (g *generic) GetSitemap(ctx context.Context) (domain.CMSSitemap, error) {
	target := g.route("sitemap")
	value, err := g.sitemap(ctx, target, true)
	if isNotFound(err) {
		return domain.CMSSitemap{}, domain.ErrExecutionUnsupported
	}
	return domain.CMSSitemap{URL: target.String(), XML: value}, err
}

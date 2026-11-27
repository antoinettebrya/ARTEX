package executioncms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type wordpress struct {
	*client
	mu     sync.Mutex
	cached *wordPressDetails
}

type wpRoute struct {
	Methods []string `json:"methods"`
}

type wpRoot struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	URL         string             `json:"url"`
	Home        string             `json:"home"`
	Namespaces  []string           `json:"namespaces"`
	Routes      map[string]wpRoute `json:"routes"`
}

type wpUser struct {
	ID           int64           `json:"id"`
	Capabilities map[string]bool `json:"capabilities"`
}

type metaField struct {
	Type       json.RawMessage      `json:"type"`
	ReadOnly   bool                 `json:"readonly"`
	Context    []string             `json:"context"`
	Properties map[string]metaField `json:"properties"`
}

type wpOptions struct {
	Schema metaField `json:"schema"`
}

type seoNames struct{ title, description, canonical string }

var yoastNames = seoNames{"_yoast_wpseo_title", "_yoast_wpseo_metadesc", "_yoast_wpseo_canonical"}
var rankMathNames = seoNames{"rank_math_title", "rank_math_description", "rank_math_canonical_url"}

type wordPressDetails struct {
	info domain.CMSSiteInfo
	root wpRoot
	user wpUser
	meta map[string]seoNames
}

func (w *wordpress) route(parts ...string) *url.URL {
	return w.endpoint(append([]string{"wp-json", "wp", "v2"}, parts...)...)
}

func setQuery(u *url.URL, fields url.Values) *url.URL {
	u.RawQuery = fields.Encode()
	return u
}

func routeAllows(root wpRoot, collection, verb string) bool {
	return slices.Contains(root.Routes["/wp/v2/"+collection].Methods, verb)
}

func (w *wordpress) details(ctx context.Context) (*wordPressDetails, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cached != nil {
		return w.cached, nil
	}
	d := &wordPressDetails{meta: make(map[string]seoNames)}
	rootURL := w.endpoint("wp-json")
	rootURL.Path += "/"
	result, err := w.readJSON(ctx, http.MethodGet, rootURL, nil, &d.root)
	if err != nil {
		return nil, err
	}
	me := setQuery(w.route("users", "me"), url.Values{"context": {"edit"}, "_fields": {"id,capabilities"}})
	if _, err := w.readJSON(ctx, http.MethodGet, me, nil, &d.user); err != nil {
		return nil, err
	}
	if d.user.ID < 1 || d.user.Capabilities == nil || !slices.Contains(d.root.Namespaces, "wp/v2") {
		return nil, &domain.DataError{Code: "cms_wordpress_identity"}
	}
	home := d.root.Home
	if home == "" {
		home = d.root.URL
	}
	if _, err := w.approved(home); err != nil {
		return nil, err
	}
	caps := domain.CMSCapabilities{
		ReadPages:  routeAllows(d.root, "posts", "GET") || routeAllows(d.root, "pages", "GET"),
		WritePages: canEdit(d, "posts") || canEdit(d, "pages"),
		Publish:    canPublish(d, "posts") || canPublish(d, "pages"),
		ReadMedia:  routeAllows(d.root, "media", "GET"),
		WriteMedia: routeAllows(d.root, "media", "POST") && d.user.Capabilities["upload_files"],
	}
	caps.Schedule = caps.Publish
	switch {
	case slices.Contains(d.root.Namespaces, "yoast/v1") && slices.Contains(d.root.Namespaces, "rankmath/v1"):
		caps.SEOPlugin = "yoast+rank_math"
	case slices.Contains(d.root.Namespaces, "yoast/v1"):
		caps.SEOPlugin = "yoast"
	case slices.Contains(d.root.Namespaces, "rankmath/v1"):
		caps.SEOPlugin = "rank_math"
	}
	var candidate seoNames
	switch caps.SEOPlugin {
	case "yoast":
		candidate = yoastNames
	case "rank_math":
		candidate = rankMathNames
	}
	if candidate != (seoNames{}) {
		for _, collection := range []string{"posts", "pages"} {
			if !canEdit(d, collection) {
				continue
			}
			var options wpOptions
			if _, err := w.readJSON(ctx, http.MethodOptions, w.route(collection), nil, &options); err != nil {
				if isNotFound(err) {
					continue
				}
				return nil, err
			}
			meta := options.Schema.Properties["meta"]
			actual := seoNames{}
			if !meta.ReadOnly {
				actual = seoNames{
					writableName(meta, candidate.title),
					writableName(meta, candidate.description),
					writableName(meta, candidate.canonical),
				}
			}
			for _, field := range []string{actual.title, actual.description, actual.canonical} {
				if field != "" && !slices.Contains(caps.SEOFields, field) {
					caps.SEOFields = append(caps.SEOFields, field)
				}
			}
			d.meta[collection] = actual
		}
	}
	d.info = domain.CMSSiteInfo{
		Name: d.root.Name, Description: d.root.Description, URL: home, Capabilities: caps, Raw: result.body,
	}
	w.cached = d
	return d, nil
}

func writableName(meta metaField, name string) string {
	f, ok := meta.Properties[name]
	if !ok || f.ReadOnly || string(f.Type) != `"string"` ||
		len(f.Context) > 0 && !slices.Contains(f.Context, "edit") {
		return ""
	}
	return name
}

func canEdit(d *wordPressDetails, collection string) bool {
	return routeAllows(d.root, collection, "POST") && d.user.Capabilities["edit_"+collection]
}

func canPublish(d *wordPressDetails, collection string) bool {
	return canEdit(d, collection) && d.user.Capabilities["publish_"+collection]
}

func (w *wordpress) TestConnection(ctx context.Context) (domain.CMSCapabilities, error) {
	d, err := w.details(ctx)
	if err != nil {
		return domain.CMSCapabilities{}, err
	}
	return d.info.Capabilities, nil
}

type wpTerm struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (w *wordpress) terms(ctx context.Context, d *wordPressDetails, collection string) ([]domain.CMSTerm, error) {
	if !routeAllows(d.root, collection, "GET") {
		return []domain.CMSTerm{}, nil
	}
	var rows []wpTerm
	target := setQuery(w.route(collection), url.Values{"per_page": {"100"}, "_fields": {"id,name,slug"}})
	if _, err := w.readJSON(ctx, http.MethodGet, target, nil, &rows); err != nil {
		return nil, err
	}
	out := make([]domain.CMSTerm, 0, len(rows))
	for _, row := range rows {
		if row.ID < 1 {
			return nil, &domain.DataError{Code: "cms_invalid_term"}
		}
		out = append(out, domain.CMSTerm{ID: strconv.FormatInt(row.ID, 10), Name: row.Name, Slug: row.Slug})
	}
	return out, nil
}

func (w *wordpress) GetSiteInfo(ctx context.Context) (domain.CMSSiteInfo, error) {
	d, err := w.details(ctx)
	if err != nil {
		return domain.CMSSiteInfo{}, err
	}
	info := d.info
	if info.Categories, err = w.terms(ctx, d, "categories"); err != nil {
		return domain.CMSSiteInfo{}, err
	}
	if info.Tags, err = w.terms(ctx, d, "tags"); err != nil {
		return domain.CMSSiteInfo{}, err
	}
	return info, nil
}

type wpText struct {
	Raw      *string `json:"raw"`
	Rendered string  `json:"rendered"`
}

type wpHead struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Canonical   string `json:"canonical"`
}

type wpPage struct {
	ID       int64                      `json:"id"`
	Link     string                     `json:"link"`
	Slug     string                     `json:"slug"`
	Status   string                     `json:"status"`
	Date     string                     `json:"date_gmt"`
	Modified string                     `json:"modified_gmt"`
	Title    wpText                     `json:"title"`
	Content  wpText                     `json:"content"`
	Excerpt  wpText                     `json:"excerpt"`
	Meta     map[string]json.RawMessage `json:"meta"`
	Yoast    wpHead                     `json:"yoast_head_json"`
	RankMath wpHead                     `json:"rank_math_head_json"`
}

func parseDate(raw string) *time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05"} {
		if value, err := time.Parse(layout, raw); err == nil {
			value = value.UTC()
			return &value
		}
	}
	return nil
}

func wpID(id string) bool {
	value, err := strconv.ParseInt(id, 10, 64)
	return err == nil && value > 0 && strconv.FormatInt(value, 10) == id
}

func observedMeta(raw map[string]json.RawMessage, yoast, rankMath, renderedYoast, renderedRankMath string) string {
	for _, key := range []string{yoast, rankMath} {
		if text, ok := raw[key]; ok {
			var value string
			if json.Unmarshal(text, &value) == nil {
				return value
			}
		}
	}
	return firstText(renderedYoast, renderedRankMath)
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (w *wordpress) normalizePage(raw json.RawMessage, kind string, status int) (domain.CMSPage, error) {
	var page wpPage
	if json.Unmarshal(raw, &page) != nil || page.ID < 1 || page.Status == "" ||
		page.Title.Raw == nil || page.Content.Raw == nil || page.Excerpt.Raw == nil ||
		len(*page.Content.Raw) > maxPageBody {
		return domain.CMSPage{}, &domain.DataError{Code: "cms_wordpress_edit_response"}
	}
	if page.Link != "" {
		if _, err := w.approved(page.Link); err != nil {
			return domain.CMSPage{}, err
		}
	}
	result := domain.CMSPage{
		ID: strconv.FormatInt(page.ID, 10), Resource: kind, URL: page.Link, Slug: page.Slug, Status: page.Status,
		Title: *page.Title.Raw, Body: *page.Content.Raw, Excerpt: *page.Excerpt.Raw,
		SEOTitle:    observedMeta(page.Meta, yoastNames.title, rankMathNames.title, page.Yoast.Title, page.RankMath.Title),
		Description: observedMeta(page.Meta, yoastNames.description, rankMathNames.description, page.Yoast.Description, page.RankMath.Description),
		Canonical:   observedMeta(page.Meta, yoastNames.canonical, rankMathNames.canonical, page.Yoast.Canonical, page.RankMath.Canonical),
		ModifiedAt:  parseDate(page.Modified), Raw: raw, HTTPStatus: status,
	}
	if page.Status == "future" {
		result.ScheduledAt = parseDate(page.Date)
	}
	return result, nil
}

func editQuery() url.Values {
	return url.Values{
		"context": {"edit"},
		"_fields": {"id,link,slug,status,modified_gmt,date_gmt,title,content,excerpt,meta,yoast_head_json,rank_math_head_json"},
	}
}

func (w *wordpress) ListPages(ctx context.Context, kind, slug string, limit int) ([]domain.CMSPage, error) {
	collection, err := resourceKind(kind)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 25
	}
	if limit > maxListing {
		return nil, invalid("limit", "must not exceed 250 pages")
	}
	if err := checkPageInput(domain.CMSPageInput{Resource: kind, Slug: slug}); err != nil {
		return nil, err
	}
	q := editQuery()
	q["status[]"] = []string{"draft", "pending", "private", "publish", "future"}
	if slug != "" {
		q.Set("slug", slug)
	}
	out := make([]domain.CMSPage, 0, min(limit, 100))
	for page := 1; len(out) < limit && page <= 5; page++ {
		q.Set("per_page", strconv.Itoa(min(limit, 100)))
		q.Set("page", strconv.Itoa(page))
		target := setQuery(w.route(collection), q)
		var rows []json.RawMessage
		result, err := w.readJSON(ctx, http.MethodGet, target, nil, &rows)
		if err != nil {
			return nil, err
		}
		if len(rows) > 100 {
			return nil, &domain.DataError{Code: "cms_wordpress_page_limit"}
		}
		for _, raw := range rows {
			p, err := w.normalizePage(raw, kind, result.status)
			if err != nil {
				return nil, err
			}
			if slug == "" || p.Slug == slug {
				out = append(out, p)
			}
			if len(out) == limit {
				break
			}
		}
		pages, _ := strconv.Atoi(result.header.Get("X-WP-TotalPages"))
		if page >= pages || len(rows) == 0 {
			break
		}
	}
	return out, nil
}

func (w *wordpress) GetPage(ctx context.Context, kind, id string) (domain.CMSPage, error) {
	collection, err := resourceKind(kind)
	if err != nil {
		return domain.CMSPage{}, err
	}
	if !wpID(id) {
		return domain.CMSPage{}, invalid("remote_id", "must be a positive WordPress numeric ID")
	}
	return w.pageRequest(ctx, http.MethodGet, w.route(collection, id), kind, id, nil)
}

func (w *wordpress) pageRequest(ctx context.Context, method string, target *url.URL, kind, id string, value any) (domain.CMSPage, error) {
	var raw json.RawMessage
	result, err := w.readJSON(ctx, method, setQuery(target, editQuery()), value, &raw)
	if err != nil {
		return domain.CMSPage{}, err
	}
	page, err := w.normalizePage(raw, kind, result.status)
	if err == nil && id != "" && page.ID != id {
		err = &domain.DataError{Code: "cms_remote_identity"}
	}
	return page, err
}

func (w *wordpress) CreatePage(ctx context.Context, in domain.CMSPageInput) (domain.CMSPage, error) {
	if in.Status != "" && in.Status != "draft" {
		return domain.CMSPage{}, invalid("status", "new content starts as a draft")
	}
	in.Status = "draft"
	return w.write(ctx, "", in, false)
}

func (w *wordpress) UpdatePage(ctx context.Context, id string, in domain.CMSPageInput) (domain.CMSPage, error) {
	if !wpID(id) {
		return domain.CMSPage{}, invalid("remote_id", "must be a positive WordPress numeric ID")
	}
	return w.write(ctx, id, in, false)
}

func (w *wordpress) PublishPage(ctx context.Context, id string, in domain.CMSPageInput) (domain.CMSPage, error) {
	if !wpID(id) {
		return domain.CMSPage{}, invalid("remote_id", "must be a positive WordPress numeric ID")
	}
	in, err := publishInput(in)
	if err != nil {
		return domain.CMSPage{}, err
	}
	return w.write(ctx, id, in, true)
}

func (w *wordpress) write(ctx context.Context, id string, in domain.CMSPageInput, publish bool) (domain.CMSPage, error) {
	if err := w.allowWrites(); err != nil {
		return domain.CMSPage{}, err
	}
	if err := checkPageInput(in); err != nil {
		return domain.CMSPage{}, err
	}
	collection, _ := resourceKind(in.Resource)
	d, err := w.details(ctx)
	if err != nil {
		return domain.CMSPage{}, err
	}
	if !canEdit(d, collection) || (in.Status == "publish" || in.Status == "future") && !canPublish(d, collection) {
		return domain.CMSPage{}, domain.ErrExecutionUnsupported
	}
	payload := map[string]any{}
	if !publish || in.Title != "" || in.Body != "" {
		payload["title"] = in.Title
		payload["content"] = in.Body
		payload["excerpt"] = in.Excerpt
	}
	if in.Slug != "" {
		payload["slug"] = in.Slug
	}
	if in.Status != "" {
		payload["status"] = in.Status
	}
	if in.ScheduledAt != nil {
		payload["date_gmt"] = in.ScheduledAt.UTC().Format("2006-01-02T15:04:05")
	}
	fields := d.meta[collection]
	meta := map[string]string{}
	for name, value := range map[string]string{fields.title: in.SEOTitle, fields.description: in.Description, fields.canonical: in.Canonical} {
		if name != "" && (!publish || value != "") {
			meta[name] = value
		}
	}
	if len(meta) != 0 {
		payload["meta"] = meta
	}
	target := w.route(collection)
	if id != "" {
		target = w.route(collection, id)
	}
	return w.pageRequest(ctx, http.MethodPost, target, in.Resource, id, payload)
}

type wpMedia struct {
	ID        int64  `json:"id"`
	SourceURL string `json:"source_url"`
	MIME      string `json:"mime_type"`
	Title     wpText `json:"title"`
}

func normalizeMedia(raw json.RawMessage) (domain.CMSMedia, error) {
	var val wpMedia
	if json.Unmarshal(raw, &val) != nil || val.ID < 1 {
		return domain.CMSMedia{}, &domain.DataError{Code: "cms_invalid_media"}
	}
	if _, err := domain.EvidenceURL(val.SourceURL); err != nil {
		return domain.CMSMedia{}, &domain.DataError{Code: "cms_invalid_media"}
	}
	title := val.Title.Rendered
	if val.Title.Raw != nil {
		title = *val.Title.Raw
	}
	return domain.CMSMedia{ID: strconv.FormatInt(val.ID, 10), URL: val.SourceURL, Title: title, MediaType: val.MIME, Raw: raw}, nil
}

func (w *wordpress) GetMedia(ctx context.Context, limit int) ([]domain.CMSMedia, error) {
	if limit < 1 {
		limit = 25
	}
	target := setQuery(w.route("media"), url.Values{
		"per_page": {strconv.Itoa(min(limit, 100))}, "_fields": {"id,source_url,mime_type,title"},
	})
	var rows []json.RawMessage
	if _, err := w.readJSON(ctx, http.MethodGet, target, nil, &rows); err != nil {
		return nil, err
	}
	if len(rows) > 100 {
		return nil, &domain.DataError{Code: "cms_wordpress_page_limit"}
	}
	out := make([]domain.CMSMedia, 0, len(rows))
	for _, raw := range rows {
		item, err := normalizeMedia(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (w *wordpress) UploadMedia(ctx context.Context, filename, mediaType string, data []byte) (domain.CMSMedia, error) {
	if err := w.allowWrites(); err != nil {
		return domain.CMSMedia{}, err
	}
	d, err := w.details(ctx)
	if err != nil {
		return domain.CMSMedia{}, err
	}
	if !d.info.Capabilities.WriteMedia {
		return domain.CMSMedia{}, domain.ErrExecutionUnsupported
	}
	target := setQuery(w.route("media"), url.Values{"_fields": {"id,source_url,mime_type,title"}})
	result, err := w.upload(ctx, target, filename, mediaType, data)
	if err != nil {
		return domain.CMSMedia{}, err
	}
	return normalizeMedia(result.body)
}

func (w *wordpress) GetSitemap(ctx context.Context) (domain.CMSSitemap, error) {
	files := []string{"wp-sitemap.xml", "sitemap_index.xml", "sitemap.xml"}
	for _, filename := range files {
		target := w.endpoint(filename)
		answer, err := w.sitemap(ctx, target, false)
		if err == nil || !isNotFound(err) {
			return domain.CMSSitemap{URL: target.String(), XML: answer}, err
		}
	}
	return domain.CMSSitemap{}, domain.ErrExecutionUnsupported
}

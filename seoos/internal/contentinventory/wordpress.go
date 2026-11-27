package contentinventory

import (
	"context"
	"html"
	"net/url"
	"strconv"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const wpPageSize = 100

type wordpressItem struct {
	Link       string `json:"link"`
	Modified   string `json:"modified_gmt"`
	Categories []int  `json:"categories"`
	Tags       []int  `json:"tags"`
}

type wordpressTerm struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Link string `json:"link"`
}

func (c *Crawler) discoverWordPress(ctx context.Context, set *inventorySet, configured string) error {
	api := siteEndpoint(set.base, "wp-json/wp/v2")
	if configured != "" {
		var err error
		api, err = scopedURL(set.base, configured)
		if err != nil {
			return err
		}
	}
	cats, moreCats, err := wordpressList[wordpressTerm](ctx, c, api, "categories", "id,name,link", set.limit, false)
	if err != nil {
		return err
	}
	tags, moreTags, err := wordpressList[wordpressTerm](ctx, c, api, "tags", "id,name,link", set.limit, false)
	if err != nil {
		return err
	}
	pages, morePages, err := wordpressList[wordpressItem](ctx, c, api, "pages", "link,modified_gmt", set.limit, true)
	if err != nil {
		return err
	}
	posts, morePosts, err := wordpressList[wordpressItem](ctx, c, api, "posts",
		"link,modified_gmt,categories,tags", set.limit, true)
	if err != nil {
		return err
	}
	set.batch.Truncated = set.batch.Truncated || moreCats || moreTags || morePages || morePosts
	categoryNames, tagNames := wordpressTermNames(cats), wordpressTermNames(tags)
	for i := 0; i < max(len(pages), len(posts)); i++ {
		if i < len(pages) && !set.add(wordpressEntry(pages[i], "page", categoryNames, tagNames)) {
			return nil
		}
		if i < len(posts) && !set.add(wordpressEntry(posts[i], "post", categoryNames, tagNames)) {
			return nil
		}
	}
	for _, term := range cats {
		if !set.add(domain.InventoryEntry{URL: term.Link, Kind: "category", Categories: []string{html.UnescapeString(term.Name)}}) {
			return nil
		}
	}
	for _, term := range tags {
		if !set.add(domain.InventoryEntry{URL: term.Link, Kind: "tag", Tags: []string{html.UnescapeString(term.Name)}}) {
			return nil
		}
	}
	return nil
}

func wordpressList[T any](ctx context.Context, c *Crawler, api *url.URL, collection, fields string, limit int, published bool) ([]T, bool, error) {
	endpoint := siteEndpoint(api, collection)
	var items []T
	for page := 1; len(items) < limit && page <= maxPageLimit/wpPageSize; page++ {
		params := url.Values{
			"per_page": {strconv.Itoa(wpPageSize)},
			"page":     {strconv.Itoa(page)},
			"_fields":  {fields},
		}
		if published {
			params.Set("status", "publish")
		}
		endpoint.RawQuery = params.Encode()
		var result []T
		headers, err := c.readJSON(ctx, endpoint, &result)
		if err != nil {
			return nil, false, err
		}
		if result == nil {
			return nil, false, &domain.DataError{Code: "inventory_invalid_wordpress_json"}
		}
		take := min(limit-len(items), len(result))
		items = append(items, result[:take]...)
		hasNext := len(result) == wpPageSize
		if total, err := strconv.Atoi(headers.Get("X-WP-TotalPages")); err == nil {
			hasNext = page < total
		}
		if take < len(result) {
			return items, true, nil
		}
		if !hasNext || len(result) == 0 {
			return items, false, nil
		}
	}
	return items, true, nil
}

func wordpressEntry(item wordpressItem, kind string, categories, tags map[int]string) domain.InventoryEntry {
	entry := domain.InventoryEntry{URL: item.Link, ModifiedAt: parseTimestamp(item.Modified), Kind: kind}
	for _, id := range item.Categories {
		if name := categories[id]; name != "" {
			entry.Categories = append(entry.Categories, name)
		}
	}
	for _, id := range item.Tags {
		if name := tags[id]; name != "" {
			entry.Tags = append(entry.Tags, name)
		}
	}
	return entry
}

func wordpressTermNames(terms []wordpressTerm) map[int]string {
	names := make(map[int]string, len(terms))
	for _, term := range terms {
		names[term.ID] = html.UnescapeString(term.Name)
	}
	return names
}

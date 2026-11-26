package researchprovider

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type marketEcho struct {
	URL        string `json:"url"`
	Engine     string `json:"se"`
	EngineType string `json:"se_type"`
	Location   *int   `json:"location_code"`
	Language   string `json:"language_code"`
	Device     string `json:"device"`
}

func (echo marketEcho) matches(call dataCall) bool {
	engine := echo.Engine
	if engine == "google_ads" {
		engine = "google"
	}
	if engine == "" && (echo.EngineType == "google" || echo.EngineType == "bing") {
		engine = echo.EngineType
	}
	if engine != "" && engine != call.market.SearchEngine {
		return false
	}
	if call.params["location_code"] != nil && echo.Location != nil && *echo.Location != call.market.LocationCode {
		return false
	}
	if language, ok := call.params["language_code"].(string); ok && echo.Language != "" && !strings.EqualFold(echo.Language, language) {
		return false
	}
	if device, ok := call.params["device"].(string); ok && echo.Device != "" && echo.Device != device {
		return false
	}
	return true
}

type labResult struct {
	marketEcho
	Items []labKeyword `json:"items"`
}

type labKeyword struct {
	marketEcho
	Keyword    string `json:"keyword"`
	Difficulty *int   `json:"keyword_difficulty"`
	Info       struct {
		Volume      *int64   `json:"search_volume"`
		CPC         *float64 `json:"cpc"`
		Competition *float64 `json:"competition"`
	} `json:"keyword_info"`
	Properties struct {
		Difficulty *int `json:"keyword_difficulty"`
	} `json:"keyword_properties"`
	Intent struct {
		Main string `json:"main_intent"`
	} `json:"search_intent_info"`
	Related *labKeyword `json:"keyword_data"`
}

type adsKeyword struct {
	marketEcho
	Keyword          string          `json:"keyword"`
	Volume           *int64          `json:"search_volume"`
	CPC              *float64        `json:"cpc"`
	Competition      json.RawMessage `json:"competition"`
	CompetitionIndex *int            `json:"competition_index"`
}

type serpResult struct {
	marketEcho
	Datetime string     `json:"datetime"`
	Items    []serpItem `json:"items"`
}

type serpItem struct {
	Type        string `json:"type"`
	RankGroup   int    `json:"rank_group"`
	URL         string `json:"url"`
	Domain      string `json:"domain"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type instantResult struct {
	Items []instantPage `json:"items"`
}

type instantPage struct {
	Type       string `json:"resource_type"`
	StatusCode int    `json:"status_code"`
	URL        string `json:"url"`
	Meta       struct {
		Title        string              `json:"title"`
		Description  string              `json:"description"`
		Canonical    string              `json:"canonical"`
		MetaKeywords string              `json:"meta_keywords"`
		HeadingTags  map[string][]string `json:"htags"`
	} `json:"meta"`
	Checks struct {
		Micromarkup bool `json:"has_micromarkup"`
	} `json:"checks"`
	LastModified struct {
		Header  string          `json:"header"`
		Sitemap json.RawMessage `json:"sitemap"`
		MetaTag string          `json:"meta_tag"`
	} `json:"last_modified"`
}

var (
	errInvalidResult  = errors.New("invalid_provider_response")
	errMarketMismatch = errors.New("provider_market_mismatch")
	errPageMissing    = errors.New("provider_page_unavailable")
	errCrawlPending   = errors.New("provider_crawl_pending")
)

func readResult(response *domain.DataResponse, task apiTask, call dataCall, req domain.DataRequest) error {
	switch req.Operation {
	case domain.DataRelated, domain.DataOverview:
		return readLabs(response, task, call, req.Operation)
	case domain.DataSuggestions:
		if call.market.SearchEngine == "google" {
			return readLabs(response, task, call, req.Operation)
		}
		return readAds(response, task, call)
	case domain.DataVolume:
		return readAds(response, task, call)
	case domain.DataSERP:
		return readSERP(response, task, call)
	case domain.DataPage:
		return readPage(response, task, req.TargetURL)
	case domain.DataPageContent:
		return readPageContent(response, task, req.TargetURL)
	case domain.DataPageMicrodata:
		return readPageMicrodata(response, task, req.TargetURL)
	default:
		return errInvalidResult
	}
}

func readLabs(response *domain.DataResponse, task apiTask, call dataCall, operation domain.DataOperation) error {
	seen := make(map[string]struct{})
	for _, row := range task.Result {
		var result labResult
		if err := json.Unmarshal(row, &result); err != nil {
			return errInvalidResult
		}
		if !result.matches(call) {
			return errMarketMismatch
		}
		for _, data := range result.Items {
			value := &data
			if operation == domain.DataRelated {
				value = data.Related
			}
			if value == nil {
				continue
			}
			if !value.matches(call) {
				return errMarketMismatch
			}
			difficulty := value.Properties.Difficulty
			if call.market.SearchEngine == "bing" && operation == domain.DataOverview {
				difficulty = value.Difficulty
			}
			metric := domain.KeywordMetric{
				Keyword:           strings.TrimSpace(value.Keyword),
				SearchVolume:      inRange(value.Info.Volume, 0, 1<<63-1),
				CPCUSD:            inRange(value.Info.CPC, 0, 1e15),
				Competition:       inRange(value.Info.Competition, 0, 1),
				KeywordDifficulty: inRange(difficulty, 0, 100),
				ProviderIntent:    strings.TrimSpace(value.Intent.Main),
			}
			appendKeyword(response, metric, seen)
		}
	}
	return nil
}

func readAds(response *domain.DataResponse, task apiTask, call dataCall) error {
	seen := make(map[string]struct{})
	for _, row := range task.Result {
		var value adsKeyword
		if err := json.Unmarshal(row, &value); err != nil {
			return errInvalidResult
		}
		if !value.matches(call) {
			return errMarketMismatch
		}
		metric := domain.KeywordMetric{
			Keyword:          strings.TrimSpace(value.Keyword),
			SearchVolume:     inRange(value.Volume, 0, 1<<63-1),
			CPCUSD:           inRange(value.CPC, 0, 1e15),
			CompetitionIndex: inRange(value.CompetitionIndex, 0, 100),
		}
		// Google Ads returns a category here. Preserve it in the snapshot and
		// surface only the separately supplied numeric index.
		if call.market.SearchEngine == "bing" && len(value.Competition) > 0 {
			var numeric *float64
			if err := json.Unmarshal(value.Competition, &numeric); err != nil {
				return errInvalidResult
			}
			metric.Competition = inRange(numeric, 0, 1)
		}
		appendKeyword(response, metric, seen)
	}
	return nil
}

func inRange[T int | int64 | float64](v *T, lo, hi T) *T {
	if v == nil || *v < lo || *v > hi {
		return nil
	}
	return v
}

func appendKeyword(response *domain.DataResponse, metric domain.KeywordMetric, seen map[string]struct{}) {
	if len(response.Keywords) >= keywordLimit || metric.Keyword == "" || len(metric.Keyword) > 400 ||
		!utf8.ValidString(metric.Keyword) ||
		strings.IndexFunc(metric.Keyword, unicode.IsControl) >= 0 {
		return
	}
	key := domain.KeywordKey(metric.Keyword)
	if len(key) > 400 {
		return
	}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	response.Keywords = append(response.Keywords, metric)
}

func readSERP(response *domain.DataResponse, task apiTask, call dataCall) error {
	seen := make(map[int]struct{}, 10)
	for _, row := range task.Result {
		var result serpResult
		if err := json.Unmarshal(row, &result); err != nil {
			return errInvalidResult
		}
		if !result.matches(call) {
			return errMarketMismatch
		}
		if collected := sourceTime(result.Datetime, response.CollectedAt); collected != nil {
			response.CollectedAt = *collected
		}
		for _, item := range result.Items {
			if item.Type != "organic" || item.RankGroup < 1 || item.RankGroup > 10 {
				continue
			}
			u, err := domain.EvidenceURL(item.URL)
			if err != nil {
				continue
			}
			if _, ok := seen[item.RankGroup]; ok {
				continue
			}
			seen[item.RankGroup] = struct{}{}
			resultDomain := item.Domain
			if resultDomain == "" {
				resultDomain = u.Hostname()
			}
			response.Results = append(response.Results, domain.SERPEntry{
				Rank:        item.RankGroup,
				URL:         item.URL,
				Domain:      resultDomain,
				Title:       item.Title,
				Description: item.Description,
			})
		}
	}
	sort.Slice(response.Results, func(i, j int) bool { return response.Results[i].Rank < response.Results[j].Rank })
	return nil
}

func readPage(response *domain.DataResponse, task apiTask, target string) error {
	requested, _ := publicPageURL(target)
	for _, row := range task.Result {
		var result instantResult
		if err := json.Unmarshal(row, &result); err != nil {
			return errInvalidResult
		}
		for _, page := range result.Items {
			if page.Type != "html" || page.StatusCode < 200 || page.StatusCode >= 300 {
				continue
			}
			u, ok := publicPageURL(page.URL)
			if !ok || !samePageURL(u, requested) {
				continue
			}
			evidence := &domain.PageEvidence{
				URL:               page.URL,
				Title:             page.Meta.Title,
				Description:       page.Meta.Description,
				MicrodataObserved: page.Checks.Micromarkup,
				Source:            response.Source,
			}
			if page.Meta.Canonical != "" {
				ref, err := url.Parse(page.Meta.Canonical)
				if err == nil {
					if canonical, ok := publicPageURL(u.ResolveReference(ref).String()); ok {
						evidence.CanonicalURL = canonical.String()
					}
				}
			}
			for level := 1; level <= 6; level++ {
				for _, heading := range page.Meta.HeadingTags["h"+strconv.Itoa(level)] {
					if heading = strings.TrimSpace(heading); heading != "" {
						evidence.Headings = append(evidence.Headings, domain.HeadingEvidence{Level: level, Text: heading})
					}
				}
			}
			seen := make(map[string]struct{})
			for _, topic := range strings.Split(page.Meta.MetaKeywords, ",") {
				if topic = strings.TrimSpace(topic); topic != "" {
					key := domain.KeywordKey(topic)
					if _, ok := seen[key]; !ok {
						evidence.Topics = append(evidence.Topics, topic)
						seen[key] = struct{}{}
					}
				}
			}
			var sitemap string
			_ = json.Unmarshal(page.LastModified.Sitemap, &sitemap)
			for _, value := range []string{page.LastModified.Header, sitemap, page.LastModified.MetaTag} {
				observed := sourceTime(value, response.CollectedAt)
				if observed != nil && (evidence.LastModified == nil || observed.After(*evidence.LastModified)) {
					evidence.LastModified = observed
				}
			}
			response.Page = evidence
			return nil
		}
	}
	return errPageMissing
}

func samePageURL(observed, requested *url.URL) bool {
	if requested == nil {
		return false
	}
	path := func(u *url.URL) string {
		if u.Path == "" {
			return "/"
		}
		return u.EscapedPath()
	}
	return observed.Scheme == requested.Scheme &&
		strings.EqualFold(strings.TrimSuffix(observed.Hostname(), "."), strings.TrimSuffix(requested.Hostname(), ".")) &&
		path(observed) == path(requested) && observed.RawQuery == requested.RawQuery
}

func sourceTime(value string, now time.Time) *time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05 -07:00", time.DateOnly} {
		if parsed, err := time.Parse(layout, value); err == nil && !parsed.After(now) {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

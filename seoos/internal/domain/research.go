package domain

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

const (
	KeywordDiscovery   = "keyword_discovery"
	KeywordMetrics     = "keyword_metrics"
	KeywordSuggestions = "keyword_suggestions"
	KeywordIntent      = "keyword_intent"
	KeywordClustering  = "keyword_clustering"
	SERPAnalysis       = "serp_analysis"
	CompetitorAnalysis = "competitor_analysis"
	KeywordURLMapping  = "keyword_url_mapping"
	OpportunityScoring = "opportunity_scoring"

	ProviderOpenAI     = "openai"
	ProviderDataForSEO = "dataforseo"
)

func IsResearchKind(kind string) bool {
	switch kind {
	case KeywordDiscovery, KeywordMetrics, KeywordSuggestions, KeywordIntent, KeywordClustering,
		SERPAnalysis, CompetitorAnalysis, KeywordURLMapping, OpportunityScoring:
		return true
	default:
		return false
	}
}

func UsesDataProvider(kind string) bool {
	switch kind {
	case KeywordDiscovery, KeywordMetrics, KeywordSuggestions, SERPAnalysis, CompetitorAnalysis:
		return true
	default:
		return false
	}
}

type Market struct {
	CountryCode  string `json:"country_code"`
	LocationCode int    `json:"location_code"`
	LanguageCode string `json:"language_code"`
	SearchEngine string `json:"search_engine"`
	Device       string `json:"device"`
}

func (m Market) Normalize() (Market, error) {
	m.CountryCode = strings.ToUpper(strings.TrimSpace(m.CountryCode))
	m.LanguageCode = strings.TrimSpace(m.LanguageCode)
	m.SearchEngine = strings.ToLower(strings.TrimSpace(m.SearchEngine))
	m.Device = strings.ToLower(strings.TrimSpace(m.Device))
	region, err := language.ParseRegion(m.CountryCode)
	if len(m.CountryCode) != 2 || err != nil || !region.IsCountry() {
		return m, invalid("country_code", "must be a two-letter country code")
	}
	if m.LocationCode < 1 || m.LocationCode > 100000000 {
		return m, invalid("location_code", "must be a DataForSEO location code")
	}
	validLanguage := len(m.LanguageCode) >= 2 && len(m.LanguageCode) <= 20
	for _, r := range m.LanguageCode {
		validLanguage = validLanguage && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_')
	}
	if !validLanguage {
		return m, invalid("language_code", "must be a DataForSEO language code")
	}
	if m.SearchEngine != "google" && m.SearchEngine != "bing" {
		return m, invalid("search_engine", "must be google or bing")
	}
	if m.Device != "desktop" && m.Device != "mobile" {
		return m, invalid("device", "must be desktop or mobile")
	}
	return m, nil
}

func KeywordKey(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(label)), " "))
}

type ResearchInput struct {
	SiteID           string   `json:"site_id"`
	ProviderID       string   `json:"provider_id,omitempty"`
	IntentProviderID string   `json:"intent_provider_id,omitempty"`
	Kind             string   `json:"kind"`
	Keyword          string   `json:"keyword"`
	Market           Market   `json:"market"`
	TargetURL        string   `json:"target_url,omitempty"`
	IdempotencyKey   string   `json:"idempotency_key"`
	Priority         int      `json:"priority"`
	BusinessValue    *float64 `json:"business_value,omitempty"`
}

func (in ResearchInput) Normalize() (ResearchInput, error) {
	if !ValidID(in.SiteID) {
		return in, invalid("site_id", "must be a canonical UUID")
	}
	if !IsResearchKind(in.Kind) {
		return in, invalid("kind", "must be a supported research job")
	}
	in.Keyword = strings.Join(strings.Fields(in.Keyword), " ")
	if in.Keyword == "" || len(in.Keyword) > 400 || !utf8.ValidString(in.Keyword) || hasControl(in.Keyword) || len(KeywordKey(in.Keyword)) > 400 {
		return in, invalid("keyword", "must be nonempty UTF-8 search text of at most 400 bytes")
	}
	market, err := in.Market.Normalize()
	if err != nil {
		return in, err
	}
	in.Market = market
	if UsesDataProvider(in.Kind) && !ValidID(in.ProviderID) || !UsesDataProvider(in.Kind) && in.ProviderID != "" {
		return in, invalid("provider_id", "is required for DataForSEO operations only")
	}
	if in.IntentProviderID != "" && (in.Kind != KeywordIntent || !ValidID(in.IntentProviderID)) {
		return in, invalid("intent_provider_id", "may select an OpenAI provider for keyword_intent")
	}
	if in.Kind == CompetitorAnalysis {
		in.TargetURL = strings.TrimSpace(in.TargetURL)
		if _, err := EvidenceURL(in.TargetURL); err != nil {
			return in, err
		}
	} else if in.TargetURL != "" {
		return in, invalid("target_url", "is only accepted for competitor_analysis")
	}
	if len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 120 || !identifier(in.IdempotencyKey) {
		return in, invalid("idempotency_key", "must contain 8 to 120 letters, digits or . _ : -")
	}
	if in.Priority < 0 || in.Priority > 100 {
		return in, invalid("priority", "must be between 0 and 100")
	}
	if in.BusinessValue != nil && (in.Kind != OpportunityScoring || *in.BusinessValue < 0 || *in.BusinessValue > 10) {
		return in, invalid("business_value", "may set a score from 0 to 10 for opportunity_scoring")
	}
	return in, nil
}

func EvidenceURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || !utf8.ValidString(raw) || hasControl(raw) || u.Opaque != "" ||
		(u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, invalid("url", "must be an HTTP(S) page URL without credentials or fragment")
	}
	return u, nil
}

type DataOperation string

const (
	DataRelated       DataOperation = "related_keywords"
	DataSuggestions   DataOperation = "keyword_suggestions"
	DataOverview      DataOperation = "keyword_overview"
	DataVolume        DataOperation = "search_volume"
	DataSERP          DataOperation = "serp"
	DataPage          DataOperation = "page"
	DataPageContent   DataOperation = "page_content"
	DataPageMicrodata DataOperation = "page_microdata"
)

type DataRequest struct {
	Operation    DataOperation
	Market       Market
	Keyword      string
	TargetURL    string
	TargetTaskID string
	RequestID    string
}

// DeviceSpecific reports whether the upstream operation can vary by device.
func (r DataRequest) DeviceSpecific() bool {
	return r.Operation == DataSERP || (r.Market.SearchEngine == "bing" &&
		(r.Operation == DataSuggestions || r.Operation == DataVolume))
}

type DataPlan struct {
	ReserveMicros int64
	TTL           time.Duration
}

type DataAccess struct {
	Login    string
	Password string
	Timeout  time.Duration
}

type DataResponse struct {
	Raw         json.RawMessage `json:"-"`
	TaskID      string          `json:"task_id"`
	Source      string          `json:"source"`
	CollectedAt time.Time       `json:"collected_at"`
	CostMicros  *int64          `json:"cost_micros,omitempty"`
	Keywords    []KeywordMetric `json:"keywords,omitempty"`
	Results     []SERPEntry     `json:"results,omitempty"`
	Page        *PageEvidence   `json:"page,omitempty"`
}

const MaxProviderRawBytes = 8 << 20

type DataError struct {
	Code       string
	Retry      bool
	RetryAfter time.Duration
	// KnownUnbilled is true only when the response establishes no upstream charge.
	KnownUnbilled bool
}

func (e *DataError) Error() string { return e.Code }

type SEODataProvider interface {
	Plan(DataRequest) (DataPlan, error)
	Fetch(context.Context, DataRequest, DataAccess) (DataResponse, error)
}

type KeywordMetric struct {
	Keyword           string   `json:"keyword"`
	DeviceScope       string   `json:"device_scope,omitempty"`
	SearchVolume      *int64   `json:"search_volume,omitempty"`
	CPCUSD            *float64 `json:"cpc_usd,omitempty"`
	Competition       *float64 `json:"competition,omitempty"`
	CompetitionIndex  *int     `json:"competition_index,omitempty"`
	KeywordDifficulty *int     `json:"keyword_difficulty,omitempty"`
	ProviderIntent    string   `json:"provider_intent,omitempty"`
}

type SERPEntry struct {
	Rank        int    `json:"rank"`
	URL         string `json:"url"`
	Domain      string `json:"domain"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PageType    string `json:"page_type,omitempty"`
}

type HeadingEvidence struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type PageSectionEvidence struct {
	Level           int    `json:"level"`
	Heading         string `json:"heading"`
	PrimaryBlocks   int    `json:"primary_blocks"`
	SecondaryBlocks int    `json:"secondary_blocks"`
	Tables          int    `json:"tables"`
}

type PageSource struct {
	Source      string    `json:"source"`
	SnapshotID  string    `json:"snapshot_id"`
	CollectedAt time.Time `json:"collected_at"`
}

type PageEvidence struct {
	URL               string                `json:"url"`
	Title             string                `json:"title,omitempty"`
	Description       string                `json:"description,omitempty"`
	CanonicalURL      string                `json:"canonical_url,omitempty"`
	Headings          []HeadingEvidence     `json:"headings,omitempty"`
	Topics            []string              `json:"topics,omitempty"`
	Entities          []string              `json:"entities,omitempty"`
	FAQ               []string              `json:"faq,omitempty"`
	Schema            []string              `json:"schema,omitempty"`
	Sections          []PageSectionEvidence `json:"sections,omitempty"`
	MicrodataObserved bool                  `json:"microdata_observed,omitempty"`
	SourceSnapshots   []PageSource          `json:"source_snapshots,omitempty"`
	PageType          string                `json:"page_type,omitempty"`
	LastModified      *time.Time            `json:"last_modified,omitempty"`
	Source            string                `json:"source,omitempty"`
}

type SearchIntent struct {
	Primary    string   `json:"primary"`
	Secondary  string   `json:"secondary,omitempty"`
	Need       string   `json:"need"`
	PageType   string   `json:"page_type"`
	Confidence float64  `json:"confidence"`
	Reasons    []string `json:"reasons"`
	Sources    []string `json:"sources"`
}

type KeywordEvidence struct {
	ID            string                      `json:"id"`
	Term          string                      `json:"keyword"`
	Metric        KeywordMetric               `json:"metrics"`
	MetricSources map[string]MetricProvenance `json:"metric_sources,omitempty"`
	SERP          []SERPEntry                 `json:"serp"`
	Pages         []PageEvidence              `json:"pages,omitempty"`
	Intent        *SearchIntent               `json:"intent,omitempty"`
}

type MetricProvenance struct {
	Source      string    `json:"source"`
	SnapshotID  string    `json:"snapshot_id"`
	CollectedAt time.Time `json:"collected_at"`
	DeviceScope string    `json:"device_scope"`
}

type KeywordCluster struct {
	ID             string   `json:"id"`
	PrimaryKeyword string   `json:"primary_keyword"`
	Secondary      []string `json:"secondary_keywords"`
	LongTail       []string `json:"long_tail_keywords"`
	MemberIDs      []string `json:"member_ids"`
	RecommendedURL string   `json:"recommended_url,omitempty"`
	PageType       string   `json:"page_type"`
	Confidence     float64  `json:"confidence"`
	Reasons        []string `json:"reasons"`
}

type URLMapping struct {
	TargetURL         string   `json:"target_url,omitempty"`
	PreviousTargetURL string   `json:"previous_target_url,omitempty"`
	Action            string   `json:"action"`
	Cannibalizing     []string `json:"cannibalizing_urls,omitempty"`
	CanonicalIssue    bool     `json:"canonical_issue"`
	Reasons           []string `json:"reasons"`
	Evidence          []string `json:"evidence"`
}

type ScorePart struct {
	Name     string   `json:"name"`
	Weight   int      `json:"weight"`
	Points   *float64 `json:"points,omitempty"`
	Evidence string   `json:"evidence"`
}

type Opportunity struct {
	Score           *float64                    `json:"score,omitempty"`
	KnownPoints     float64                     `json:"known_points"`
	Coverage        int                         `json:"coverage"`
	Version         string                      `json:"version"`
	EvidenceMetrics *KeywordMetric              `json:"evidence_metrics,omitempty"`
	MetricSources   map[string]MetricProvenance `json:"metric_sources,omitempty"`
	Parts           []ScorePart                 `json:"parts"`
}

type ResearchJob struct {
	Job     Job           `json:"job"`
	Request ResearchInput `json:"request"`
}

type ResearchContext struct {
	Request   ResearchInput
	MarketID  string
	KeywordID string
}

type ResearchQuery struct {
	SiteID string
	Market *Market
	Query  string
	Limit  int
	Offset int
}

type SitePageInput struct {
	SiteID string       `json:"site_id"`
	Market Market       `json:"market"`
	Page   PageEvidence `json:"page"`
}

type DataReservation struct {
	ID         string
	Access     DataAccess
	Cached     *DataResponse
	DeferUntil time.Time
}

type ResearchAnalysis struct {
	Intent      *SearchIntent
	Clusters    []KeywordCluster
	Mapping     *URLMapping
	Opportunity *Opportunity
}

type ResearchRepository interface {
	EnqueueResearch(context.Context, string, ResearchInput) (Job, bool, error)
	ResearchJobs(context.Context, string, ResearchQuery) ([]ResearchJob, error)
	ListResearch(context.Context, string, string, ResearchQuery) ([]json.RawMessage, error)
	SaveSitePage(context.Context, string, SitePageInput) (string, error)
	ResearchSnapshot(context.Context, string, string) (json.RawMessage, error)
}

// BelongsToSite bounds explicitly supplied page evidence to its registered site.
// It is an evidence association and does not authorize a network request.
func BelongsToSite(siteURL string, page *url.URL) bool {
	base, err := url.Parse(siteURL)
	if err != nil || page == nil || !strings.EqualFold(base.Hostname(), page.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	if port(base) != port(page) || base.Scheme != page.Scheme {
		return false
	}
	prefix := path.Clean("/" + strings.TrimPrefix(base.Path, "/"))
	pagePath := path.Clean("/" + strings.TrimPrefix(page.Path, "/"))
	return prefix == "/" || pagePath == prefix || strings.HasPrefix(pagePath, prefix+"/")
}

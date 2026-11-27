package domain

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	ProviderAnthropic  = "anthropic"
	ProviderGemini     = "gemini"
	ProviderDeepSeek   = "deepseek"
	ProviderCompatible = "openai_compatible"
	ContentInventory   = "site_inventory"
	ContentExtract     = "page_extraction"
	ContentPlan        = "content_strategy"
	ContentBriefJob    = "content_brief"
	ContentResearch    = "content_research"
	ContentOutline     = "content_outline"
	ContentDraft       = "content_draft"
	ContentFactCheck   = "content_fact_check"
	ContentReview      = "content_quality_review"
	ContentOptimize    = "content_seo_optimization"
	ContentRefresh     = "content_refresh"
	ContentVerify      = "content_publish_verify"
)

func IsModelProvider(kind string) bool {
	return kind == ProviderOpenAI || kind == ProviderAnthropic || kind == ProviderGemini || kind == ProviderDeepSeek || kind == ProviderCompatible
}

var (
	ErrContentPrerequisite = errors.New("required content evidence or version is unavailable")
	ErrContentGate         = errors.New("a passing review and explicit approval are required")
	ErrContentForbidden    = errors.New("content action requires a higher role")
	ErrCrawlUnauthorized   = errors.New("the owner has not authorized this read")
)

func IsContentKind(kind string) bool {
	switch kind {
	case ContentInventory, ContentExtract, ContentPlan, ContentBriefJob, ContentResearch, ContentOutline,
		ContentDraft, ContentFactCheck, ContentReview, ContentOptimize, ContentRefresh, ContentVerify:
		return true
	}
	return false
}

func UsesContentModel(kind string) bool {
	return kind == ContentOutline || kind == ContentDraft || kind == ContentReview
}

type CrawlPolicy struct {
	SiteID        string   `json:"site_id"`
	Enabled       bool     `json:"enabled"`
	SitemapURL    string   `json:"sitemap_url,omitempty"`
	Connector     string   `json:"connector"`
	ConnectorURL  string   `json:"connector_url,omitempty"`
	SourceOrigins []string `json:"source_origins"`
	MaxPages      int      `json:"max_pages"`
}

type InventoryEntry struct {
	URL        string     `json:"url"`
	Kind       string     `json:"kind"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
	Categories []string   `json:"categories,omitempty"`
	Tags       []string   `json:"tags,omitempty"`
}

type InventoryBatch struct {
	Entries   []InventoryEntry `json:"entries"`
	Truncated bool             `json:"truncated"`
}

type PageImage struct {
	URL string `json:"url"`
	Alt string `json:"alt,omitempty"`
}

type SitePage struct {
	Categories   []string    `json:"categories,omitempty"`
	Tags         []string    `json:"tags,omitempty"`
	ID           string      `json:"id,omitempty"`
	SiteID       string      `json:"site_id,omitempty"`
	State        string      `json:"state"`
	IndexStatus  string      `json:"index_status"`
	Mapped       bool        `json:"mapped"`
	HTTPStatus   int         `json:"http_status,omitempty"`
	Body         string      `json:"body,omitempty"`
	Language     string      `json:"language,omitempty"`
	Robots       string      `json:"robots,omitempty"`
	Images       []PageImage `json:"images,omitempty"`
	Fingerprint  string      `json:"fingerprint,omitempty"`
	ETag         string      `json:"etag,omitempty"`
	HTTPModified string      `json:"http_modified,omitempty"`
	CheckedAt    *time.Time  `json:"checked_at,omitempty"`
	DiscoveredAt *time.Time  `json:"discovered_at,omitempty"`
	SnapshotID   string      `json:"snapshot_id,omitempty"`
	Unchanged    bool        `json:"-"`
	PageEvidence
}

type ContentProjectInput struct {
	SiteID  string `json:"site_id"`
	Keyword string `json:"keyword"`
	Market  Market `json:"market"`
}

type ContentProject struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	ContentProjectInput
	Revision  int       `json:"revision"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ContentJobInput struct {
	SiteID         string `json:"site_id"`
	ProjectID      string `json:"project_id,omitempty"`
	ProviderID     string `json:"provider_id,omitempty"`
	Kind           string `json:"kind"`
	URL            string `json:"url,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	Priority       int    `json:"priority"`
}

type ContentJobRecord struct {
	Job     Job             `json:"job"`
	Request ContentJobInput `json:"request"`
}

type ContentEvidence struct {
	Keyword        KeywordEvidence     `json:"keyword"`
	SERPSnapshotID string              `json:"serp_snapshot_id,omitempty"`
	Mapping        *URLMapping         `json:"mapping,omitempty"`
	Opportunity    *Opportunity        `json:"opportunity,omitempty"`
	Clusters       []KeywordCluster    `json:"clusters,omitempty"`
	Competitors    []ContentCompetitor `json:"competitors,omitempty"`
}

type ContentCompetitor struct {
	SnapshotID string       `json:"snapshot_id"`
	Page       PageEvidence `json:"page"`
	Gaps       []string     `json:"gaps"`
}

type ContentStrategy struct {
	Action       string   `json:"action"`
	TargetURL    string   `json:"target_url,omitempty"`
	PageType     string   `json:"page_type,omitempty"`
	TopicCluster bool     `json:"topic_cluster"`
	Reasons      []string `json:"reasons"`
	Evidence     []string `json:"evidence"`
}

type ContentHeading struct {
	Level   int    `json:"level"`
	Heading string `json:"heading"`
	Purpose string `json:"purpose,omitempty"`
}

type ContentBrief struct {
	PrimaryKeyword   string           `json:"primary_keyword"`
	Secondary        []string         `json:"secondary_keywords"`
	Intent           string           `json:"intent"`
	Country          string           `json:"country_code"`
	Language         string           `json:"language_code"`
	Need             string           `json:"need"`
	PageType         string           `json:"page_type"`
	URL              string           `json:"url,omitempty"`
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	H1               string           `json:"h1"`
	Headings         []ContentHeading `json:"headings"`
	Questions        []string         `json:"questions"`
	Entities         []string         `json:"entities"`
	Topics           []string         `json:"topics"`
	SourceURLs       []string         `json:"source_urls"`
	Gaps             []string         `json:"gaps"`
	Images           []string         `json:"images"`
	InternalLinks    []string         `json:"internal_links"`
	FactRequirements []string         `json:"fact_requirements"`
}

type ContentSource struct {
	ID             string     `json:"id,omitempty"`
	URL            string     `json:"url"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	CollectedAt    *time.Time `json:"collected_at,omitempty"`
	Fingerprint    string     `json:"fingerprint,omitempty"`
	SERPSnapshotID string     `json:"serp_snapshot_id,omitempty"`
	Status         string     `json:"status,omitempty"`
}

type SourceDocument struct {
	ContentSource
	Text string `json:"text"`
}

type FactEvidence struct {
	SourceID string `json:"source_id"`
	Excerpt  string `json:"excerpt"`
	Conflict bool   `json:"conflict,omitempty"`
}

type ContentFact struct {
	ID         string         `json:"id"`
	Statement  string         `json:"statement"`
	Status     string         `json:"status"`
	Confidence float64        `json:"confidence"`
	Market     Market         `json:"market"`
	Evidence   []FactEvidence `json:"evidence"`
}

type ContentClaim struct {
	Text    string   `json:"text"`
	FactIDs []string `json:"fact_ids"`
}

type ArticleSection struct {
	Level   int            `json:"level"`
	Heading string         `json:"heading"`
	Body    string         `json:"body"`
	Claims  []ContentClaim `json:"claims"`
}

type ContentArticle struct {
	Author        string           `json:"author,omitempty"`
	Language      string           `json:"language_code"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	H1            string           `json:"h1"`
	Slug          string           `json:"slug,omitempty"`
	Canonical     string           `json:"canonical,omitempty"`
	Sections      []ArticleSection `json:"sections"`
	InternalLinks []string         `json:"internal_links"`
	ImageAlts     []string         `json:"image_alts"`
	Schema        []string         `json:"schema"`
}

type ClaimResult struct {
	Text     string   `json:"text"`
	Status   string   `json:"status"`
	FactIDs  []string `json:"fact_ids"`
	Reason   string   `json:"reason"`
	Material bool     `json:"material"`
}

type FactReport struct {
	Passed bool          `json:"passed"`
	Claims []ClaimResult `json:"claims"`
}

type EditorialReview struct {
	Passed        bool     `json:"passed"`
	Intent        bool     `json:"intent"`
	Language      bool     `json:"language"`
	OriginalValue bool     `json:"original_value"`
	Issues        []string `json:"issues"`
	Model         string   `json:"model,omitempty"`
}

type QualityDimension struct {
	Name        string             `json:"name"`
	Score       float64            `json:"score"`
	Weight      int                `json:"weight"`
	Explanation string             `json:"explanation"`
	Metrics     map[string]float64 `json:"metrics,omitempty"`
}

type ContentQuality struct {
	Passed      bool               `json:"passed"`
	Score       float64            `json:"score"`
	Algorithm   string             `json:"algorithm"`
	Dimensions  []QualityDimension `json:"dimensions"`
	Issues      []string           `json:"issues"`
	Suggestions []string           `json:"suggestions"`
	ReviewedAt  time.Time          `json:"reviewed_at"`
}

type RefreshSignals struct {
	TrafficChange *float64 `json:"traffic_change,omitempty"`
	RankChange    *float64 `json:"rank_change,omitempty"`
	CTRChange     *float64 `json:"ctr_change,omitempty"`
	SourceChanged bool     `json:"source_changed"`
	SERPChanged   bool     `json:"serp_changed"`
	IntentChanged bool     `json:"intent_changed"`
	EvidenceURL   string   `json:"evidence_url,omitempty"`
}

type ContentRefreshResult struct {
	Action     string   `json:"action"`
	TargetURL  string   `json:"target_url,omitempty"`
	SnapshotID string   `json:"snapshot_id,omitempty"`
	Reasons    []string `json:"reasons"`
}

type ContentVersion struct {
	Number    int             `json:"number"`
	CreatedAt time.Time       `json:"created_at"`
	Reason    string          `json:"reason"`
	Brief     *ContentBrief   `json:"brief,omitempty"`
	Article   *ContentArticle `json:"article,omitempty"`
	FactCheck *FactReport     `json:"fact_check,omitempty"`
	Quality   *ContentQuality `json:"quality,omitempty"`
}

type ContentPublication struct {
	Sources      []ContentSource `json:"sources"`
	ID           string          `json:"id"`
	Version      int             `json:"version"`
	Status       string          `json:"status"`
	Article      ContentArticle  `json:"article"`
	Checksum     string          `json:"checksum"`
	PublishedURL string          `json:"published_url,omitempty"`
	VerifiedAt   *time.Time      `json:"verified_at,omitempty"`
}

type ContentWorkspace struct {
	Project        ContentProject        `json:"project"`
	Evidence       ContentEvidence       `json:"evidence"`
	Strategy       *ContentStrategy      `json:"strategy,omitempty"`
	Brief          *ContentBrief         `json:"brief,omitempty"`
	BriefVersion   int                   `json:"brief_version"`
	Outline        []ContentHeading      `json:"outline,omitempty"`
	Article        *ContentArticle       `json:"article,omitempty"`
	ArticleVersion int                   `json:"article_version"`
	Facts          []ContentFact         `json:"facts"`
	Sources        []ContentSource       `json:"sources"`
	FactCheck      *FactReport           `json:"fact_check,omitempty"`
	Review         *EditorialReview      `json:"review,omitempty"`
	Quality        *ContentQuality       `json:"quality,omitempty"`
	Refresh        *ContentRefreshResult `json:"refresh,omitempty"`
	RefreshSignals RefreshSignals        `json:"refresh_signals"`
	History        []ContentVersion      `json:"history"`
	Publications   []ContentPublication  `json:"publications"`
	Assets         []SitePage            `json:"-"`
	LastWriterID   string                `json:"-"`
	SiteURL        string                `json:"-"`
}

type ContentAction struct {
	Action   string          `json:"action"`
	Revision int             `json:"revision"`
	Version  int             `json:"version,omitempty"`
	Brief    *ContentBrief   `json:"brief,omitempty"`
	Article  *ContentArticle `json:"article,omitempty"`
	Source   *ContentSource  `json:"source,omitempty"`
	Signals  *RefreshSignals `json:"signals,omitempty"`
	Note     string          `json:"note,omitempty"`
	URL      string          `json:"url,omitempty"`
}

type ContentModelProfile struct {
	ProviderID                  string  `json:"provider_id"`
	Role                        string  `json:"role"`
	SystemPrompt                string  `json:"system_prompt"`
	Temperature                 float64 `json:"temperature"`
	MaxInputTokens              int     `json:"max_input_tokens"`
	DailyTokens                 int64   `json:"daily_tokens"`
	DailyCostMicros             int64   `json:"daily_cost_micros"`
	InputMicrosPerMillion       int64   `json:"input_micros_per_million"`
	CachedInputMicrosPerMillion *int64  `json:"cached_input_micros_per_million,omitempty"`
	OutputMicrosPerMillion      int64   `json:"output_micros_per_million"`
	MaxRetries                  int     `json:"max_retries"`
	RetrySeconds                int     `json:"retry_seconds"`
}

type ContentModelRequest struct {
	Stage    string          `json:"stage"`
	Language string          `json:"language"`
	System   string          `json:"system"`
	Input    json.RawMessage `json:"input"`
}

type ContentModelResponse struct {
	Text          string          `json:"text"`
	Model         string          `json:"model"`
	InputTokens   int64           `json:"input_tokens"`
	OutputTokens  int64           `json:"output_tokens"`
	ReceiptID     string          `json:"receipt_id,omitempty"`
	ResponseID    string          `json:"-"`
	UsageReported bool            `json:"-"`
	Diagnostic    ProviderReceipt `json:"-"`
}

type ContentStageResult struct {
	Strategy       *ContentStrategy
	Brief          *ContentBrief
	Sources        []SourceDocument
	Facts          []ContentFact
	Outline        []ContentHeading
	Article        *ContentArticle
	FactCheck      *FactReport
	Review         *EditorialReview
	Quality        *ContentQuality
	Refresh        *ContentRefreshResult
	Writer         string
	WriterID       string
	Publication    *ContentPublication
	SERPSnapshotID string
}

type ContentModelReservation struct {
	ID         string
	Provider   Provider
	Profile    ContentModelProfile
	APIKey     string
	Cached     *ContentModelResponse
	DeferUntil time.Time
}

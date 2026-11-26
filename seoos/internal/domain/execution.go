package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	ExecutionWordPress = "wordpress"
	ExecutionGeneric   = "generic"
	ExecutionGSC       = "gsc"
	ExecutionGA4       = "ga4"
	ExecutionBing      = "bing"
	ExecutionIndexNow  = "indexnow"

	ExecConnectionTest = "cms_connection_test"
	ExecPageSync       = "cms_page_sync"
	ExecMediaUpload    = "cms_media_upload"
	ExecPublication    = "cms_publication"
	ExecVerify         = "cms_publication_verify"
	ExecGSCSync        = "gsc_sync"
	ExecGA4Sync        = "ga4_sync"
	ExecBingSync       = "bing_sync"
	ExecSitemap        = "sitemap_refresh"
	ExecIndexNow       = "indexnow_submit"
	ExecInspection     = "url_inspection"
	ExecPDF            = "pdf_extraction"
	ExecBrowser        = "browser_extraction"
)

const (
	PublicationPending    = "PENDING"
	PublicationPreparing  = "PREPARING"
	PublicationDraft      = "DRAFT_CREATED"
	PublicationScheduled  = "SCHEDULED"
	PublicationPublished  = "PUBLISHED"
	PublicationVerifying  = "VERIFYING"
	PublicationVerified   = "VERIFIED"
	PublicationFailed     = "FAILED"
	PublicationRetry      = "RETRY_WAIT"
	PublicationCancelled  = "CANCELLED"
	PublicationRollback   = "ROLLBACK_REQUIRED"
	PublicationRolledBack = "ROLLED_BACK"
)

const (
	IndexDiscovered = "DISCOVERED"
	IndexSubmitted  = "SUBMITTED"
	IndexCrawled    = "CRAWLED"
	IndexIndexed    = "INDEXED"
	IndexNotIndexed = "NOT_INDEXED"
	IndexUnknown    = "UNKNOWN"
)

var (
	ErrExecutionUnsupported = errors.New("the connection does not support this operation")
	ErrExecutionState       = errors.New("the execution state does not allow this operation")
	ErrExecutionAuthority   = errors.New("explicit execution authorization is required")
)

func IsExecutionKind(kind string) bool {
	switch kind {
	case ExecConnectionTest, ExecPageSync, ExecMediaUpload, ExecPublication, ExecVerify, ExecGSCSync,
		ExecGA4Sync, ExecBingSync, ExecSitemap, ExecIndexNow, ExecInspection, ExecPDF, ExecBrowser:
		return true
	}
	return false
}

func IsCMSConnection(kind string) bool { return kind == ExecutionWordPress || kind == ExecutionGeneric }

type ExecutionConnectionInput struct {
	SiteID            string `json:"site_id"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	BaseURL           string `json:"base_url,omitempty"`
	Property          string `json:"property,omitempty"`
	KeyLocation       string `json:"key_location,omitempty"`
	RedirectURI       string `json:"redirect_uri,omitempty"`
	Enabled           bool   `json:"enabled"`
	AllowWrites       bool   `json:"allow_writes"`
	RequestsPerMinute int    `json:"requests_per_minute"`
	RequestsPerDay    int    `json:"requests_per_day"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
}

type ExecutionCredential struct {
	Username     string     `json:"username,omitempty"`
	Secret       string     `json:"secret,omitempty"`
	ClientID     string     `json:"client_id,omitempty"`
	ClientSecret string     `json:"client_secret,omitempty"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	AccessToken  string     `json:"access_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type ExecutionConnection struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	ExecutionConnectionInput
	Version      int64           `json:"version"`
	Configured   bool            `json:"configured"`
	Capabilities CMSCapabilities `json:"capabilities"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type ExecutionConnectionSave struct {
	ExecutionConnectionInput
	Version    int64                `json:"version,omitempty"`
	Credential *ExecutionCredential `json:"credential,omitempty"`
}

type CMSCapabilities struct {
	ReadPages  bool     `json:"read_pages"`
	WritePages bool     `json:"write_pages"`
	Publish    bool     `json:"publish"`
	Schedule   bool     `json:"schedule"`
	ReadMedia  bool     `json:"read_media"`
	WriteMedia bool     `json:"write_media"`
	SEOPlugin  string   `json:"seo_plugin,omitempty"`
	SEOFields  []string `json:"seo_fields,omitempty"`
}

type CMSTerm struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
}

type CMSSiteInfo struct {
	Name         string          `json:"name"`
	URL          string          `json:"url"`
	Description  string          `json:"description,omitempty"`
	Categories   []CMSTerm       `json:"categories,omitempty"`
	Tags         []CMSTerm       `json:"tags,omitempty"`
	Capabilities CMSCapabilities `json:"capabilities"`
	Raw          json.RawMessage `json:"-"`
}

type CMSPage struct {
	ID          string          `json:"id"`
	Resource    string          `json:"resource"`
	URL         string          `json:"url,omitempty"`
	Slug        string          `json:"slug,omitempty"`
	Status      string          `json:"status"`
	Title       string          `json:"title"`
	Body        string          `json:"body,omitempty"`
	Excerpt     string          `json:"excerpt,omitempty"`
	SEOTitle    string          `json:"seo_title,omitempty"`
	Description string          `json:"description,omitempty"`
	Canonical   string          `json:"canonical,omitempty"`
	ModifiedAt  *time.Time      `json:"modified_at,omitempty"`
	ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
	HTTPStatus  int             `json:"http_status,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

type CMSPageInput struct {
	Resource    string     `json:"resource"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Excerpt     string     `json:"excerpt,omitempty"`
	SEOTitle    string     `json:"seo_title,omitempty"`
	Description string     `json:"description,omitempty"`
	Canonical   string     `json:"canonical,omitempty"`
	Status      string     `json:"status,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

type CMSMedia struct {
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Title     string          `json:"title,omitempty"`
	MediaType string          `json:"media_type,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

type CMSSitemap struct {
	URL string
	XML string
}

type CMSConnector interface {
	TestConnection(context.Context) (CMSCapabilities, error)
	GetSiteInfo(context.Context) (CMSSiteInfo, error)
	ListPages(context.Context, string, string, int) ([]CMSPage, error)
	GetPage(context.Context, string, string) (CMSPage, error)
	CreatePage(context.Context, CMSPageInput) (CMSPage, error)
	UpdatePage(context.Context, string, CMSPageInput) (CMSPage, error)
	PublishPage(context.Context, string, CMSPageInput) (CMSPage, error)
	GetMedia(context.Context, int) ([]CMSMedia, error)
	UploadMedia(context.Context, string, string, []byte) (CMSMedia, error)
	GetSitemap(context.Context) (CMSSitemap, error)
	ObservePublic(context.Context, string) (CMSPage, error)
}

type ExecutionPublicationInput struct {
	SiteID         string `json:"site_id"`
	ConnectionID   string `json:"connection_id"`
	PackageID      string `json:"package_id"`
	Resource       string `json:"resource"`
	RemoteID       string `json:"remote_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type ExecutionPublication struct {
	VerificationJobID string     `json:"verification_job_id,omitempty"`
	DraftID           string     `json:"draft_id,omitempty"`
	ScheduledAt       *time.Time `json:"scheduled_at,omitempty"`
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	ArticleVersion    int        `json:"article_version"`
	Title             string     `json:"title"`
	ExecutionPublicationInput
	Status    string    `json:"status"`
	URL       string    `json:"url,omitempty"`
	TargetID  string    `json:"target_id,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ExecutionPublicationEvent struct {
	Status    string    `json:"status"`
	Action    string    `json:"action"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ExecutionJobInput struct {
	SiteID         string     `json:"site_id"`
	ConnectionID   string     `json:"connection_id,omitempty"`
	PublicationID  string     `json:"publication_id,omitempty"`
	Kind           string     `json:"kind"`
	Operation      string     `json:"operation,omitempty"`
	Note           string     `json:"note,omitempty"`
	URL            string     `json:"url,omitempty"`
	From           string     `json:"from,omitempty"`
	To             string     `json:"to,omitempty"`
	ScheduledAt    *time.Time `json:"scheduled_at,omitempty"`
	Filename       string     `json:"filename,omitempty"`
	MediaType      string     `json:"media_type,omitempty"`
	MediaBase64    string     `json:"media_base64,omitempty"`
	IdempotencyKey string     `json:"idempotency_key"`
	Priority       int        `json:"priority"`
}

type ExecutionJobRecord struct {
	Job     Job               `json:"job"`
	Request ExecutionJobInput `json:"request"`
}

type ExecutionProperty struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Permission string `json:"permission,omitempty"`
}

type ExecutionMetric struct {
	Date     string             `json:"date"`
	Page     string             `json:"page,omitempty"`
	Query    string             `json:"query,omitempty"`
	Country  string             `json:"country,omitempty"`
	Device   string             `json:"device,omitempty"`
	Language string             `json:"language,omitempty"`
	Values   map[string]float64 `json:"values"`
}

type ExecutionIndexRecord struct {
	URL         string     `json:"url"`
	Status      string     `json:"status"`
	Provider    string     `json:"provider"`
	Evidence    string     `json:"evidence,omitempty"`
	ObservedAt  time.Time  `json:"observed_at"`
	LastCrawled *time.Time `json:"last_crawled,omitempty"`
}

type ExecutionSitemap struct {
	JobID    string     `json:"job_id,omitempty"`
	Entries  int        `json:"entries,omitempty"`
	Parts    int        `json:"parts,omitempty"`
	URL      string     `json:"url"`
	Provider string     `json:"provider"`
	Status   string     `json:"status,omitempty"`
	LastRead *time.Time `json:"last_read,omitempty"`
	Warnings *int64     `json:"warnings,omitempty"`
	Errors   *int64     `json:"errors,omitempty"`
}

type ExecutionSync struct {
	Properties []ExecutionProperty    `json:"properties"`
	Metrics    []ExecutionMetric      `json:"metrics"`
	Sitemaps   []ExecutionSitemap     `json:"sitemaps"`
	Indexes    []ExecutionIndexRecord `json:"indexes"`
	Requests   int                    `json:"requests"`
	Truncated  bool                   `json:"truncated"`
}

type ExecutionExtraction struct {
	URL       string            `json:"url"`
	Kind      string            `json:"kind"`
	Title     string            `json:"title,omitempty"`
	Text      string            `json:"text,omitempty"`
	Pages     int               `json:"pages,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Requests  int               `json:"requests,omitempty"`
	Truncated bool              `json:"truncated"`
}

type ExecutionFilter struct {
	SiteID, ConnectionID, Kind, From, To, Page, Country string
	Limit, Offset                                       int
}

type SitemapEntry struct {
	URL       string             `json:"url"`
	LastMod   string             `json:"lastmod,omitempty"`
	Alternate []SitemapAlternate `json:"alternate,omitempty"`
}

type SitemapAlternate struct {
	Language string `json:"hreflang"`
	URL      string `json:"url"`
}

type SitemapDocument struct {
	URL       string            `json:"url"`
	Entries   []SitemapEntry    `json:"entries"`
	Children  []string          `json:"children,omitempty"`
	Files     map[string]string `json:"files,omitempty"`
	Truncated bool              `json:"truncated"`
}

type ExecutionWork struct {
	Input             ExecutionJobInput
	ConnectionVersion int64
	PriorStatus       string
	StartedWrite      bool
	Applied           json.RawMessage
}

type ExecutionPackage struct {
	ProjectID string `json:"project_id"`
	ContentPublication
}

type ExecutionRelease struct {
	Original *CMSPage
	ExecutionPublication
	Article ContentArticle
	Before  *CMSPage
	After   *CMSPage
}

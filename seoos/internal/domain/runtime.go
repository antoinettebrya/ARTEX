package domain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	JobSiteInventory = "site_inventory"
	JobSiteBrief     = "site_brief"
	MaxJobAttempts   = 3
)

var (
	ErrSiteInactive          = errors.New("site is paused or archived")
	ErrIdempotencyConflict   = errors.New("idempotency key is bound to a different job")
	ErrJobSettled            = errors.New("job has already finished")
	ErrLeaseLost             = errors.New("job lease is no longer current")
	ErrProviderDisabled      = errors.New("provider is disabled")
	ErrProviderNameTaken     = errors.New("provider name already exists")
	ErrProviderNotConfigured = errors.New("provider credential encryption is unavailable")
	ErrProviderTestLimited   = errors.New("provider connection test limit reached")
)

type JobInput struct {
	SiteID         string `json:"site_id"`
	ProviderID     string `json:"provider_id,omitempty"`
	Kind           string `json:"kind"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (in JobInput) Validate() error {
	if !ValidID(in.SiteID) {
		return invalid("site_id", "must be a canonical UUID")
	}
	switch in.Kind {
	case JobSiteInventory:
		if in.ProviderID != "" {
			return invalid("provider_id", "is only accepted for site_brief")
		}
	case JobSiteBrief:
		if !ValidID(in.ProviderID) {
			return invalid("provider_id", "must be a canonical UUID for site_brief")
		}
	default:
		return invalid("kind", "must be site_inventory or site_brief")
	}
	if len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 120 || !identifier(in.IdempotencyKey) {
		return invalid("idempotency_key", "must contain 8 to 120 letters, digits or . _ : -")
	}
	return nil
}

func identifier(value string) bool {
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

type Job struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	JobInput
	State       string          `json:"state"`
	Attempts    int             `json:"attempts"`
	AvailableAt time.Time       `json:"available_at"`
	ErrorCode   string          `json:"error_code,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type JobRepository interface {
	EnqueueJob(context.Context, string, JobInput) (Job, bool, error)
	ListJobs(context.Context, string, int, int) ([]Job, error)
	GetJob(context.Context, string, string) (Job, error)
	CancelJob(context.Context, string, string) (Job, error)
}

type Lease struct {
	Job   Job
	Token string
}

type JobOutcome struct {
	Result     json.RawMessage
	Code       string
	Retry      bool
	DeferUntil time.Time
	RetryAfter time.Time
}

type ProviderSettings struct {
	Name              string `json:"name"`
	Model             string `json:"model"`
	TimeoutSeconds    int    `json:"timeout_seconds"`
	RequestsPerMinute int    `json:"requests_per_minute"`
	RequestsPerDay    int    `json:"requests_per_day"`
	MaxOutputTokens   int    `json:"max_output_tokens,omitempty"`
	Enabled           bool   `json:"enabled"`
	DailyCostMicros   *int64 `json:"daily_cost_micros,omitempty"`
}

type ProviderInput struct {
	ProviderSettings
	APIKey   string `json:"api_key,omitempty"`
	Version  int64  `json:"version,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Login    string `json:"login,omitempty"`
	Password string `json:"password,omitempty"`
}

func (in ProviderInput) Normalize(creating bool) (ProviderInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !utf8.ValidString(in.Name) || in.Name == "" || utf8.RuneCountInString(in.Name) > 100 || hasControl(in.Name) {
		return in, invalid("name", "must contain 1 to 100 characters without control characters")
	}
	if in.Kind == "" {
		in.Kind = ProviderOpenAI
	}
	if !IsModelProvider(in.Kind) && in.Kind != ProviderDataForSEO {
		return in, invalid("kind", "must be openai, anthropic, gemini, deepseek or dataforseo")
	}
	if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 30 {
		return in, invalid("timeout_seconds", "must be between 1 and 30")
	}
	if in.RequestsPerMinute < 1 || in.RequestsPerMinute > 60 || in.RequestsPerDay < 1 || in.RequestsPerDay > 10000 {
		return in, invalid("request_budget", "set 1 to 60 requests per minute and 1 to 10000 per UTC day")
	}
	if in.Kind == ProviderDataForSEO {
		in.Model, in.MaxOutputTokens = "dataforseo-v3", 0
		if in.DailyCostMicros == nil || *in.DailyCostMicros < 1 || *in.DailyCostMicros > 100000000000 {
			return in, invalid("daily_cost_micros", "set a positive daily USD budget in micros")
		}
		if in.RequestsPerMinute > 12 {
			return in, invalid("requests_per_minute", "DataForSEO supports at most 12 requests per minute here")
		}
		if in.APIKey != "" {
			return in, invalid("api_key", "use the DataForSEO login and password fields")
		}
		if creating || in.Login != "" || in.Password != "" {
			if len(in.Login) < 1 || len(in.Login) > 254 ||
				strings.ContainsFunc(in.Login, func(r rune) bool { return r <= ' ' || r > '~' || r == ':' }) ||
				len(in.Password) < 1 || len(in.Password) > 512 ||
				strings.ContainsFunc(in.Password, func(r rune) bool { return r < ' ' || r > '~' }) {
				return in, invalid("login", "provide a valid DataForSEO login and password together")
			}
		}
	} else {
		if in.Login != "" || in.Password != "" || in.DailyCostMicros != nil {
			return in, invalid("kind", "DataForSEO settings require a DataForSEO provider")
		}
		if in.Model == "" || len(in.Model) > 100 || !identifier(in.Model) {
			return in, invalid("model", "must be a provider model identifier")
		}
		if in.MaxOutputTokens < 16 || in.MaxOutputTokens > 4096 {
			return in, invalid("max_output_tokens", "must be between 16 and 4096")
		}
		if creating || in.APIKey != "" {
			if len(in.APIKey) < 16 || len(in.APIKey) > 512 || strings.ContainsFunc(in.APIKey, func(r rune) bool { return r < '!' || r > '~' }) {
				return in, invalid("api_key", "must contain 16 to 512 non-whitespace ASCII characters")
			}
		}
	}
	if creating && in.Version != 0 || !creating && in.Version < 1 {
		return in, invalid("version", "include a positive version when updating only")
	}
	return in, nil
}

type Provider struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Kind           string `json:"kind"`
	ProviderSettings
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProviderRepository interface {
	ListProviders(context.Context, string, string, int, int) ([]Provider, error)
	SaveProvider(context.Context, string, string, ProviderInput) (Provider, error)
	ReserveProviderTest(context.Context, string, string) (ProviderTestAccess, error)
	RecordProviderTest(context.Context, string, string, ProviderTestResult) error
}

const (
	ProviderCategoryModel = "model"
	ProviderCategoryData  = "data"
)

type ProviderTestAccess struct {
	Provider Provider
	APIKey   string `json:"-"`
	Login    string `json:"-"`
	Password string `json:"-"`
}

type ProviderTestResult struct {
	Status          string `json:"status"`
	DurationMS      int64  `json:"duration_ms"`
	ProviderVersion int64  `json:"provider_version"`
}

type ProviderCall struct {
	Provider   Provider
	APIKey     string
	DeferUntil time.Time
}

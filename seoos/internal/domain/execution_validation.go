package domain

import (
	"encoding/base64"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

func (in ExecutionConnectionInput) Normalize(site Site) (ExecutionConnectionInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.SiteID != site.ID || in.Name == "" || len(in.Name) > 120 || hasControl(in.Name) {
		return in, invalid("name", "choose a valid site and connection name")
	}
	u, err := EvidenceURL(site.BaseURL)
	if err != nil || u.Scheme != "https" || u.RawQuery != "" {
		return in, invalid("base_url", "the registered site must use HTTPS")
	}
	if in.BaseURL != "" && strings.TrimSuffix(in.BaseURL, "/") != strings.TrimSuffix(site.BaseURL, "/") {
		return in, invalid("base_url", "use the registered site URL")
	}
	in.BaseURL = site.BaseURL
	if in.RequestsPerMinute == 0 {
		in.RequestsPerMinute = 30
	}
	if in.RequestsPerDay == 0 {
		in.RequestsPerDay = 500
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = 12
	}
	if in.RequestsPerMinute < 1 || in.RequestsPerMinute > 120 || in.RequestsPerDay < 1 || in.RequestsPerDay > 25000 ||
		in.TimeoutSeconds < 1 || in.TimeoutSeconds > 30 {
		return in, invalid("limits", "set bounded request budgets and timeout")
	}
	in.Property = strings.TrimSpace(in.Property)
	switch in.Kind {
	case ExecutionWordPress, ExecutionGeneric:
		if in.Property != "" || in.RedirectURI != "" || in.KeyLocation != "" {
			return in, invalid("connection", "these settings do not belong to a CMS connection")
		}
	case ExecutionGSC:
		name := strings.ToLower(strings.TrimPrefix(in.Property, "sc-domain:"))
		if strings.HasPrefix(in.Property, "sc-domain:") {
			if name != u.Hostname() && !strings.HasSuffix(u.Hostname(), "."+name) || name == "" || !strings.Contains(name, ".") {
				return in, invalid("property", "use a property encompassing the registered site")
			}
		} else {
			p, err := EvidenceURL(in.Property)
			if err != nil || !BelongsToSite(in.Property, u) || p.RawQuery != "" {
				return in, invalid("property", "use the exact site URL-prefix property")
			}
		}
	case ExecutionGA4:
		value := strings.TrimPrefix(in.Property, "properties/")
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil || n == 0 {
			return in, invalid("property", "use a numeric GA4 property ID")
		}
		in.Property = "properties/" + strconv.FormatUint(n, 10)
	case ExecutionBing:
		if in.Property == "" {
			in.Property = strings.TrimSuffix(site.BaseURL, "/") + "/"
		}
		p, err := EvidenceURL(in.Property)
		if err != nil || !BelongsToSite(in.Property, u) || !BelongsToSite(site.BaseURL, p) || p.RawQuery != "" {
			return in, invalid("property", "use the registered Bing site URL")
		}
	case ExecutionIndexNow:
		location, err := EvidenceURL(in.KeyLocation)
		if err != nil || !BelongsToSite(site.BaseURL, location) || location.RawQuery != "" {
			return in, invalid("key_location", "use a key file on the registered site")
		}
	default:
		return in, invalid("kind", "choose a supported connection")
	}
	if in.Kind == ExecutionGSC || in.Kind == ExecutionGA4 {
		if in.AllowWrites {
			return in, invalid("allow_writes", "Google connections are read-only")
		}
		if in.RedirectURI != "" {
			callback, err := EvidenceURL(in.RedirectURI)
			if err != nil || callback.Scheme != "https" || callback.RawQuery != "" ||
				strings.TrimSuffix(callback.Path, "/") != "/seo/execution" {
				return in, invalid("redirect_uri", "use the registered HTTPS execution page as callback")
			}
		}
	} else if in.RedirectURI != "" {
		return in, invalid("redirect_uri", "only used by Google OAuth")
	}
	return in, nil
}

func (s ExecutionCredential) Validate(in ExecutionConnectionInput) error {
	for _, value := range []string{s.Username, s.Secret, s.ClientID, s.ClientSecret, s.RefreshToken, s.AccessToken} {
		if len(value) > 4096 || hasControl(value) {
			return invalid("credential", "invalid credential format")
		}
	}
	invalidCred := invalid("credential", "provide the connection's complete credential")
	switch in.Kind {
	case ExecutionWordPress:
		if strings.TrimSpace(s.Username) == "" || len(s.Username) > 200 || strings.Contains(s.Username, ":") || strings.TrimSpace(s.Secret) == "" {
			return invalidCred
		}
	case ExecutionGeneric, ExecutionBing:
		if strings.TrimSpace(s.Secret) == "" || strings.ContainsAny(s.Secret, " \t") {
			return invalidCred
		}
	case ExecutionIndexNow:
		if len(s.Secret) < 8 || len(s.Secret) > 128 ||
			strings.Trim(s.Secret, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return invalidCred
		}
		loc, _ := url.Parse(in.KeyLocation)
		if loc == nil || path.Base(loc.Path) != s.Secret+".txt" {
			return invalid("key_location", "the key filename must match the IndexNow key")
		}
	case ExecutionGSC, ExecutionGA4:
		if s.ClientID == "" || s.ClientSecret == "" {
			return invalidCred
		}
	default:
		return invalidCred
	}
	return nil
}

func (in ExecutionPublicationInput) Validate() error {
	if !ValidID(in.SiteID) || !ValidID(in.ConnectionID) || !ValidID(in.PackageID) || !executionKey(in.IdempotencyKey) ||
		(in.Resource != "post" && in.Resource != "page") || len(in.RemoteID) > 100 ||
		in.RemoteID != "" && !identifier(in.RemoteID) {
		return invalid("publication", "select a site, CMS, approved package and idempotency key")
	}
	return nil
}

func executionKey(key string) bool { return len(key) >= 8 && len(key) <= 120 && identifier(key) }

func (in ExecutionJobInput) Validate() error {
	if !IsExecutionKind(in.Kind) || !ValidID(in.SiteID) || !executionKey(in.IdempotencyKey) ||
		in.Priority < 0 || in.Priority > 100 {
		return invalid("job", "choose a supported operation, site, key and priority")
	}
	contentOnly := in.Kind == ExecPDF || in.Kind == ExecBrowser || in.Kind == ExecSitemap
	if !contentOnly && !ValidID(in.ConnectionID) || contentOnly && in.ConnectionID != "" && !ValidID(in.ConnectionID) {
		return invalid("connection_id", "select the integration required by this task")
	}
	publishing := in.Kind == ExecPublication || in.Kind == ExecVerify
	if publishing && !ValidID(in.PublicationID) || !publishing && in.PublicationID != "" {
		return invalid("publication_id", "an execution publication is required only for CMS operations")
	}
	if in.Kind == ExecPublication {
		if in.Operation != "draft" && in.Operation != "publish" && in.Operation != "rollback" {
			return invalid("operation", "choose draft, publish or rollback")
		}
		if (in.Operation == "publish" || in.Operation == "rollback") && strings.TrimSpace(in.Note) == "" {
			return invalid("note", "record the independent approval or rollback reason")
		}
	} else if in.Operation != "" {
		return invalid("operation", "operation is only accepted on publication jobs")
	}
	if len(in.Note) > 1500 || hasControl(strings.ReplaceAll(in.Note, "\n", "")) {
		return invalid("note", "limit notes to 1500 characters")
	}
	if in.ScheduledAt != nil && (in.Kind != ExecPublication || in.Operation != "publish" ||
		time.Until(*in.ScheduledAt) > 365*24*time.Hour) {
		return invalid("scheduled_at", "provide a future time for a scheduled publication")
	}
	needsURL := in.Kind == ExecPDF || in.Kind == ExecBrowser || in.Kind == ExecInspection || in.Kind == ExecIndexNow
	if needsURL || in.Kind == ExecSitemap && in.URL != "" {
		u, err := EvidenceURL(in.URL)
		if err != nil || u.Scheme != "https" {
			return invalid("url", "provide a scoped HTTPS URL")
		}
	} else if in.URL != "" {
		return invalid("url", "use a URL only for indexing or extraction operations")
	}
	sync := in.Kind == ExecGSCSync || in.Kind == ExecGA4Sync || in.Kind == ExecBingSync
	if (in.From != "" || in.To != "") && !sync {
		return invalid("range", "date range is only used for analytics")
	}
	if _, _, err := ExecutionDateRange(in.From, in.To); err != nil {
		return err
	}
	if in.Kind == ExecMediaUpload {
		allowed := in.MediaType == "image/png" || in.MediaType == "image/jpeg" || in.MediaType == "image/webp" || in.MediaType == "image/gif"
		if !allowed || len(in.Filename) < 3 || len(in.Filename) > 140 || path.Base(in.Filename) != in.Filename ||
			strings.Contains(in.Filename, "\\") || hasControl(in.Filename) || len(in.MediaBase64) > base64.StdEncoding.EncodedLen(4<<20) {
			return invalid("media", "use an image up to 4 MiB with a safe filename")
		}
		data, err := base64.StdEncoding.DecodeString(in.MediaBase64)
		if err != nil || len(data) == 0 || len(data) > 4<<20 {
			return invalid("media", "provide valid base64 image data")
		}
	} else if in.Filename != "" || in.MediaType != "" || in.MediaBase64 != "" {
		return invalid("media", "media payload is only used by upload jobs")
	}
	return nil
}

func ExecutionDateRange(from, to string) (time.Time, time.Time, error) {
	var a, b time.Time
	var err error
	if from != "" {
		a, err = time.Parse(time.DateOnly, from)
		if err != nil {
			return a, b, invalid("from", "use YYYY-MM-DD")
		}
	}
	if to != "" {
		b, err = time.Parse(time.DateOnly, to)
		if err != nil {
			return a, b, invalid("to", "use YYYY-MM-DD")
		}
	}
	if !a.IsZero() && !b.IsZero() && (b.Before(a) || b.Sub(a) >= 31*24*time.Hour) || !b.IsZero() && b.After(time.Now().UTC()) {
		return a, b, invalid("range", "use a historical date range of up to 31 days")
	}
	return a, b, nil
}

func (f ExecutionFilter) ValidateReportFilters() error {
	if len(f.Country) > 160 || hasControl(f.Country) {
		return invalid("country", "choose a country code or display name")
	}
	if len(f.Page) > 2048 || hasControl(f.Page) {
		return invalid("page", "choose a valid reported page URL")
	}
	return nil
}

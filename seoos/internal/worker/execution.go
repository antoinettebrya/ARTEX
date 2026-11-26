package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/executioncms"
	"github.com/Autumn-27/artex/seoos/internal/executionextract"
	"github.com/Autumn-27/artex/seoos/internal/executionhttp"
	"github.com/Autumn-27/artex/seoos/internal/executionsearch"
	"github.com/Autumn-27/artex/seoos/internal/store"
)

type ExecutionServices struct {
	Fixture *http.Client
}

type executionRuntime interface {
	ExecutionJob(context.Context, domain.Lease) (domain.ExecutionWork, error)
	ExecutionConnectionAccess(context.Context, string, string, string) (domain.ExecutionConnection, domain.ExecutionCredential, error)
	UseExecutionRequest(context.Context, string, string, string, int64, *domain.Lease) error
	SaveExecutionTokens(context.Context, domain.ExecutionConnection, domain.ExecutionCredential) error
	ExecutionSyncDates(context.Context, domain.ExecutionConnection, domain.ExecutionJobInput) (string, string, error)
	SaveExecutionResult(context.Context, domain.Lease, domain.ExecutionWork, store.ExecutionResult) (json.RawMessage, error)
	CrawlPolicy(context.Context, string, string) (*domain.CrawlPolicy, error)
	ExecutionRelease(context.Context, domain.Lease, domain.ExecutionWork) (domain.ExecutionRelease, error)
	CheckExecutionPackage(context.Context, string, string, string) error
	BeginPublication(context.Context, domain.Lease, domain.ExecutionWork, string) error
	MarkExecutionWrite(context.Context, domain.Lease, domain.ExecutionWork, *domain.CMSPage) error
	ClearExecutionWrite(context.Context, domain.Lease) error
	CompletePublication(context.Context, domain.Lease, domain.ExecutionWork, string, string, *domain.CMSPage, *domain.CMSPage) (json.RawMessage, error)
	PublicationRetry(context.Context, domain.Lease, domain.ExecutionWork, string, string) error
}

type executionScope struct {
	repo       executionRuntime
	site       domain.Site
	lease      domain.Lease
	work       domain.ExecutionWork
	connection domain.ExecutionConnection
	secret     domain.ExecutionCredential
	client     *http.Client
}

func executionJob(ctx context.Context, repo repository, lease domain.Lease, site domain.Site, services *ExecutionServices) domain.JobOutcome {
	runtime, ok := repo.(executionRuntime)
	if !ok || services == nil {
		return domain.JobOutcome{Code: "execution_unavailable"}
	}
	work, err := runtime.ExecutionJob(ctx, lease)
	if err != nil {
		return executionFailure(err)
	}
	if len(work.Applied) > 0 {
		var done struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(work.Applied, &done) == nil && done.Error != "" {
			return domain.JobOutcome{Code: done.Error}
		}
		return domain.JobOutcome{Result: work.Applied}
	}
	s := executionScope{repo: runtime, site: site, lease: lease, work: work}
	timeout := 12 * time.Second
	if id := work.Input.ConnectionID; id != "" {
		s.connection, s.secret, err = runtime.ExecutionConnectionAccess(ctx, site.OrganizationID, site.ID, id)
		if err != nil {
			return s.failure(ctx, err)
		}
		if s.connection.Version != work.ConnectionVersion {
			return s.failure(ctx, domain.ErrVersionConflict)
		}
		timeout = time.Duration(s.connection.TimeoutSeconds) * time.Second
	}
	s.client = executionhttp.Client(services.Fixture, timeout, func(ctx context.Context) error {
		return runtime.UseExecutionRequest(ctx, site.OrganizationID, site.ID, work.Input.ConnectionID, work.ConnectionVersion, &lease)
	})
	in := work.Input
	if domain.IsCMSConnection(s.connection.Kind) {
		conn, err := executioncms.New(s.connection, s.secret, s.client)
		if err != nil {
			return s.failure(ctx, err)
		}
		if in.Kind == domain.ExecPublication || in.Kind == domain.ExecVerify {
			return s.publication(ctx, conn)
		}
		return s.cmsReadOrMedia(ctx, conn)
	}
	return s.execute(ctx)
}

func (s executionScope) execute(ctx context.Context) domain.JobOutcome {
	var out store.ExecutionResult
	var err error
	in, c := s.work.Input, s.connection
	switch in.Kind {
	case domain.ExecPDF, domain.ExecBrowser, domain.ExecSitemap:
		if in.Kind == domain.ExecSitemap && (c.Kind == domain.ExecutionGSC || c.Kind == domain.ExecutionBing) {
			out.Sync, err = s.search(ctx, true)
			break
		}
		out, err = s.extract(ctx)
	case domain.ExecGSCSync, domain.ExecGA4Sync, domain.ExecBingSync, domain.ExecConnectionTest:
		if in.Kind == domain.ExecConnectionTest && c.Kind == domain.ExecutionIndexNow {
			if err = s.verifyIndexNowKey(ctx); err == nil {
				out.Info = &domain.CMSSiteInfo{Name: c.Name, URL: c.BaseURL}
			}
			break
		}
		out.Sync, err = s.search(ctx, in.Kind == domain.ExecConnectionTest)
	case domain.ExecInspection:
		client := executionsearch.New(s.client)
		var secret domain.ExecutionCredential
		secret, err = s.searchCredential(ctx, client)
		if err == nil {
			var record domain.ExecutionIndexRecord
			record, err = client.Inspect(ctx, c, secret, in.URL)
			if err == nil {
				out.Indexes = []domain.ExecutionIndexRecord{record}
			}
		}
	case domain.ExecIndexNow:
		return s.submit(ctx)
	default:
		err = domain.ErrExecutionUnsupported
	}
	if err != nil {
		return s.failure(ctx, err)
	}
	return s.save(ctx, out)
}

func (s executionScope) cmsReadOrMedia(ctx context.Context, c domain.CMSConnector) domain.JobOutcome {
	var out store.ExecutionResult
	var err error
	switch s.work.Input.Kind {
	case domain.ExecConnectionTest:
		var capable domain.CMSCapabilities
		capable, err = c.TestConnection(ctx)
		if err == nil {
			var info domain.CMSSiteInfo
			info, err = c.GetSiteInfo(ctx)
			if err == nil {
				info.Capabilities = capable
				out.Info = &info
			}
		}
	case domain.ExecPageSync:
		for _, resource := range []string{"post", "page"} {
			var rows []domain.CMSPage
			rows, err = c.ListPages(ctx, resource, "", 100)
			if err != nil {
				break
			}
			out.Pages = append(out.Pages, rows...)
		}
		if err == nil {
			out.Media, err = c.GetMedia(ctx, 100)
			if errors.Is(err, domain.ErrExecutionUnsupported) {
				err = nil
			}
		}
	case domain.ExecMediaUpload:
		if s.work.StartedWrite {
			return domain.JobOutcome{Code: "cms_media_receipt_unknown"}
		}
		in := s.work.Input
		image, e := base64.StdEncoding.DecodeString(in.MediaBase64)
		if e != nil || http.DetectContentType(image) != in.MediaType {
			return domain.JobOutcome{Code: "media_type_mismatch"}
		}
		if err = s.repo.MarkExecutionWrite(ctx, s.lease, s.work, nil); err != nil {
			break
		}
		s.work.StartedWrite = true
		var m domain.CMSMedia
		m, err = c.UploadMedia(ctx, in.Filename, in.MediaType, image)
		if err != nil {
			return s.failure(ctx, s.resetRejectedWrite(ctx, err))
		}
		out.Media = []domain.CMSMedia{m}
	case domain.ExecSitemap:
		var source domain.CMSSitemap
		source, err = c.GetSitemap(ctx)
		if err == nil {
			var doc domain.SitemapDocument
			doc, err = executionextract.Sitemap(ctx, s.client, s.site, source.URL, source.XML)
			if err == nil {
				out.Sitemap = &doc
			}
		}
	default:
		err = domain.ErrExecutionUnsupported
	}
	if err != nil {
		return s.failure(ctx, err)
	}
	return s.save(ctx, out)
}

func (s executionScope) searchCredential(ctx context.Context, client *executionsearch.Client) (domain.ExecutionCredential, error) {
	secret := s.secret
	if c := s.connection; c.Kind == domain.ExecutionGSC || c.Kind == domain.ExecutionGA4 {
		if secret.AccessToken == "" || secret.ExpiresAt == nil || time.Until(*secret.ExpiresAt) < time.Minute {
			fresh, err := client.Refresh(ctx, c, secret)
			if err != nil {
				return secret, err
			}
			if err = s.repo.SaveExecutionTokens(ctx, c, fresh); err != nil {
				return secret, err
			}
			secret = fresh
		}
	}
	return secret, nil
}

func (s executionScope) search(ctx context.Context, onlySitemaps bool) (*domain.ExecutionSync, error) {
	client := executionsearch.New(s.client)
	secret, err := s.searchCredential(ctx, client)
	if err != nil {
		return nil, err
	}
	var from, to string
	if !onlySitemaps {
		from, to, err = s.repo.ExecutionSyncDates(ctx, s.connection, s.work.Input)
		if err != nil {
			return nil, err
		}
	}
	limit := min(14, s.connection.RequestsPerMinute, s.connection.RequestsPerDay)
	if (s.connection.Kind == domain.ExecutionGSC || s.connection.Kind == domain.ExecutionGA4) &&
		(s.secret.ExpiresAt == nil || time.Until(*s.secret.ExpiresAt) < time.Minute || s.secret.AccessToken == "") {
		limit--
	}
	// Partial reports preserve the data read within the configured call allowance.
	out, err := client.Sync(ctx, s.connection, secret, from, to, limit)
	if onlySitemaps {
		out.Metrics = nil
		out.Indexes = nil
	}
	return &out, err
}

func (s executionScope) extract(ctx context.Context) (store.ExecutionResult, error) {
	var out store.ExecutionResult
	policy, err := s.repo.CrawlPolicy(ctx, s.site.OrganizationID, s.site.ID)
	if err != nil {
		return out, err
	}
	if policy == nil || !policy.Enabled {
		return out, domain.ErrCrawlUnauthorized
	}
	in := s.work.Input
	if in.Kind == domain.ExecSitemap {
		target := in.URL
		if target == "" {
			target = policy.SitemapURL
		}
		doc, err := executionextract.Sitemap(ctx, s.client, s.site, target, "")
		out.Sitemap = &doc
		return out, err
	}
	target, err := contentinventory.ValidatePageURL(s.site, *policy, in.URL)
	if err != nil {
		return out, err
	}
	var artifact domain.ExecutionExtraction
	if in.Kind == domain.ExecPDF {
		artifact, err = executionextract.PDF(ctx, s.client, target)
	} else {
		artifact, err = executionextract.Browser(ctx, s.client, s.site, *policy, target)
	}
	out.Extraction = &artifact
	return out, err
}

func (s executionScope) submit(ctx context.Context) domain.JobOutcome {
	c, in := s.connection, s.work.Input
	if !c.AllowWrites {
		return s.failure(ctx, domain.ErrExecutionAuthority)
	}
	if c.Kind == domain.ExecutionIndexNow {
		if err := s.verifyIndexNowKey(ctx); err != nil {
			return s.failure(ctx, err)
		}
	}
	u, err := url.Parse(in.URL)
	if err != nil || !domain.BelongsToSite(s.site.BaseURL, u) {
		return s.failure(ctx, domain.ErrExecutionAuthority)
	}
	status, err := executionsearch.New(s.client).SubmitStatus(ctx, c, s.secret, []string{in.URL})
	if err != nil {
		problem := executionFailure(err)
		_ = s.repoSubmissionFailure(ctx, status, problem.Code)
		return problem
	}
	code := "accepted"
	if status == http.StatusAccepted {
		code = "pending_key_validation"
	}
	out := store.ExecutionResult{Submission: map[string]any{"url": in.URL, "status": code, "http_status": status},
		Indexes: []domain.ExecutionIndexRecord{{URL: in.URL, Status: domain.IndexSubmitted, Provider: c.Kind, Evidence: code, ObservedAt: time.Now().UTC()}}}
	return s.save(ctx, out)
}

func (s executionScope) verifyIndexNowKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.connection.KeyLocation, nil)
	if err != nil {
		return &domain.DataError{Code: "indexnow_ownership_unverified"}
	}
	response, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return &domain.DataError{Code: "indexnow_ownership_unavailable", Retry: true}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1025))
	if readErr != nil {
		return &domain.DataError{Code: "indexnow_ownership_unavailable", Retry: true}
	}
	if response.StatusCode != http.StatusOK || len(body) > 1024 || strings.TrimSpace(string(body)) != s.secret.Secret {
		return &domain.DataError{Code: "indexnow_ownership_unverified"}
	}
	return nil
}

func (s executionScope) repoSubmissionFailure(ctx context.Context, status int, code string) error {
	if reporter, ok := s.repo.(interface {
		ExecutionSubmissionFailure(context.Context, domain.Lease, string, string, int, string) error
	}); ok {
		return reporter.ExecutionSubmissionFailure(ctx, s.lease, s.connection.Kind, s.work.Input.URL, status, code)
	}
	return nil
}

func (s executionScope) save(ctx context.Context, out store.ExecutionResult) domain.JobOutcome {
	raw, err := s.repo.SaveExecutionResult(ctx, s.lease, s.work, out)
	if err != nil {
		return s.failure(ctx, err)
	}
	return domain.JobOutcome{Result: raw}
}

func (s *executionScope) resetRejectedWrite(ctx context.Context, err error) error {
	var problem *domain.DataError
	if s.work.StartedWrite && errors.As(err, &problem) && problem.KnownUnbilled {
		if e := s.repo.ClearExecutionWrite(ctx, s.lease); e != nil {
			return e
		}
		s.work.StartedWrite = false
	}
	return err
}

func (s executionScope) failure(_ context.Context, err error) domain.JobOutcome {
	if s.work.StartedWrite && s.work.Input.Kind == domain.ExecMediaUpload {
		return domain.JobOutcome{Code: "cms_media_receipt_unknown"}
	}
	return executionFailure(err)
}

func executionFailure(err error) domain.JobOutcome {
	var data *domain.DataError
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &data):
		if data.Code == "execution_budget" {
			return domain.JobOutcome{Code: data.Code, DeferUntil: time.Now().UTC().Add(max(data.RetryAfter, time.Second))}
		}
		out := domain.JobOutcome{Code: data.Code, Retry: data.Retry}
		if data.RetryAfter > 0 {
			out.RetryAfter = time.Now().UTC().Add(min(data.RetryAfter, 24*time.Hour))
		}
		return out
	case errors.Is(err, domain.ErrExecutionAuthority), errors.Is(err, domain.ErrCrawlUnauthorized):
		return domain.JobOutcome{Code: "execution_unauthorized"}
	case errors.Is(err, domain.ErrExecutionUnsupported):
		return domain.JobOutcome{Code: "execution_unsupported"}
	case errors.Is(err, domain.ErrExecutionState), errors.Is(err, domain.ErrVersionConflict):
		return domain.JobOutcome{Code: "execution_state_changed"}
	case errors.Is(err, domain.ErrContentGate):
		return domain.JobOutcome{Code: "content_gate_blocked"}
	case errors.As(err, &validation):
		return domain.JobOutcome{Code: "execution_invalid"}
	default:
		return researchFailure(err)
	}
}

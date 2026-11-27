package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
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
	ReserveExecutionOperation(context.Context, string, string, string, int64, *domain.Lease, string) (int64, error)
	executionsearch.AccountLedger
	RefreshIntegration(context.Context, string, string, int64, bool, func(context.Context, domain.IntegrationAccess) (domain.ExecutionCredential, []string, error)) (domain.IntegrationAccess, error)
	RecordIndexNowVerification(context.Context, domain.Lease, domain.ExecutionConnection, string) error
	SettleExecutionRequest(context.Context, string, string, int64, domain.ProviderReceipt) error
	BeginExecutionSubmissions(context.Context, domain.Lease, string, []string) error
	ExecutionSubmissionsFailure(context.Context, domain.Lease, string, []string, int, string, bool) error
	SaveExecutionTokens(context.Context, domain.ExecutionConnection, domain.ExecutionCredential) error
	ExecutionSyncDates(context.Context, domain.ExecutionConnection, domain.ExecutionJobInput) (string, string, error)
	StartGSCDays(context.Context, domain.Lease, domain.ExecutionConnection, string, string) error
	NextGSCDay(context.Context, string, string) (*domain.GSCCursor, error)
	GSCProgress(context.Context, string, string) (domain.GSCProgress, error)
	SaveGSCPage(context.Context, domain.Lease, domain.ExecutionWork, domain.GSCCursor, domain.GSCReportPage) error
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
	fixture    *http.Client
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
	s := executionScope{repo: runtime, site: site, lease: lease, work: work, fixture: services.Fixture}
	timeout := 12 * time.Second
	if id := work.Input.ConnectionID; id != "" {
		s.connection, s.secret, err = runtime.ExecutionConnectionAccess(ctx, site.OrganizationID, site.ID, id)
		if err != nil {
			return s.failure(ctx, err)
		}
		if !s.connection.MatchesIntegration(work.IntegrationAccountID, work.IntegrationVersion) {
			return s.failure(ctx, domain.ErrIntegrationRevoked)
		}
		if s.connection.Version != work.ConnectionVersion {
			return s.failure(ctx, domain.ErrVersionConflict)
		}
		timeout = time.Duration(s.connection.TimeoutSeconds) * time.Second
	}
	s.client = executionhttp.Client(services.Fixture, timeout, executionhttp.Ledger{
		Provider: s.connection.Kind,
		Secrets:  []string{s.secret.Secret, s.secret.ClientSecret, s.secret.RefreshToken, s.secret.AccessToken},
		ReserveOperation: func(ctx context.Context, operation string) (int64, error) {
			return runtime.ReserveExecutionOperation(ctx, site.OrganizationID, site.ID, work.Input.ConnectionID, work.ConnectionVersion, &lease, operation)
		},
		Settle: func(ctx context.Context, id int64, receipt domain.ProviderReceipt) error {
			return runtime.SettleExecutionRequest(ctx, site.OrganizationID, site.ID, id, receipt)
		},
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
		if in.Kind == domain.ExecGSCSync && c.Property != "" {
			return s.syncGSCDays(ctx)
		}
		if in.Kind == domain.ExecConnectionTest && c.Kind == domain.ExecutionIndexNow {
			err = s.checkIndexNowKey(ctx)
			if err == nil {
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
	if c := s.connection; c.IntegrationAccountID != "" {
		access, err := s.repo.RefreshIntegration(ctx, s.site.OrganizationID, c.IntegrationAccountID, c.IntegrationVersion, false,
			func(ctx context.Context, access domain.IntegrationAccess) (domain.ExecutionCredential, []string, error) {
				return executionsearch.AccountClient(s.fixture, s.repo, s.site.OrganizationID, access, false).RefreshIntegrationToken(ctx, access)
			})
		return access.Credential, err
	}
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
	if s.connection.Kind == domain.ExecutionGSC {
		limit = min(32, s.connection.RequestsPerMinute, s.connection.RequestsPerDay)
	}
	if !s.connection.Managed && (s.connection.Kind == domain.ExecutionGSC || s.connection.Kind == domain.ExecutionGA4) &&
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
	targets := in.URLs
	if len(targets) == 0 {
		targets = []string{in.URL}
	}
	for _, target := range targets {
		u, err := domain.EvidenceURL(target)
		if err != nil || !domain.BelongsToSite(s.site.BaseURL, u) {
			return s.failure(ctx, domain.ErrExecutionAuthority)
		}
	}
	client := executionsearch.New(s.client)
	secret, err := s.searchCredential(ctx, client)
	if err != nil {
		return s.failure(ctx, err)
	}
	if err = client.ValidateSubmission(c, secret, targets); err != nil {
		return s.failure(ctx, err)
	}
	if c.Kind == domain.ExecutionIndexNow {
		if err = s.checkIndexNowKey(ctx); err != nil {
			return s.failure(ctx, err)
		}
	}
	if err = s.repo.BeginExecutionSubmissions(ctx, s.lease, c.Kind, targets); err != nil {
		return s.failure(ctx, err)
	}
	status, err := client.SubmitStatus(ctx, c, secret, targets)
	if err != nil {
		problem := executionFailure(err)
		var upstream *domain.DataError
		knownUnsent := errors.As(err, &upstream) && upstream.KnownUnbilled
		unknown := !knownUnsent && status == 0 || status == http.StatusRequestTimeout || status >= 500 || status >= 200 && status < 300
		if unknown {
			problem = domain.JobOutcome{Code: "execution_indexing_receipt_unknown"}
		}
		settlement, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.repo.ExecutionSubmissionsFailure(settlement, s.lease, c.Kind, targets, status, problem.Code, unknown); err != nil {
			return domain.JobOutcome{Code: "execution_indexing_receipt_unknown"}
		}
		return problem
	}
	code, label := "accepted", "SUBMITTED"
	if status == http.StatusAccepted {
		code, label = "pending_key_validation", "ACCEPTED_PENDING_VERIFICATION"
	}
	now := time.Now().UTC()
	out := store.ExecutionResult{Submission: map[string]any{"url": targets[0], "urls": targets, "host": s.siteHost(),
		"status": code, "integration_status": label, "http_status": status, "responded_at": now}}
	if c.Kind == domain.ExecutionIndexNow && c.Managed {
		out.Submission["key_id"] = c.ID
	}
	for _, url := range targets {
		out.Indexes = append(out.Indexes, domain.ExecutionIndexRecord{URL: url, Status: domain.IndexSubmitted, Provider: c.Kind, Evidence: code, ObservedAt: now})
	}
	return s.save(ctx, out)
}

func (s executionScope) siteHost() string {
	base, _ := domain.EvidenceURL(s.site.BaseURL)
	if base == nil {
		return ""
	}
	return base.Hostname()
}

func (s executionScope) checkIndexNowKey(ctx context.Context) error {
	err := s.verifyIndexNowKey(ctx)
	if !s.connection.Managed {
		return err
	}
	code := ""
	if err != nil {
		code = executionFailure(err).Code
	}
	if recordErr := s.repo.RecordIndexNowVerification(ctx, s.lease, s.connection, code); recordErr != nil {
		return recordErr
	}
	return err
}

func (s executionScope) verifyIndexNowKey(ctx context.Context) error {
	location := s.connection.KeyLocation
	client := s.client
	if s.connection.Managed {
		location = s.connection.IndexNowVerificationURL
		isolated := *client
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &isolated
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil || !domain.ValidIndexNowKey(s.secret.Secret) || path.Base(req.URL.Path) != s.secret.Secret+".txt" {
		return &domain.DataError{Code: "indexnow_ownership_unverified"}
	}
	response, err := client.Do(req)
	if err != nil {
		var providerError *domain.DataError
		if errors.As(err, &providerError) || ctx.Err() != nil {
			return err
		}
		return &domain.DataError{Code: "indexnow_ownership_unavailable", Retry: true}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return &domain.DataError{Code: "indexnow_ownership_unavailable", Retry: true}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1025))
	if readErr != nil {
		return &domain.DataError{Code: "indexnow_ownership_unavailable", Retry: true}
	}
	actual := string(body)
	if !s.connection.Managed || s.connection.LegacyKey {
		actual = strings.TrimSpace(actual)
	}
	if response.StatusCode != http.StatusOK || len(body) > 1024 || actual != s.secret.Secret {
		return &domain.DataError{Code: "indexnow_ownership_unverified"}
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
	case errors.Is(err, domain.ErrIntegrationRevoked):
		return domain.JobOutcome{Code: "integration_authorization_changed"}
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

// Package worker dispatches persisted SEO jobs to narrowly scoped specialists.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type repository interface {
	ClaimJob(context.Context) (*domain.Lease, error)
	GetSite(context.Context, string, string) (domain.Site, error)
	ReserveProviderCall(context.Context, domain.Lease) (domain.ProviderCall, error)
	FinishJob(context.Context, domain.Lease, domain.JobOutcome) error
}

type briefing interface {
	generate(context.Context, domain.Site, domain.ProviderCall) domain.JobOutcome
}

// Run polls PostgreSQL as the master dispatcher. Lease recovery also works
// when multiple service processes are running against the same database.
func Run(ctx context.Context, repo repository, provider domain.SEODataProvider) {
	run(ctx, repo, openAI{endpoint: "https://api.openai.com/v1/responses"}, provider)
}

func run(ctx context.Context, repo repository, briefs briefing, provider domain.SEODataProvider) {
	ctx = domain.WithPrincipal(ctx, domain.Principal{Kind: "worker"})
	for ctx.Err() == nil {
		found, err := runOnce(ctx, repo, briefs, provider)
		if err == nil && found {
			continue
		}
		if err != nil && ctx.Err() == nil {
			slog.Warn("SEO worker will retry after a storage error")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func runOnce(ctx context.Context, repo repository, briefs briefing, provider domain.SEODataProvider) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	lease, err := repo.ClaimJob(claimCtx)
	cancel()
	if err != nil || lease == nil {
		return false, err
	}

	timeout := 40 * time.Second
	if domain.IsResearchKind(lease.Job.Kind) {
		timeout = 70 * time.Second
	}
	workCtx, cancel := context.WithTimeout(ctx, timeout)
	outcome := dispatch(workCtx, repo, *lease, briefs, provider)
	cancel()

	finishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = repo.FinishJob(finishCtx, *lease, outcome)
	if errors.Is(err, domain.ErrLeaseLost) {
		return true, nil
	}
	return true, err
}

func dispatch(ctx context.Context, repo repository, lease domain.Lease, briefs briefing, provider domain.SEODataProvider) domain.JobOutcome {
	site, err := repo.GetSite(ctx, lease.Job.OrganizationID, lease.Job.SiteID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.JobOutcome{Code: "site_missing"}
		}
		return domain.JobOutcome{Code: "site_read_unavailable", Retry: true}
	}
	if site.Status != "active" {
		return domain.JobOutcome{Code: "site_inactive"}
	}
	if domain.IsResearchKind(lease.Job.Kind) {
		return researchJob(ctx, repo, provider, briefs, lease, site)
	}
	switch lease.Job.Kind {
	case domain.JobSiteInventory:
		return encodedResult(site)
	case domain.JobSiteBrief:
		call, err := repo.ReserveProviderCall(ctx, lease)
		switch {
		case errors.Is(err, domain.ErrProviderDisabled), errors.Is(err, domain.ErrNotFound):
			return domain.JobOutcome{Code: "provider_disabled"}
		case errors.Is(err, domain.ErrProviderNotConfigured):
			return domain.JobOutcome{Code: "provider_credentials_unavailable"}
		case err != nil:
			return domain.JobOutcome{Code: "provider_reservation_unavailable", Retry: true}
		case !call.DeferUntil.IsZero():
			return domain.JobOutcome{Code: "provider_budget_wait", DeferUntil: call.DeferUntil}
		default:
			return briefs.generate(ctx, site, call)
		}
	default:
		return domain.JobOutcome{Code: "unknown_job_kind"}
	}
}

func encodedResult(value any) domain.JobOutcome {
	payload, err := json.Marshal(value)
	if err != nil {
		return domain.JobOutcome{Code: "result_encoding_failed"}
	}
	return domain.JobOutcome{Result: payload}
}

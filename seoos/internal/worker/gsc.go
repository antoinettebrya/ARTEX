package worker

import (
	"context"
	"errors"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/executionsearch"
	"github.com/Autumn-27/artex/seoos/internal/store"
)

func (s executionScope) syncGSCDays(ctx context.Context) domain.JobOutcome {
	org, job := s.site.OrganizationID, s.lease.Job.ID
	progress, err := s.repo.GSCProgress(ctx, org, job)
	if err != nil {
		return s.failure(ctx, err)
	}
	if progress.From == "" {
		from, to, err := s.repo.ExecutionSyncDates(ctx, s.connection, s.work.Input)
		if err != nil {
			return s.failure(ctx, err)
		}
		if err := s.repo.StartGSCDays(ctx, s.lease, s.connection, from, to); err != nil {
			return s.failure(ctx, err)
		}
	}
	client := executionsearch.New(s.client)
	secret, err := s.searchCredential(ctx, client)
	if err != nil {
		return s.failure(ctx, err)
	}
	s.secret = secret

	for requests := 0; requests < 24; requests++ {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 3*time.Second {
			break
		}
		cursor, err := s.repo.NextGSCDay(ctx, org, job)
		if err != nil {
			return s.failure(ctx, err)
		}
		if cursor == nil {
			return s.finishGSCSync(ctx)
		}
		rows, err := client.GSCPage(ctx, s.connection, secret, cursor.Day, cursor.NextRow, !cursor.TotalsChecked)
		if err != nil {
			// Expiry of this worker slice does not consume a remote retry.
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				break
			}
			return s.failure(ctx, err)
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
		err = s.repo.SaveGSCPage(saveCtx, s.lease, s.work, *cursor, rows)
		cancel()
		if err != nil {
			return s.failure(ctx, err)
		}
	}
	return domain.JobOutcome{Code: "execution_gsc_resuming", DeferUntil: time.Now().UTC().Add(time.Second)}
}

func (s executionScope) finishGSCSync(ctx context.Context) domain.JobOutcome {
	progress, err := s.repo.GSCProgress(ctx, s.site.OrganizationID, s.lease.Job.ID)
	if err != nil {
		return s.failure(ctx, err)
	}
	// This refreshes the property and sitemap directories. The date windows
	// are already stored, so a retry here never replays a page of analytics.
	directories, err := s.search(ctx, true)
	if err != nil {
		return s.failure(ctx, err)
	}
	return s.save(ctx, store.ExecutionResult{GSC: &progress, Sync: directories})
}

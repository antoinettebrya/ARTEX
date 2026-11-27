package worker

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	htmlparser "golang.org/x/net/html"
)

func (s executionScope) publication(ctx context.Context, c domain.CMSConnector) domain.JobOutcome {
	r, err := s.repo.ExecutionRelease(ctx, s.lease, s.work)
	if err != nil {
		return s.failure(ctx, err)
	}
	if r.SiteID != s.site.ID || r.ConnectionID != s.connection.ID {
		return s.failure(ctx, domain.ErrExecutionAuthority)
	}
	if s.work.Input.Kind == domain.ExecVerify {
		return s.verifyPublication(ctx, c, r)
	}
	if !s.connection.AllowWrites {
		return s.publicationProblem(ctx, domain.ErrExecutionAuthority)
	}
	if s.work.Input.Operation != "rollback" && !s.work.StartedWrite {
		if err := s.repo.CheckExecutionPackage(ctx, s.site.OrganizationID, s.site.ID, r.PackageID); err != nil {
			return s.publicationProblem(ctx, err)
		}
	}
	if err := s.repo.BeginPublication(ctx, s.lease, s.work, domain.PublicationPreparing); err != nil {
		return s.failure(ctx, err)
	}
	switch s.work.Input.Operation {
	case "draft":
		return s.createDraft(ctx, c, r)
	case "publish":
		return s.publish(ctx, c, r)
	case "rollback":
		return s.rollback(ctx, c, r)
	default:
		return s.publicationProblem(ctx, domain.ErrExecutionUnsupported)
	}
}

func releaseInput(release domain.ExecutionRelease, shadow bool) domain.CMSPageInput {
	a := release.Article
	slug := strings.Trim(a.Slug, "/- ")
	if u, err := url.Parse(a.Canonical); err == nil && u.Path != "" && u.Path != "/" {
		slug = path.Base(strings.TrimSuffix(u.Path, "/"))
	}
	if slug == "" || strings.ContainsAny(slug, "/\\?# 	\n") || len(slug) > 170 {
		slug = "article-" + release.ID[:8]
	}
	if shadow {
		slug += "-artex-" + release.ID[:8]
	}
	var out strings.Builder
	// The stable marker identifies only this release in authenticated draft
	// reads. All authored content is emitted as escaped text.
	out.WriteString(releaseMarker(release.ID))
	for _, section := range a.Sections {
		level := min(max(section.Level, 2), 6)
		_, _ = fmt.Fprintf(&out, "<h%d>%s</h%d>\n", level, html.EscapeString(section.Heading), level)
		for _, p := range strings.Split(section.Body, "\n\n") {
			if strings.TrimSpace(p) != "" {
				out.WriteString("<p>" + html.EscapeString(strings.TrimSpace(p)) + "</p>\n")
			}
		}
	}
	return domain.CMSPageInput{Resource: release.Resource, Slug: slug, Title: a.Title, Body: out.String(), Excerpt: a.Description,
		SEOTitle: a.Title, Description: a.Description, Canonical: a.Canonical, Status: "draft"}
}

func releaseMarker(id string) string { return "<!-- artex-publication:" + id + " -->\n" }

func (s executionScope) createDraft(ctx context.Context, c domain.CMSConnector, r domain.ExecutionRelease) domain.JobOutcome {
	payload := releaseInput(r, r.RemoteID != "")
	found, err := c.ListPages(ctx, r.Resource, payload.Slug, 2)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	for _, p := range found {
		if p.Slug != payload.Slug {
			continue
		}
		if hasReleaseMarker(p, r.ID) && p.Status == "draft" && sameReleaseContent(p, payload) {
			return s.completePublication(ctx, domain.PublicationDraft, "", &p, nil)
		}
		if s.work.StartedWrite {
			return s.completePublication(ctx, domain.PublicationRollback, "cms_draft_receipt_conflict", &p, nil)
		}
		return s.publicationProblem(ctx, &domain.DataError{Code: "cms_slug_collision", KnownUnbilled: true})
	}
	if s.work.StartedWrite {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_draft_receipt_unknown", nil, nil)
	}
	var before *domain.CMSPage
	if r.RemoteID != "" {
		old, err := c.GetPage(ctx, r.Resource, r.RemoteID)
		if err != nil {
			return s.publicationProblem(ctx, err)
		}
		before = &old
	}
	if err = s.repo.MarkExecutionWrite(ctx, s.lease, s.work, before); err != nil {
		return s.publicationProblem(ctx, err)
	}
	s.work.StartedWrite = true
	page, err := c.CreatePage(ctx, payload)
	if err != nil {
		return s.publicationProblem(ctx, s.resetRejectedWrite(ctx, err))
	}
	if page.ID == "" || page.Status != "draft" || page.Slug != payload.Slug || !hasReleaseMarker(page, r.ID) || !sameReleaseContent(page, payload) {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_draft_readback_mismatch", &page, nil)
	}
	return s.completePublication(ctx, domain.PublicationDraft, "", &page, nil)
}

func (s executionScope) publish(ctx context.Context, c domain.CMSConnector, r domain.ExecutionRelease) domain.JobOutcome {
	if r.DraftID == "" || r.TargetID == "" {
		return s.publicationProblem(ctx, domain.ErrExecutionState)
	}
	payload := releaseInput(r, false)
	payload.Status = "publish"
	status := domain.PublicationPublished
	if at := s.work.Input.ScheduledAt; at != nil {
		if time.Until(*at) <= 0 && !s.work.StartedWrite {
			return s.publicationProblem(ctx, &domain.DataError{Code: "cms_schedule_elapsed"})
		}
		payload.ScheduledAt = at
		payload.Status = "future"
		status = domain.PublicationScheduled
	}
	current, err := c.GetPage(ctx, r.Resource, r.TargetID)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	if s.work.StartedWrite {
		if hasReleaseMarker(current, r.ID) && (matchesRelease(current, payload) ||
			payload.Status == "future" && current.Status == "publish" && sameReleaseContent(current, payload)) {
			if current.Status == "publish" {
				status = domain.PublicationPublished
			}
			return s.completePublication(ctx, status, "", &current, nil)
		}
		return s.completePublication(ctx, domain.PublicationRollback, "cms_publish_receipt_unknown", nil, nil)
	}
	if r.RemoteID != "" {
		if r.Original == nil || !sameCMSRevision(current, *r.Original) {
			return s.publicationProblem(ctx, &domain.DataError{Code: "cms_remote_changed_since_draft"})
		}
	} else if current.Status != "draft" || !sameReleaseContent(current, payload) || !hasReleaseMarker(current, r.ID) {
		return s.publicationProblem(ctx, &domain.DataError{Code: "cms_draft_changed"})
	}
	draft, err := c.GetPage(ctx, r.Resource, r.DraftID)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	if draft.Status != "draft" || !sameReleaseContent(draft, payload) || !hasReleaseMarker(draft, r.ID) {
		return s.publicationProblem(ctx, &domain.DataError{Code: "cms_draft_changed"})
	}
	if err = s.repo.MarkExecutionWrite(ctx, s.lease, s.work, &current); err != nil {
		return s.publicationProblem(ctx, err)
	}
	s.work.StartedWrite = true
	after, err := c.PublishPage(ctx, r.TargetID, payload)
	if err != nil {
		return s.publicationProblem(ctx, s.resetRejectedWrite(ctx, err))
	}
	if after.ID != r.TargetID || !matchesRelease(after, payload) || !hasReleaseMarker(after, r.ID) {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_publish_readback_mismatch", &after, nil)
	}
	return s.completePublication(ctx, status, "", &after, nil)
}

func (s executionScope) rollback(ctx context.Context, c domain.CMSConnector, r domain.ExecutionRelease) domain.JobOutcome {
	if r.Before == nil || r.TargetID == "" || r.Before.ID != r.TargetID {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_rollback_snapshot_missing", nil, nil)
	}
	want := r.Before
	payload := domain.CMSPageInput{Resource: r.Resource, Slug: want.Slug, Title: want.Title, Body: want.Body, Excerpt: want.Excerpt,
		SEOTitle: want.SEOTitle, Description: want.Description, Canonical: want.Canonical, Status: want.Status, ScheduledAt: want.ScheduledAt}
	current, err := c.GetPage(ctx, r.Resource, r.TargetID)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	if sameCMSRevision(current, *want) {
		return s.completePublication(ctx, domain.PublicationRolledBack, "", &current, nil)
	}
	if s.work.StartedWrite {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_rollback_receipt_unknown", nil, nil)
	}
	if r.After != nil && r.After.ID == current.ID {
		applied := *r.After
		if applied.Status == "future" && current.Status == "publish" {
			applied.Status, applied.ScheduledAt = current.Status, current.ScheduledAt
		}
		if !sameCMSRevision(current, applied) {
			return s.completePublication(ctx, domain.PublicationRollback, "cms_remote_changed_since_publish", &current, nil)
		}
	} else if !hasReleaseMarker(current, r.ID) {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_rollback_target_unconfirmed", &current, nil)
	}
	if err = s.repo.MarkExecutionWrite(ctx, s.lease, s.work, &current); err != nil {
		return s.publicationProblem(ctx, err)
	}
	s.work.StartedWrite = true
	if _, err = c.UpdatePage(ctx, r.TargetID, payload); err != nil {
		return s.publicationProblem(ctx, s.resetRejectedWrite(ctx, err))
	}
	check, err := c.GetPage(ctx, r.Resource, r.TargetID)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	if !sameCMSRevision(check, *want) {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_rollback_readback_mismatch", &check, nil)
	}
	return s.completePublication(ctx, domain.PublicationRolledBack, "", &check, nil)
}

func (s executionScope) verifyPublication(ctx context.Context, c domain.CMSConnector, r domain.ExecutionRelease) domain.JobOutcome {
	if r.TargetID == "" {
		return s.publicationProblem(ctx, domain.ErrExecutionState)
	}
	if err := s.repo.BeginPublication(ctx, s.lease, s.work, domain.PublicationVerifying); err != nil {
		return s.failure(ctx, err)
	}
	have, err := c.GetPage(ctx, r.Resource, r.TargetID)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	if have.Status == "future" && r.ScheduledAt != nil && time.Since(*r.ScheduledAt) < 48*time.Hour {
		next := time.Now().UTC().Add(time.Minute)
		if r.ScheduledAt.After(next) {
			next = *r.ScheduledAt
		}
		if e := s.repo.PublicationRetry(ctx, s.lease, s.work, domain.PublicationScheduled, "cms_schedule_pending"); e != nil {
			return s.failure(ctx, e)
		}
		return domain.JobOutcome{Code: "cms_schedule_pending", DeferUntil: next}
	}
	want := releaseInput(r, false)
	want.Status = "publish"
	if !matchesRelease(have, want) || !hasReleaseMarker(have, r.ID) || have.URL == "" {
		return s.completePublication(ctx, domain.PublicationRollback, "cms_rest_content_mismatch", &have, nil)
	}
	public, err := c.ObservePublic(ctx, have.URL)
	if err != nil {
		return s.publicationProblem(ctx, err)
	}
	code := publicMismatch(public, r, s.connection.Capabilities)
	if code != "" {
		return s.completePublication(ctx, domain.PublicationRollback, code, &have, &public)
	}
	return s.completePublication(ctx, domain.PublicationVerified, "", &have, &public)
}

func publicMismatch(have domain.CMSPage, r domain.ExecutionRelease, c domain.CMSCapabilities) string {
	a := r.Article
	if have.HTTPStatus != 200 {
		return "cms_public_http_status"
	}
	if normalizedText(have.Title) != normalizedText(a.Title) && normalizedText(have.Title) != normalizedText(a.H1) {
		return "cms_public_title_mismatch"
	}
	body := normalizedText(have.Body)
	for _, section := range a.Sections {
		if !strings.Contains(body, normalizedText(section.Body)) {
			return "cms_public_body_mismatch"
		}
	}
	if a.Canonical != "" && strings.TrimSuffix(have.Canonical, "/") != strings.TrimSuffix(a.Canonical, "/") {
		return "cms_public_canonical_mismatch"
	}
	var title, description bool
	for _, field := range c.SEOFields {
		field = strings.ToLower(field)
		title = title || strings.Contains(field, "title")
		description = description || strings.Contains(field, "description") || strings.Contains(field, "metadesc")
	}
	if (title || have.SEOTitle != "") && !strings.Contains(normalizedText(have.SEOTitle), normalizedText(a.Title)) {
		return "cms_public_seo_title_mismatch"
	}
	if (description || c.SEOPlugin != "" && have.Description != "") && a.Description != "" && normalizedText(have.Description) != normalizedText(a.Description) {
		return "cms_public_description_mismatch"
	}
	return ""
}

func normalizedText(s string) string {
	t := htmlparser.NewTokenizer(strings.NewReader(s))
	var text strings.Builder
	for {
		switch t.Next() {
		case htmlparser.TextToken:
			text.Write(t.Text())
		case htmlparser.StartTagToken, htmlparser.EndTagToken:
			text.WriteByte(' ')
		case htmlparser.ErrorToken:
			return domain.KeywordKey(text.String())
		}
	}
}

func sameReleaseContent(have domain.CMSPage, want domain.CMSPageInput) bool {
	return normalizedText(have.Title) == normalizedText(want.Title) && normalizedText(have.Body) == normalizedText(want.Body)
}
func matchesRelease(have domain.CMSPage, want domain.CMSPageInput) bool {
	if want.Status == "future" && (want.ScheduledAt == nil || have.ScheduledAt == nil ||
		!have.ScheduledAt.UTC().Truncate(time.Second).Equal(want.ScheduledAt.UTC().Truncate(time.Second))) {
		return false
	}
	return have.Status == want.Status && have.ID != "" && sameReleaseContent(have, want)
}

func hasReleaseMarker(page domain.CMSPage, id string) bool {
	return strings.Contains(page.Body, strings.TrimSpace(releaseMarker(id)))
}

func sameCMSRevision(a, b domain.CMSPage) bool {
	if a.Status == "future" && (a.ScheduledAt == nil || b.ScheduledAt == nil || !a.ScheduledAt.Equal(*b.ScheduledAt)) {
		return false
	}
	return a.ID == b.ID && a.Status == b.Status && a.Slug == b.Slug && a.Title == b.Title &&
		a.Body == b.Body && a.Excerpt == b.Excerpt &&
		a.Description == b.Description && a.SEOTitle == b.SEOTitle && a.Canonical == b.Canonical
}

func (s executionScope) completePublication(ctx context.Context, status, reason string, after, public *domain.CMSPage) domain.JobOutcome {
	value, err := s.repo.CompletePublication(ctx, s.lease, s.work, status, reason, after, public)
	if err != nil {
		return executionFailure(err)
	}
	if reason != "" {
		return domain.JobOutcome{Code: reason}
	}
	return domain.JobOutcome{Result: value}
}

func (s executionScope) publicationProblem(ctx context.Context, err error) domain.JobOutcome {
	out := s.failure(ctx, err)
	if out.Code == "lease_lost" || ctx.Err() != nil {
		return out
	}
	if !out.DeferUntil.IsZero() || out.Retry && s.lease.Job.Attempts < domain.MaxJobAttempts {
		if err := s.repo.PublicationRetry(ctx, s.lease, s.work, domain.PublicationRetry, out.Code); err != nil {
			return executionFailure(err)
		}
		return out
	}
	status := domain.PublicationFailed
	switch {
	case s.work.Input.Kind == domain.ExecVerify || s.work.StartedWrite:
		status = domain.PublicationRollback
	case s.work.Input.Operation == "publish" && s.work.PriorStatus == domain.PublicationDraft:
		// A failed preflight or definitively rejected write keeps the existing
		// draft usable. A new attempt still requires fresh owner authorization.
		status = domain.PublicationDraft
	case s.work.Input.Operation == "rollback":
		status = domain.PublicationRollback
	}
	return s.completePublication(ctx, status, out.Code, nil, nil)
}

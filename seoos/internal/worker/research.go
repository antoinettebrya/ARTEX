package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchengine"
)

type researchRuntime interface {
	repository
	ResearchContext(context.Context, domain.Lease) (domain.ResearchContext, error)
	ReserveData(context.Context, domain.Lease, domain.ResearchContext, domain.DataRequest, domain.DataPlan) (domain.DataReservation, error)
	SettleData(context.Context, domain.Lease, string, domain.DataResponse, domain.DataPlan, *domain.DataError) error
	PersistData(context.Context, domain.Lease, domain.ResearchContext, domain.DataOperation, string, domain.DataResponse, []string) error
	ResearchEvidence(context.Context, domain.Job, domain.ResearchContext) ([]domain.KeywordEvidence, error)
	ResearchOwnedPages(context.Context, domain.Job, string) ([]domain.PageEvidence, error)
	ExistingMapping(context.Context, domain.Job, domain.ResearchContext) (*domain.URLMapping, error)
	PersistResearchAnalysis(context.Context, domain.Lease, domain.ResearchContext, domain.ResearchAnalysis) error
}

type intentAssistant interface {
	intent(context.Context, domain.KeywordEvidence, domain.Market, domain.ProviderCall) (*domain.SearchIntent, domain.JobOutcome)
}

func researchJob(ctx context.Context, repo repository, provider domain.SEODataProvider, briefs briefing, lease domain.Lease, site domain.Site) domain.JobOutcome {
	store, ok := repo.(researchRuntime)
	if !ok {
		return domain.JobOutcome{Code: "research_unavailable"}
	}
	scope, err := store.ResearchContext(ctx, lease)
	if err != nil {
		return researchFailure(err)
	}
	if domain.UsesDataProvider(lease.Job.Kind) {
		if provider == nil {
			return domain.JobOutcome{Code: "data_provider_unavailable"}
		}
		return collectResearch(ctx, store, provider, lease, site, scope)
	}
	return analyzeResearch(ctx, store, briefs, lease, site, scope)
}

func operations(kind string, market domain.Market) []domain.DataOperation {
	bingWithoutLabs := market.SearchEngine == "bing" &&
		(market.CountryCode != "US" || market.LocationCode != 2840 || !strings.EqualFold(market.LanguageCode, "en"))
	switch kind {
	case domain.KeywordDiscovery:
		if bingWithoutLabs {
			return []domain.DataOperation{domain.DataSuggestions}
		}
		return []domain.DataOperation{domain.DataRelated, domain.DataSuggestions}
	case domain.KeywordMetrics:
		if bingWithoutLabs {
			return []domain.DataOperation{domain.DataVolume}
		}
		return []domain.DataOperation{domain.DataOverview, domain.DataVolume}
	case domain.KeywordSuggestions:
		return []domain.DataOperation{domain.DataSuggestions}
	case domain.SERPAnalysis:
		return []domain.DataOperation{domain.DataSERP}
	default:
		return nil
	}
}

func collectResearch(ctx context.Context, repo researchRuntime, provider domain.SEODataProvider, lease domain.Lease, site domain.Site, scope domain.ResearchContext) domain.JobOutcome {
	in := scope.Request
	if in.Kind == domain.CompetitorAnalysis {
		return collectCompetitor(ctx, repo, provider, lease, site, scope)
	}
	calls := operations(in.Kind, in.Market)
	ids := make([]string, 0, len(calls))
	cacheHits := 0
	for _, operation := range calls {
		request := domain.DataRequest{Operation: operation, Market: in.Market, Keyword: in.Keyword, TargetURL: in.TargetURL}
		result, reservation, outcome := obtainData(ctx, repo, provider, lease, scope, request)
		if outcome.Code != "" {
			return outcome
		}
		if reservation.Cached != nil {
			cacheHits++
		}
		if err := repo.PersistData(ctx, lease, scope, operation, reservation.ID, result, nil); err != nil {
			return researchFailure(err)
		}
		ids = append(ids, reservation.ID)
	}
	return encodedResult(map[string]any{"snapshots": ids, "cache_hits": cacheHits, "market": in.Market})
}

func obtainData(ctx context.Context, repo researchRuntime, provider domain.SEODataProvider, lease domain.Lease, scope domain.ResearchContext, request domain.DataRequest) (domain.DataResponse, domain.DataReservation, domain.JobOutcome) {
	plan, err := provider.Plan(request)
	if err != nil {
		return domain.DataResponse{}, domain.DataReservation{}, researchFailure(err)
	}
	reservation, err := repo.ReserveData(ctx, lease, scope, request, plan)
	if err != nil {
		return domain.DataResponse{}, reservation, researchFailure(err)
	}
	if !reservation.DeferUntil.IsZero() {
		return domain.DataResponse{}, reservation, domain.JobOutcome{Code: "research_budget_or_cache_wait", DeferUntil: reservation.DeferUntil}
	}
	if reservation.Cached != nil {
		return *reservation.Cached, reservation, domain.JobOutcome{}
	}
	request.RequestID = reservation.ID
	result, outcome := fetchAndSettle(ctx, repo, provider, lease, request, reservation, plan)
	return result, reservation, outcome
}

func collectCompetitor(ctx context.Context, repo researchRuntime, provider domain.SEODataProvider, lease domain.Lease, site domain.Site, scope domain.ResearchContext) domain.JobOutcome {
	in := scope.Request
	evidence, err := repo.ResearchEvidence(ctx, lease.Job, scope)
	if err != nil {
		return researchFailure(err)
	}
	if !observedCompetitor(evidence, site, in.TargetURL) {
		return domain.JobOutcome{Code: "competitor_not_in_serp"}
	}
	owned, err := repo.ResearchOwnedPages(ctx, lease.Job, scope.MarketID)
	if err != nil {
		return researchFailure(err)
	}
	request := domain.DataRequest{Operation: domain.DataPage, Market: in.Market, TargetURL: in.TargetURL}
	base, reservation, outcome := obtainData(ctx, repo, provider, lease, scope, request)
	if outcome.Code != "" {
		return outcome
	}
	if base.Page == nil {
		return domain.JobOutcome{Code: "provider_page_unavailable"}
	}
	page := *base.Page
	page.SourceSnapshots = []domain.PageSource{{Source: base.Source, SnapshotID: reservation.ID, CollectedAt: base.CollectedAt}}
	ids := []string{reservation.ID}
	cacheHits := 0
	if reservation.Cached != nil {
		cacheHits++
	}
	// Save the verified metadata before asking for supplemental sources. An
	// upstream timeout, cancellation, or budget wait leaves usable evidence.
	save := func() error {
		snapshot := page
		snapshot.PageType = researchengine.PageType(page)
		base.Page = &snapshot
		return repo.PersistData(ctx, lease, scope, domain.DataPage, reservation.ID, base, researchengine.ContentGaps(snapshot, owned))
	}
	if err := save(); err != nil {
		return researchFailure(err)
	}
	additional := []domain.DataOperation{domain.DataPageContent}
	if page.MicrodataObserved {
		additional = append(additional, domain.DataPageMicrodata)
	}
	var unavailable []string
	for _, operation := range additional {
		request.Operation = operation
		if operation == domain.DataPageMicrodata {
			request.TargetTaskID = base.TaskID
		}
		collected, extra, outcome := obtainData(ctx, repo, provider, lease, scope, request)
		if outcome.Code == "provider_page_unavailable" {
			unavailable = append(unavailable, string(operation))
			continue
		}
		if outcome.Code != "" {
			return outcome
		}
		if extra.Cached != nil {
			cacheHits++
		}
		ids = append(ids, extra.ID)
		page.SourceSnapshots = append(page.SourceSnapshots, domain.PageSource{Source: collected.Source, SnapshotID: extra.ID, CollectedAt: collected.CollectedAt})
		if supplement := collected.Page; supplement != nil {
			page.Topics = appendDistinct(page.Topics, supplement.Topics)
			page.Entities = appendDistinct(page.Entities, supplement.Entities)
			page.FAQ = appendDistinct(page.FAQ, supplement.FAQ)
			page.Schema = appendDistinct(page.Schema, supplement.Schema)
			page.Sections = append(page.Sections, supplement.Sections...)
		}
		if err := save(); err != nil {
			return researchFailure(err)
		}
	}
	return encodedResult(map[string]any{"snapshots": ids, "cache_hits": cacheHits, "market": in.Market, "unavailable_sources": unavailable})
}

func appendDistinct(values, additional []string) []string {
	seen := make(map[string]bool, len(values)+len(additional))
	for _, value := range values {
		seen[domain.KeywordKey(value)] = true
	}
	for _, value := range additional {
		key := domain.KeywordKey(value)
		if key != "" && !seen[key] {
			values = append(values, value)
			seen[key] = true
		}
	}
	return values
}

func fetchAndSettle(ctx context.Context, repo researchRuntime, provider domain.SEODataProvider, lease domain.Lease, request domain.DataRequest, reservation domain.DataReservation, plan domain.DataPlan) (domain.DataResponse, domain.JobOutcome) {
	result, fetchErr := provider.Fetch(ctx, request, reservation.Access)
	deviceScope := "all"
	if request.DeviceSpecific() {
		deviceScope = request.Market.Device
	}
	for i := range result.Keywords {
		result.Keywords[i].DeviceScope = deviceScope
	}
	for i := range result.Results {
		row := &result.Results[i]
		row.PageType = researchengine.PageType(domain.PageEvidence{URL: row.URL, Title: row.Title, PageType: row.PageType})
	}
	var failure *domain.DataError
	if fetchErr != nil && !errors.As(fetchErr, &failure) {
		failure = &domain.DataError{Code: "provider_transport"}
	}
	settlement, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := repo.SettleData(settlement, lease, reservation.ID, result, plan, failure); err != nil {
		// A missing receipt persists as a reservation and will be recovered by
		// the queue. It was potentially charged, so never immediately resend.
		return result, domain.JobOutcome{Code: "provider_receipt_unavailable"}
	}
	if failure != nil && result.CostMicros == nil && !failure.KnownUnbilled {
		return result, domain.JobOutcome{Code: "provider_receipt_unknown"}
	}
	if failure != nil {
		outcome := researchFailure(failure)
		if result.CostMicros != nil && *result.CostMicros > 0 {
			outcome.Retry = false
			outcome.RetryAfter = time.Time{}
		}
		return result, outcome
	}
	return result, domain.JobOutcome{}
}

func observedCompetitor(evidence []domain.KeywordEvidence, site domain.Site, target string) bool {
	u, err := domain.EvidenceURL(target)
	if err != nil || domain.BelongsToSite(site.BaseURL, u) {
		return false
	}
	for _, e := range evidence {
		for _, rank := range e.SERP {
			if rank.URL == target && rank.Rank >= 1 && rank.Rank <= 10 {
				return true
			}
		}
	}
	return false
}

func analyzeResearch(ctx context.Context, repo researchRuntime, briefs briefing, lease domain.Lease, site domain.Site, scope domain.ResearchContext) domain.JobOutcome {
	evidence, err := repo.ResearchEvidence(ctx, lease.Job, scope)
	if err != nil {
		return researchFailure(err)
	}
	if len(evidence) == 0 {
		return domain.JobOutcome{Code: "research_evidence_missing"}
	}
	var result domain.ResearchAnalysis
	switch lease.Job.Kind {
	case domain.KeywordIntent:
		var signal *domain.SearchIntent
		if scope.Request.IntentProviderID != "" {
			var outcome domain.JobOutcome
			signal, outcome = intentSignal(ctx, repo, briefs, lease, evidence[0], scope.Request.Market)
			if outcome.Code != "" {
				return outcome
			}
		}
		intent := researchengine.Classify(evidence[0], signal)
		result.Intent = &intent
	case domain.KeywordClustering:
		for i := range evidence {
			evidence[i] = withInferredIntent(evidence[i])
		}
		result.Clusters = researchengine.Cluster(evidence, site.BaseURL)
	case domain.KeywordURLMapping, domain.OpportunityScoring:
		owned, err := repo.ResearchOwnedPages(ctx, lease.Job, scope.MarketID)
		if err != nil {
			return researchFailure(err)
		}
		e := withInferredIntent(evidence[0])
		if lease.Job.Kind == domain.KeywordURLMapping {
			mapping := researchengine.Map(e, site.BaseURL, owned)
			previous, err := repo.ExistingMapping(ctx, lease.Job, scope)
			if err != nil {
				return researchFailure(err)
			}
			if previous != nil && previous.TargetURL != "" {
				mapping.PreviousTargetURL = previous.TargetURL
				mapping.Evidence = append(mapping.Evidence, "上一次研究保存的目标页为 "+previous.TargetURL+"。")
			}
			result.Mapping = &mapping
		} else {
			mapping, err := repo.ExistingMapping(ctx, lease.Job, scope)
			if err != nil {
				return researchFailure(err)
			}
			if mapping == nil {
				current := researchengine.Map(e, site.BaseURL, owned)
				mapping = &current
			}
			opportunity := researchengine.Score(e, site.BaseURL, owned, mapping, scope.Request.BusinessValue)
			opportunity.EvidenceMetrics = &e.Metric
			opportunity.MetricSources = e.MetricSources
			result.Opportunity = &opportunity
		}
	}
	if err := repo.PersistResearchAnalysis(ctx, lease, scope, result); err != nil {
		return researchFailure(err)
	}
	return encodedResult(map[string]any{"kind": lease.Job.Kind, "keyword": scope.Request.Keyword, "cluster_count": len(result.Clusters)})
}

func withInferredIntent(e domain.KeywordEvidence) domain.KeywordEvidence {
	if e.Intent == nil {
		intent := researchengine.Classify(e, nil)
		e.Intent = &intent
	}
	return e
}

func intentSignal(ctx context.Context, repo researchRuntime, briefs briefing, lease domain.Lease, e domain.KeywordEvidence, market domain.Market) (*domain.SearchIntent, domain.JobOutcome) {
	assistant, ok := briefs.(intentAssistant)
	if !ok {
		return nil, domain.JobOutcome{Code: "intent_provider_unavailable"}
	}
	request, err := intentModelRequest(e, market)
	if err != nil {
		return nil, domain.JobOutcome{Code: "intent_input_invalid"}
	}
	call, err := repo.ReserveProviderCall(ctx, lease, request)
	if err != nil {
		return nil, researchFailure(err)
	}
	if !call.DeferUntil.IsZero() {
		return nil, domain.JobOutcome{Code: "provider_budget_wait", DeferUntil: call.DeferUntil}
	}
	if call.Cached != nil {
		if call.Cached.Code != "" {
			return nil, *call.Cached
		}
		var saved domain.SearchIntent
		if err := json.Unmarshal(call.Cached.Result, &saved); err != nil {
			return nil, domain.JobOutcome{Code: "intent_provider_response_invalid"}
		}
		return &saved, *call.Cached
	}
	signal, outcome := assistant.intent(ctx, e, market, call)
	return signal, settleProvider(ctx, repo, lease, call, outcome)
}

func researchFailure(err error) domain.JobOutcome {
	var problem *domain.DataError
	switch {
	case errors.As(err, &problem):
		outcome := domain.JobOutcome{Code: problem.Code, Retry: problem.Retry}
		if problem.RetryAfter > 0 {
			outcome.RetryAfter = time.Now().UTC().Add(min(problem.RetryAfter, 12*time.Hour))
		}
		return outcome
	case errors.Is(err, domain.ErrProviderDisabled), errors.Is(err, domain.ErrNotFound):
		return domain.JobOutcome{Code: "research_resource_unavailable"}
	case errors.Is(err, domain.ErrProviderNotConfigured):
		return domain.JobOutcome{Code: "provider_credentials_unavailable"}
	case errors.Is(err, domain.ErrLeaseLost):
		return domain.JobOutcome{Code: "lease_lost"}
	default:
		return domain.JobOutcome{Code: "research_storage_unavailable", Retry: true}
	}
}

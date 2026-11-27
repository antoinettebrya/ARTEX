package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func jsonRows(ctx context.Context, db queryer, query string, args ...any) ([]json.RawMessage, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		items = append(items, json.RawMessage(raw))
	}
	return items, rows.Err()
}

const marketJSON = `jsonb_build_object('country_code',m.country_code,'location_code',m.location_code,
	'language_code',m.language_code,'search_engine',m.search_engine,'device',m.device)`

const linkedKeywords = ` FROM public.seo_research_site_keywords sk
	JOIN public.seo_keywords k ON k.organization_id=sk.organization_id AND k.id=sk.keyword_id
	JOIN public.seo_research_markets m ON m.organization_id=sk.organization_id AND m.id=sk.market_id `

const bestMetric = `LEFT JOIN LATERAL (
	WITH observations AS (
		SELECT e.metrics,e.source,e.snapshot_id,e.collected_at FROM public.seo_keyword_metrics e
		WHERE e.organization_id=sk.organization_id AND e.market_id=sk.market_id AND e.keyword_id=sk.keyword_id
	), fields AS (
		SELECT DISTINCT ON (value.key) value.key,value.value,e.source,e.snapshot_id,e.collected_at,
			e.metrics->>'device_scope' AS device_scope
		FROM observations e CROSS JOIN LATERAL jsonb_each(e.metrics) value
		ORDER BY value.key,e.collected_at DESC,e.source
	), combined AS (
		SELECT (jsonb_object_agg(key,value) - 'device_scope') || jsonb_build_object('device_scope',
			CASE WHEN bool_and(device_scope = 'all') THEN 'all' ELSE m.device END) AS metrics,
			jsonb_object_agg(key,jsonb_build_object('source',source,'snapshot_id',snapshot_id,
			'collected_at',collected_at,'device_scope',device_scope)) FILTER (WHERE key NOT IN ('keyword','device_scope')) AS sources
		FROM fields
	)
	SELECT combined.metrics,combined.sources,e.source,e.snapshot_id,e.collected_at FROM combined
	CROSS JOIN LATERAL (SELECT source,snapshot_id,collected_at FROM observations
		ORDER BY collected_at DESC,source LIMIT 1) e
	) metric ON true `

const intentJoin = `LEFT JOIN public.seo_search_intents i
	ON i.organization_id=sk.organization_id AND i.market_id=sk.market_id AND i.keyword_id=sk.keyword_id `

const latestSERP = `JOIN LATERAL (
	SELECT sr.id,sr.source,sr.collected_at
	FROM public.seo_serp_snapshots sr WHERE sr.organization_id=sk.organization_id
	AND sr.market_id=sk.market_id AND sr.keyword_id=sk.keyword_id
	ORDER BY sr.collected_at DESC,sr.id LIMIT 1
	) sr ON true `

const scopedResearch = ` WHERE sk.organization_id=$1 AND sk.site_id=$2 AND (NULLIF($3, '') IS NULL OR sk.market_id=NULLIF($3,'')::uuid) `

func (s *Store) ListResearch(ctx context.Context, org, section string, q domain.ResearchQuery) ([]json.RawMessage, error) {
	if section == "usage" {
		return s.ResearchUsage(ctx, org, q)
	}
	if !domain.ValidID(q.SiteID) {
		return nil, &domain.ValidationError{Field: "site_id", Message: "is required for research evidence"}
	}
	market, err := s.validateResearchQuery(ctx, org, q)
	// An unrecorded market is an empty result, but an inaccessible site is 404.
	if errors.Is(err, domain.ErrNotFound) && q.Market != nil {
		if _, siteErr := s.GetSite(ctx, org, q.SiteID); siteErr == nil {
			return []json.RawMessage{}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	if section == "keywords" {
		return jsonRows(ctx, s.db, `SELECT jsonb_build_object('id',k.id,'keyword',k.keyword,'market',`+marketJSON+`,
			'metrics',metric.metrics,'metric_sources',metric.sources,'source',metric.source,'snapshot_id',metric.snapshot_id,
			'collected_at',metric.collected_at,'intent',i.intent)`+linkedKeywords+bestMetric+intentJoin+scopedResearch+
			`AND ($6 = '' OR strpos(k.normalized,$6) > 0)
			ORDER BY metric.collected_at DESC NULLS LAST,k.normalized,sk.market_id LIMIT $4 OFFSET $5`,
			org, q.SiteID, market, q.Limit, q.Offset, domain.KeywordKey(q.Query))
	}
	if section == "suggestions" {
		return jsonRows(ctx, s.db, `SELECT jsonb_build_object('id',suggested.id,'seed',k.keyword,'keyword',suggested.keyword,
			'market',`+marketJSON+`,'operation',v.operation,'source',c.source,'snapshot_id',v.snapshot_id,'collected_at',v.collected_at)`+
			linkedKeywords+`JOIN public.seo_keyword_seeds seed ON (seed.organization_id,seed.site_id,seed.market_id,seed.keyword_id)
			=(sk.organization_id,sk.site_id,sk.market_id,sk.keyword_id)
			JOIN public.seo_keyword_suggestions v ON (v.organization_id,v.market_id,v.seed_id)=(seed.organization_id,seed.market_id,seed.keyword_id)
			JOIN public.seo_keywords suggested ON (suggested.organization_id,suggested.id)=(v.organization_id,v.keyword_id)
			JOIN public.seo_research_calls c ON (c.organization_id,c.id)=(v.organization_id,v.snapshot_id)`+scopedResearch+
			`AND ($6='' OR strpos(k.normalized,$6)>0)
			ORDER BY v.collected_at DESC,k.id,suggested.id,v.operation,sk.market_id LIMIT $4 OFFSET $5`,
			org, q.SiteID, market, q.Limit, q.Offset, domain.KeywordKey(q.Query))
	}

	var statement string
	switch section {
	case "seeds":
		statement = `SELECT jsonb_build_object('id',k.id,'keyword',k.keyword,'market',` + marketJSON + `,'collected_at',seed.updated_at)` +
			linkedKeywords + `JOIN public.seo_keyword_seeds seed ON (seed.organization_id,seed.site_id,seed.market_id,seed.keyword_id)
			=(sk.organization_id,sk.site_id,sk.market_id,sk.keyword_id)` + scopedResearch + `ORDER BY seed.updated_at DESC,k.id,sk.market_id`
	case "serps":
		statement = `SELECT jsonb_build_object('id',sr.id,'keyword',k.keyword,'market',` + marketJSON + `,
			'source',sr.source,'collected_at',sr.collected_at,'snapshot_id',sr.id,'results',` +
			`COALESCE((SELECT jsonb_agg(rank.result ORDER BY rank.rank) FROM public.seo_serp_results rank
			WHERE rank.organization_id=sk.organization_id AND rank.snapshot_id=sr.id),'[]'::jsonb))` +
			linkedKeywords + latestSERP + scopedResearch + `ORDER BY sr.collected_at DESC,sr.id`
	case "competitors":
		statement = `SELECT jsonb_build_object('id',snap.id,'keyword',k.keyword,'market',` + marketJSON + `,
			'page',snap.page,'gaps',c.gaps,'source',snap.source,'snapshot_id',snap.id,'collected_at',snap.collected_at)` +
			linkedKeywords + `JOIN public.seo_research_competitors c ON
			(c.organization_id,c.site_id,c.market_id,c.keyword_id)=(sk.organization_id,sk.site_id,sk.market_id,sk.keyword_id)
			JOIN public.seo_competitor_page_snapshots snap ON snap.organization_id=c.organization_id AND snap.id=c.snapshot_id` +
			scopedResearch + `ORDER BY snap.collected_at DESC,snap.id,k.id`
	case "intents":
		statement = `SELECT jsonb_build_object('id',k.id,'keyword',k.keyword,'market',` + marketJSON + `,
			'intent',i.intent,'source','analysis','collected_at',i.collected_at)` + linkedKeywords + intentJoin + scopedResearch +
			`AND i.intent IS NOT NULL ORDER BY i.collected_at DESC,k.id,sk.market_id`
	case "mappings":
		statement = derivedList("seo_keyword_url_mappings", "mapping")
	case "opportunities":
		statement = derivedList("seo_keyword_opportunities", "opportunity")
	case "clusters":
		statement = `SELECT sk.cluster || jsonb_build_object('market',` + marketJSON + `,
			'collected_at',sk.collected_at,'source','analysis')
			FROM public.seo_keyword_clusters sk JOIN public.seo_research_markets m
			ON m.organization_id=sk.organization_id AND m.id=sk.market_id` +
			scopedResearch + `ORDER BY sk.collected_at DESC,sk.cluster_key`
	case "pages":
		statement = `SELECT jsonb_build_object('id',sk.id,'market',` + marketJSON + `,
			'page',sk.page,'source',sk.source,'collected_at',sk.collected_at)
			FROM public.seo_research_site_pages sk JOIN public.seo_research_markets m
			ON m.organization_id=sk.organization_id AND m.id=sk.market_id` +
			scopedResearch + `ORDER BY sk.collected_at DESC,sk.id`
	default:
		return nil, domain.ErrNotFound
	}
	return jsonRows(ctx, s.db, statement+` LIMIT $4 OFFSET $5`, org, q.SiteID, market, q.Limit, q.Offset)
}

func derivedList(table, column string) string {
	return `SELECT jsonb_build_object('id',k.id,'keyword',k.keyword,'market',` + marketJSON +
		`,'` + column + `',v.` + column + `,'source','analysis','collected_at',v.collected_at)` + linkedKeywords +
		`JOIN public.` + table + ` v ON (v.organization_id,v.site_id,v.market_id,v.keyword_id)
		=(sk.organization_id,sk.site_id,sk.market_id,sk.keyword_id)` + scopedResearch +
		`ORDER BY v.collected_at DESC,k.id,sk.market_id`
}

func (s *Store) ResearchEvidence(ctx context.Context, job domain.Job, scope domain.ResearchContext) ([]domain.KeywordEvidence, error) {
	keyword := scope.KeywordID
	if job.Kind == domain.KeywordClustering {
		keyword = ""
	}
	rows, err := s.db.QueryContext(ctx, `SELECT k.id::text,k.keyword,
		COALESCE(metric.metrics,'{}'::jsonb),COALESCE(metric.sources,'{}'::jsonb),COALESCE(i.intent,'null'::jsonb),
		COALESCE((SELECT jsonb_agg(rank.result ORDER BY rank.rank) FROM public.seo_serp_results rank
				WHERE rank.organization_id=sk.organization_id AND rank.snapshot_id=sr.id),'[]'::jsonb),
		COALESCE((SELECT jsonb_agg(p.page ORDER BY p.collected_at DESC) FROM public.seo_research_competitors c
			JOIN public.seo_competitor_page_snapshots p ON p.organization_id=c.organization_id AND p.id=c.snapshot_id
			WHERE (c.organization_id,c.site_id,c.market_id,c.keyword_id)=(sk.organization_id,sk.site_id,sk.market_id,sk.keyword_id)),'[]'::jsonb)`+
		linkedKeywords+bestMetric+intentJoin+`LEFT `+latestSERP+scopedResearch+
		`AND (NULLIF($4,'') IS NULL OR k.id=NULLIF($4,'')::uuid)
		ORDER BY (sr.id IS NOT NULL) DESC,metric.collected_at DESC NULLS LAST,k.id LIMIT 300`,
		job.OrganizationID, job.SiteID, scope.MarketID, keyword)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.KeywordEvidence{}
	for rows.Next() {
		var e domain.KeywordEvidence
		var metric, sources, intent, serp, pages []byte
		if err := rows.Scan(&e.ID, &e.Term, &metric, &sources, &intent, &serp, &pages); err != nil {
			return nil, err
		}
		for _, part := range []struct {
			raw []byte
			dst any
		}{
			{metric, &e.Metric}, {sources, &e.MetricSources}, {intent, &e.Intent}, {serp, &e.SERP}, {pages, &e.Pages},
		} {
			if err := json.Unmarshal(part.raw, part.dst); err != nil {
				return nil, err
			}
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

func (s *Store) ResearchOwnedPages(ctx context.Context, job domain.Job, marketID string) ([]domain.PageEvidence, error) {
	raw, err := jsonRows(ctx, s.db, `SELECT page FROM public.seo_research_site_pages
		WHERE organization_id=$1 AND site_id=$2 AND market_id=$3 ORDER BY collected_at DESC,id LIMIT 500`,
		job.OrganizationID, job.SiteID, marketID)
	if err != nil {
		return nil, err
	}
	pages := make([]domain.PageEvidence, 0, len(raw))
	for _, value := range raw {
		var page domain.PageEvidence
		if err := json.Unmarshal(value, &page); err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

func (s *Store) ExistingMapping(ctx context.Context, job domain.Job, scope domain.ResearchContext) (*domain.URLMapping, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT mapping FROM public.seo_keyword_url_mappings
		WHERE organization_id=$1 AND site_id=$2 AND market_id=$3 AND keyword_id=$4`,
		job.OrganizationID, job.SiteID, scope.MarketID, scope.KeywordID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var mapping domain.URLMapping
	err = json.Unmarshal(raw, &mapping)
	return &mapping, err
}

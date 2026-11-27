package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func (s *Store) PersistData(ctx context.Context, lease domain.Lease, scope domain.ResearchContext, operation domain.DataOperation, snapshot string, result domain.DataResponse, gaps []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	org, market := job.OrganizationID, scope.MarketID
	collected := result.CollectedAt
	if collected.IsZero() {
		collected = time.Now().UTC()
	}
	switch operation {
	case domain.DataSERP:
		err = persistSERP(ctx, tx, org, scope, snapshot, result, collected)
	case domain.DataPage:
		err = persistCompetitorPage(ctx, tx, job, scope, snapshot, result, collected, gaps)
	case domain.DataRelated, domain.DataSuggestions, domain.DataOverview, domain.DataVolume:
		err = persistKeywords(ctx, tx, job, scope, snapshot, operation, result, collected)
	default:
		return errors.New("unknown data operation")
	}
	if err != nil {
		return err
	}
	if err := audit(ctx, tx, org, "research.evidence.save", job.ID, map[string]any{
		"market_id": market, "keyword_id": scope.KeywordID, "snapshot_id": snapshot,
		"operation": operation, "keyword_results": len(result.Keywords), "serp_results": len(result.Results),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func persistKeywords(ctx context.Context, tx *sql.Tx, job domain.Job, scope domain.ResearchContext, snapshot string, operation domain.DataOperation, result domain.DataResponse, collected time.Time) error {
	org, market := job.OrganizationID, scope.MarketID
	for _, record := range result.Keywords {
		term := strings.Join(strings.Fields(record.Keyword), " ")
		key := domain.KeywordKey(term)
		if key == "" || len(key) > 400 || len(term) > 400 || !utf8.ValidString(term) || strings.ContainsRune(term, 0) {
			continue
		}
		id, err := ensureKeyword(ctx, tx, org, term)
		if err != nil {
			return err
		}
		if err := associateKeyword(ctx, tx, job, market, id); err != nil {
			return err
		}
		payload, err := json.Marshal(record)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_keyword_metrics
			(organization_id,market_id,keyword_id,source,snapshot_id,metrics,collected_at)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
			ON CONFLICT (organization_id,market_id,keyword_id,source) DO UPDATE SET
			snapshot_id=EXCLUDED.snapshot_id,metrics=EXCLUDED.metrics,collected_at=EXCLUDED.collected_at,updated_at=clock_timestamp()
			WHERE public.seo_keyword_metrics.collected_at < EXCLUDED.collected_at`,
			org, market, id, result.Source, snapshot, string(payload), collected)
		if err != nil {
			return err
		}
		suggested := operation == domain.DataRelated || operation == domain.DataSuggestions
		if suggested && id != scope.KeywordID {
			_, err := tx.ExecContext(ctx, `INSERT INTO public.seo_keyword_suggestions
				(organization_id,market_id,seed_id,keyword_id,operation,snapshot_id,collected_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
				ON CONFLICT (organization_id,market_id,seed_id,keyword_id,operation) DO UPDATE SET
				snapshot_id=EXCLUDED.snapshot_id,collected_at=EXCLUDED.collected_at,updated_at=clock_timestamp()
				WHERE public.seo_keyword_suggestions.collected_at < EXCLUDED.collected_at`,
				org, market, scope.KeywordID, id, operation, snapshot, collected)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func persistSERP(ctx context.Context, tx *sql.Tx, org string, scope domain.ResearchContext, snapshot string, result domain.DataResponse, collected time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO public.seo_serp_snapshots
		(id,organization_id,market_id,keyword_id,source,collected_at)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO NOTHING`, snapshot, org, scope.MarketID, scope.KeywordID, result.Source, collected)
	if err != nil {
		return err
	}
	for _, entry := range result.Results {
		if _, err := domain.EvidenceURL(entry.URL); err != nil || entry.Rank < 1 || entry.Rank > 10 {
			continue
		}
		payload, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_serp_results
			(organization_id,snapshot_id,rank,url,result) VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT DO NOTHING`,
			org, snapshot, entry.Rank, entry.URL, string(payload))
		if err != nil {
			return err
		}
	}
	return nil
}

func persistCompetitorPage(ctx context.Context, tx *sql.Tx, job domain.Job, scope domain.ResearchContext, snapshot string, result domain.DataResponse, collected time.Time, gaps []string) error {
	if result.Page == nil {
		return errors.New("page evidence is unavailable")
	}
	org := job.OrganizationID
	page := *result.Page
	page.Source = result.Source
	payload, err := json.Marshal(page)
	if err != nil {
		return err
	}
	if gaps == nil {
		gaps = []string{}
	}
	gapJSON, err := json.Marshal(gaps)
	if err != nil {
		return err
	}
	target := scope.Request.TargetURL
	digest := sha256.Sum256([]byte(target))
	var sourceMarket string
	if err := tx.QueryRowContext(ctx, `SELECT market_id::text FROM public.seo_research_calls
		WHERE organization_id=$1 AND id=$2`, org, snapshot).Scan(&sourceMarket); err != nil {
		return err
	}
	var pageID string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_competitor_pages
		(id,organization_id,market_id,url,url_digest) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (organization_id,market_id,url_digest) DO UPDATE SET url=EXCLUDED.url RETURNING id::text`,
		uuid.NewString(), org, sourceMarket, target, digest[:]).Scan(&pageID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_competitor_page_snapshots
		(id,organization_id,page_id,source,page,collected_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6)
		ON CONFLICT (id) DO UPDATE SET page=EXCLUDED.page
		WHERE jsonb_array_length(COALESCE(public.seo_competitor_page_snapshots.page->'source_snapshots','[]'::jsonb)) <=
		jsonb_array_length(COALESCE(EXCLUDED.page->'source_snapshots','[]'::jsonb))`, snapshot, org, pageID, result.Source, string(payload), collected)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_research_competitors
		(organization_id,site_id,market_id,keyword_id,page_id,snapshot_id,gaps,collected_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
		ON CONFLICT (organization_id,site_id,market_id,keyword_id,page_id) DO UPDATE SET
		snapshot_id=EXCLUDED.snapshot_id,gaps=EXCLUDED.gaps,collected_at=EXCLUDED.collected_at,updated_at=clock_timestamp()
		WHERE public.seo_research_competitors.collected_at <= EXCLUDED.collected_at`,
		org, job.SiteID, scope.MarketID, scope.KeywordID, pageID, snapshot, string(gapJSON), collected)
	return err
}

func (s *Store) PersistResearchAnalysis(ctx context.Context, lease domain.Lease, scope domain.ResearchContext, result domain.ResearchAnalysis) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	org, site, market, key := job.OrganizationID, job.SiteID, scope.MarketID, scope.KeywordID
	switch job.Kind {
	case domain.KeywordIntent:
		payload, err := json.Marshal(result.Intent)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_search_intents
			(organization_id,market_id,keyword_id,intent,collected_at) VALUES ($1,$2,$3,$4::jsonb,clock_timestamp())
			ON CONFLICT (organization_id,market_id,keyword_id) DO UPDATE SET
			intent=EXCLUDED.intent,collected_at=EXCLUDED.collected_at,updated_at=clock_timestamp()`, org, market, key, string(payload))
		if err != nil {
			return err
		}
	case domain.KeywordClustering:
		if err := replaceClusters(ctx, tx, job, scope, result.Clusters); err != nil {
			return err
		}
	case domain.KeywordURLMapping:
		if err := upsertAnalysis(ctx, tx, "seo_keyword_url_mappings", "mapping", org, site, market, key, result.Mapping); err != nil {
			return err
		}
	case domain.OpportunityScoring:
		if err := upsertAnalysis(ctx, tx, "seo_keyword_opportunities", "opportunity", org, site, market, key, result.Opportunity); err != nil {
			return err
		}
	default:
		return errors.New("unknown research analysis kind")
	}
	if err := audit(ctx, tx, org, "research.analysis.save", job.ID, map[string]any{
		"market_id": market, "keyword_id": key, "kind": job.Kind, "clusters": len(result.Clusters),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertAnalysis(ctx context.Context, tx *sql.Tx, table, column, org, site, market, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.`+table+
		` (organization_id,site_id,market_id,keyword_id,`+column+`,collected_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,clock_timestamp())
		ON CONFLICT (organization_id,site_id,market_id,keyword_id) DO UPDATE SET `+column+`=EXCLUDED.`+column+
		`,collected_at=EXCLUDED.collected_at,updated_at=clock_timestamp()`, org, site, market, key, string(payload))
	return err
}

func replaceClusters(ctx context.Context, tx *sql.Tx, job domain.Job, scope domain.ResearchContext, clusters []domain.KeywordCluster) error {
	org, site, market := job.OrganizationID, job.SiteID, scope.MarketID
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('clusters:' || $1 || $2 || $3, 0))`, org, site, market); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM public.seo_keyword_clusters WHERE organization_id=$1 AND site_id=$2 AND market_id=$3`, org, site, market); err != nil {
		return err
	}
	for _, cluster := range clusters {
		payload, err := json.Marshal(cluster)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_keyword_clusters
			(organization_id,site_id,market_id,cluster_key,cluster,collected_at)
			VALUES ($1,$2,$3,$4,$5::jsonb,clock_timestamp())`, org, site, market, cluster.ID, string(payload))
		if err != nil {
			return err
		}
		for _, memberID := range cluster.MemberIDs {
			_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_keyword_cluster_members
				(organization_id,site_id,market_id,cluster_key,keyword_id)
				VALUES ($1,$2,$3,$4,$5)`, org, site, market, cluster.ID, memberID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

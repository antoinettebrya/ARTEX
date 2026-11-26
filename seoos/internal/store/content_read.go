package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

type contentState struct {
	ExecutionFeedback json.RawMessage              `json:"execution_feedback,omitempty"`
	Strategy          *domain.ContentStrategy      `json:"strategy,omitempty"`
	BriefVersion      int                          `json:"brief_version"`
	Outline           []domain.ContentHeading      `json:"outline,omitempty"`
	ArticleVersion    int                          `json:"article_version"`
	FactIDs           []string                     `json:"fact_ids,omitempty"`
	FactCheck         *domain.FactReport           `json:"fact_check,omitempty"`
	Review            *domain.EditorialReview      `json:"review,omitempty"`
	Quality           *domain.ContentQuality       `json:"quality,omitempty"`
	Refresh           *domain.ContentRefreshResult `json:"refresh,omitempty"`
	Signals           domain.RefreshSignals        `json:"signals"`
	LastWriterID      string                       `json:"last_writer_id,omitempty"`
	LastWriter        string                       `json:"last_writer,omitempty"`
}

type contentRecord struct {
	domain.ContentProject
	siteURL  string
	keyID    string
	marketID string
	state    contentState
}

const contentProjectJSON = `jsonb_build_object('id',p.id,'organization_id',p.organization_id,'site_id',p.site_id,
	'keyword',k.keyword,'market',` + marketJSON + `,'revision',p.revision,'status',p.status,'created_at',p.created_at,'updated_at',p.updated_at)`
const contentProjectsFrom = ` FROM public.seo_content_projects p
	JOIN public.seo_research_markets m ON (m.organization_id,m.id)=(p.organization_id,p.market_id)
	JOIN public.seo_keywords k ON (k.organization_id,k.id)=(p.organization_id,p.keyword_id)
	JOIN public.seo_sites s ON (s.organization_id,s.id)=(p.organization_id,p.site_id) `

func contentDecodeList[T any](rows []json.RawMessage, err error) ([]T, error) {
	if err != nil {
		return nil, err
	}
	list := make([]T, 0, len(rows))
	for _, raw := range rows {
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		list = append(list, value)
	}
	return list, nil
}

func readContentRecord(ctx context.Context, db rowQueryer, org, id, lock string) (contentRecord, error) {
	var p contentRecord
	var project, state []byte
	if !domain.ValidID(id) {
		return p, domain.ErrNotFound
	}
	err := db.QueryRowContext(ctx, `SELECT `+contentProjectJSON+`,p.data,s.base_url,p.keyword_id::text,p.market_id::text`+
		contentProjectsFrom+`WHERE p.organization_id=$1 AND p.id=$2 `+lock, org, id).Scan(&project, &state, &p.siteURL, &p.keyID, &p.marketID)
	if err != nil {
		return p, classifyError(err)
	}
	if err := json.Unmarshal(project, &p.ContentProject); err != nil {
		return p, err
	}
	return p, json.Unmarshal(state, &p.state)
}

func (s *Store) CreateContentProject(ctx context.Context, org string, in domain.ContentProjectInput) (domain.ContentProject, bool, error) {
	var project domain.ContentProject
	if !domain.ValidID(in.SiteID) || len(in.Keyword) > 400 || domain.KeywordKey(in.Keyword) == "" {
		return project, false, &domain.ValidationError{Field: "keyword", Message: "choose a site and its existing Phase 2 keyword"}
	}
	market, err := in.Market.Normalize()
	if err != nil {
		return project, false, err
	}
	if _, err := s.GetSite(ctx, org, in.SiteID); err != nil {
		return project, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return project, false, err
	}
	defer tx.Rollback()
	var marketID, keyID string
	err = tx.QueryRowContext(ctx, `SELECT m.id::text,k.id::text FROM public.seo_research_site_keywords sk
		JOIN public.seo_research_markets m ON (m.organization_id,m.id)=(sk.organization_id,sk.market_id)
		JOIN public.seo_keywords k ON (k.organization_id,k.id)=(sk.organization_id,sk.keyword_id)
		WHERE sk.organization_id=$1 AND sk.site_id=$2 AND m.country_code=$3 AND m.location_code=$4
		AND m.language_code=$5 AND m.search_engine=$6 AND m.device=$7 AND k.normalized=$8`,
		org, in.SiteID, market.CountryCode, market.LocationCode, market.LanguageCode, market.SearchEngine, market.Device,
		domain.KeywordKey(in.Keyword)).Scan(&marketID, &keyID)
	if errors.Is(err, sql.ErrNoRows) {
		return project, false, domain.ErrContentPrerequisite
	}
	if err != nil {
		return project, false, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_content_projects (id,organization_id,site_id,market_id,keyword_id)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (organization_id,site_id,market_id,keyword_id) DO NOTHING RETURNING id::text`,
		uuid.NewString(), org, in.SiteID, marketID, keyID).Scan(&id)
	created := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.seo_content_projects
			WHERE organization_id=$1 AND site_id=$2 AND market_id=$3 AND keyword_id=$4`, org, in.SiteID, marketID, keyID).Scan(&id)
	}
	if err != nil {
		return project, false, err
	}
	record, err := readContentRecord(ctx, tx, org, id, "")
	if err != nil {
		return project, false, err
	}
	if created {
		if err := audit(ctx, tx, org, "content.project.create", id, record.ContentProject); err != nil {
			return project, false, err
		}
	}
	return record.ContentProject, created, tx.Commit()
}

func (s *Store) ListContentProjects(ctx context.Context, org, site string, limit, offset int) ([]domain.ContentProject, error) {
	if _, err := s.GetSite(ctx, org, site); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	return contentDecodeList[domain.ContentProject](jsonRows(ctx, s.db, `SELECT `+contentProjectJSON+contentProjectsFrom+
		`WHERE p.organization_id=$1 AND p.site_id=$2 ORDER BY p.updated_at DESC,p.id LIMIT $3 OFFSET $4`, org, site, limit, offset))
}

func (s *Store) ContentWorkspace(ctx context.Context, org, id string) (domain.ContentWorkspace, error) {
	return s.contentWorkspace(ctx, org, id, false)
}

func (s *Store) ContentWork(ctx context.Context, org, id string) (domain.ContentWorkspace, error) {
	return s.contentWorkspace(ctx, org, id, true)
}

func (s *Store) contentWorkspace(ctx context.Context, org, id string, worker bool) (domain.ContentWorkspace, error) {
	p, err := readContentRecord(ctx, s.db, org, id, "")
	if err != nil {
		return domain.ContentWorkspace{}, err
	}
	state := p.state
	w := domain.ContentWorkspace{Project: p.ContentProject, SiteURL: p.siteURL, Strategy: state.Strategy,
		BriefVersion: state.BriefVersion, Outline: state.Outline, ArticleVersion: state.ArticleVersion,
		FactCheck: state.FactCheck, Review: state.Review, Quality: state.Quality, Refresh: state.Refresh,
		RefreshSignals: state.Signals, LastWriterID: state.LastWriterID}
	if state.BriefVersion != 0 {
		w.Brief, err = readContentVersion[domain.ContentBrief](ctx, s.db, p, true, state.BriefVersion)
		if err != nil {
			return w, err
		}
	}
	if state.ArticleVersion != 0 {
		w.Article, err = readContentVersion[domain.ContentArticle](ctx, s.db, p, false, state.ArticleVersion)
		if err != nil {
			return w, err
		}
	}
	w.Evidence, err = s.phaseTwoEvidence(ctx, p)
	if err != nil {
		return w, err
	}
	w.Sources, err = s.contentSources(ctx, p)
	if err != nil {
		return w, err
	}
	w.Facts, err = contentDecodeList[domain.ContentFact](jsonRows(ctx, s.db, `SELECT data FROM public.seo_content_facts
		WHERE organization_id=$1 AND site_id=$2 AND project_id=$3 AND id=ANY($4::text[]) ORDER BY id`,
		p.OrganizationID, p.SiteID, p.ID, state.FactIDs))
	if err != nil {
		return w, err
	}
	w.Publications, err = contentDecodeList[domain.ContentPublication](jsonRows(ctx, s.db, `SELECT data FROM public.seo_content_publications
		WHERE organization_id=$1 AND site_id=$2 AND project_id=$3 ORDER BY version DESC LIMIT 20`, p.OrganizationID, p.SiteID, p.ID))
	if err != nil {
		return w, err
	}
	if worker {
		w.Assets, err = inventoryRows(ctx, s.db, p.OrganizationID, p.SiteID, true, 80, 0)
		return w, err
	}
	w.History, err = contentHistory(ctx, s.db, p)
	return w, err
}

func readContentVersion[T any](ctx context.Context, db rowQueryer, p contentRecord, brief bool, version int) (*T, error) {
	table := "seo_content_versions"
	if brief {
		table = "seo_content_brief_versions"
	}
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT data FROM public.`+table+`
		WHERE organization_id=$1 AND site_id=$2 AND project_id=$3 AND version=$4`,
		p.OrganizationID, p.SiteID, p.ID, version).Scan(&raw)
	if err != nil {
		return nil, classifyError(err)
	}
	var value T
	return &value, json.Unmarshal(raw, &value)
}

func contentHistory(ctx context.Context, db queryer, p contentRecord) ([]domain.ContentVersion, error) {
	return contentDecodeList[domain.ContentVersion](jsonRows(ctx, db, `SELECT value FROM (
		SELECT created_at,jsonb_build_object('number',version,'created_at',created_at,'reason',reason,'brief',data) value
		  FROM public.seo_content_brief_versions WHERE organization_id=$1 AND site_id=$2 AND project_id=$3
		UNION ALL
		SELECT v.created_at,jsonb_build_object('number',v.version,'created_at',v.created_at,'reason',v.reason,'article',v.data,
			'fact_check',(SELECT r.data FROM public.seo_content_reviews r
				WHERE (r.organization_id,r.site_id,r.project_id,r.version)=(v.organization_id,v.site_id,v.project_id,v.version)
				AND r.kind='fact_check' ORDER BY r.created_at DESC LIMIT 1),
			'quality',(SELECT q.data FROM public.seo_content_quality_results q
				WHERE (q.organization_id,q.site_id,q.project_id,q.version)=(v.organization_id,v.site_id,v.project_id,v.version)
				ORDER BY q.created_at DESC LIMIT 1)) value
		  FROM public.seo_content_versions v WHERE organization_id=$1 AND site_id=$2 AND project_id=$3
	) x ORDER BY created_at DESC LIMIT 30`, p.OrganizationID, p.SiteID, p.ID))
}

func (s *Store) contentSources(ctx context.Context, p contentRecord) ([]domain.ContentSource, error) {
	return contentDecodeList[domain.ContentSource](jsonRows(ctx, s.db, `SELECT data - 'text' FROM public.seo_content_sources
		WHERE organization_id=$1 AND site_id=$2 AND project_id=$3 ORDER BY observed_at DESC LIMIT 50`,
		p.OrganizationID, p.SiteID, p.ID))
}

func (s *Store) phaseTwoEvidence(ctx context.Context, p contentRecord) (domain.ContentEvidence, error) {
	var e domain.ContentEvidence
	scope := domain.ResearchContext{MarketID: p.marketID, KeywordID: p.keyID}
	job := domain.Job{OrganizationID: p.OrganizationID, JobInput: domain.JobInput{SiteID: p.SiteID}}
	list, err := s.ResearchEvidence(ctx, job, scope)
	if err != nil {
		return e, err
	}
	if len(list) == 1 {
		e.Keyword = list[0]
	}
	e.Mapping, err = s.ExistingMapping(ctx, job, scope)
	if err != nil {
		return e, err
	}
	org, site, market, keyword := p.OrganizationID, p.SiteID, p.marketID, p.keyID
	var score []byte
	err = s.db.QueryRowContext(ctx, `SELECT
		COALESCE((SELECT id::text FROM public.seo_serp_snapshots WHERE organization_id=$1 AND market_id=$3 AND keyword_id=$4
			ORDER BY collected_at DESC,id LIMIT 1),''),
		COALESCE((SELECT opportunity FROM public.seo_keyword_opportunities
			WHERE organization_id=$1 AND site_id=$2 AND market_id=$3 AND keyword_id=$4),'null'::jsonb)`,
		org, site, market, keyword).Scan(&e.SERPSnapshotID, &score)
	if err != nil {
		return e, err
	}
	if err = json.Unmarshal(score, &e.Opportunity); err != nil {
		return e, err
	}
	e.Clusters, err = contentDecodeList[domain.KeywordCluster](jsonRows(ctx, s.db, `SELECT c.cluster
		FROM public.seo_keyword_clusters c JOIN public.seo_keyword_cluster_members n
		ON (n.organization_id,n.site_id,n.market_id,n.cluster_key)=(c.organization_id,c.site_id,c.market_id,c.cluster_key)
		WHERE n.organization_id=$1 AND n.site_id=$2 AND n.market_id=$3 AND n.keyword_id=$4
		ORDER BY c.collected_at DESC LIMIT 10`, org, site, market, keyword))
	if err != nil {
		return e, err
	}
	e.Competitors, err = contentDecodeList[domain.ContentCompetitor](jsonRows(ctx, s.db, `SELECT
		jsonb_build_object('snapshot_id',snap.id,'page',snap.page,'gaps',c.gaps)
		FROM public.seo_research_competitors c JOIN public.seo_competitor_page_snapshots snap
		ON (snap.organization_id,snap.id)=(c.organization_id,c.snapshot_id)
		WHERE c.organization_id=$1 AND c.site_id=$2 AND c.market_id=$3 AND c.keyword_id=$4
		ORDER BY snap.collected_at DESC LIMIT 10`, org, site, market, keyword))
	return e, err
}

func validContentNote(note string) bool {
	return strings.TrimSpace(note) != "" && len(note) <= 1500 && !strings.ContainsRune(note, 0)
}

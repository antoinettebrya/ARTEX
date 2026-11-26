package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func (s *Store) ApplyContentAction(ctx context.Context, org, id string, in domain.ContentAction) (domain.ContentWorkspace, error) {
	var empty domain.ContentWorkspace
	actor := domain.PrincipalFromContext(ctx)
	role := actor.Role
	if actor.OrganizationID != org || !role.CanWrite() || ((in.Action == "approve" || in.Action == "prepare_publication") && role != domain.RoleOwner) {
		return empty, domain.ErrContentForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	p, err := readContentRecord(ctx, tx, org, id, "FOR NO KEY UPDATE OF p")
	if err != nil {
		return empty, err
	}
	if p.Revision != in.Revision {
		return empty, domain.ErrVersionConflict
	}
	switch in.Action {
	case "save_brief", "restore_brief":
		brief := in.Brief
		if in.Action == "restore_brief" {
			brief, err = readContentVersion[domain.ContentBrief](ctx, tx, p, true, in.Version)
		}
		if err == nil && brief == nil {
			err = domain.ErrContentPrerequisite
		}
		if err == nil {
			err = writeBrief(ctx, tx, &p, *brief, in.Action)
		}
	case "save_article", "restore_article":
		article := in.Article
		if in.Action == "restore_article" {
			article, err = readContentVersion[domain.ContentArticle](ctx, tx, p, false, in.Version)
		}
		if err == nil && article == nil {
			err = domain.ErrContentPrerequisite
		}
		if err == nil {
			err = writeArticle(ctx, tx, &p, *article, in.Action)
		}
	case "add_source":
		if in.Source == nil {
			err = domain.ErrContentPrerequisite
			break
		}
		src := domain.ContentSource{ID: uuid.NewString(), URL: in.Source.URL, Kind: in.Source.Kind, Status: "pending"}
		err = writeContentSource(ctx, tx, p, domain.SourceDocument{ContentSource: src})
		p.resetReview()
	case "save_signals":
		if in.Signals == nil {
			err = domain.ErrContentPrerequisite
			break
		}
		if in.Signals.EvidenceURL != "" {
			if _, err = domain.EvidenceURL(in.Signals.EvidenceURL); err != nil {
				break
			}
		}
		p.state.Signals = *in.Signals
		if in.Signals.SourceChanged || in.Signals.SERPChanged || in.Signals.IntentChanged {
			p.resetReview()
		}
	case "approve":
		if !passing(p) || !validContentNote(in.Note) {
			err = domain.ErrContentGate
		} else {
			p.Status = "approved"
			err = writeReview(ctx, tx, p, "approval", map[string]string{"note": in.Note})
		}
	case "prepare_publication":
		if !passing(p) || p.Status != "approved" && p.Status != "prepared" {
			err = domain.ErrContentGate
		} else {
			err = preparePublication(ctx, tx, &p)
		}
	default:
		err = &domain.ValidationError{Field: "action", Message: "unknown content action"}
	}
	if err != nil {
		return empty, err
	}
	if err := saveContentState(ctx, tx, p, "content."+in.Action, map[string]any{"version": p.state.ArticleVersion}); err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return s.ContentWorkspace(ctx, org, id)
}

func passing(p contentRecord) bool {
	return p.state.ArticleVersion > 0 && p.state.Review != nil && p.state.Review.Passed &&
		p.state.FactCheck != nil && p.state.FactCheck.Passed && p.state.Quality != nil && p.state.Quality.Passed
}

func (p *contentRecord) resetReview() {
	p.Status = "active"
	p.state.FactCheck, p.state.Review, p.state.Quality = nil, nil, nil
}

func saveContentState(ctx context.Context, tx *sql.Tx, p contentRecord, action string, data any) error {
	raw, err := json.Marshal(p.state)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.seo_content_projects SET data=$4::jsonb,status=$5,
		revision=revision+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND site_id=$2 AND id=$3`,
		p.OrganizationID, p.SiteID, p.ID, string(raw), p.Status); err != nil {
		return err
	}
	return audit(ctx, tx, p.OrganizationID, action, p.ID, data)
}

func writeBrief(ctx context.Context, tx *sql.Tx, p *contentRecord, brief domain.ContentBrief, reason string) error {
	if err := brief.Validate(p.ContentProject); err != nil {
		return err
	}
	version, err := writeVersion(ctx, tx, *p, true, brief, reason)
	if err == nil {
		p.state.BriefVersion = version
		p.state.Outline = nil
		p.resetReview()
	}
	return err
}

func writeArticle(ctx context.Context, tx *sql.Tx, p *contentRecord, article domain.ContentArticle, reason string) error {
	if p.state.BriefVersion == 0 {
		return domain.ErrContentPrerequisite
	}
	if err := article.Validate(p.Market); err != nil {
		return err
	}
	if article.Canonical != "" {
		u, err := domain.EvidenceURL(article.Canonical)
		if err != nil || !domain.BelongsToSite(p.siteURL, u) {
			return &domain.ValidationError{Field: "canonical", Message: "canonical URL must belong to the registered site"}
		}
	}
	version, err := writeVersion(ctx, tx, *p, false, article, reason)
	if err != nil {
		return err
	}
	for i, section := range article.Sections {
		raw, err := json.Marshal(section)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO public.seo_content_sections
			(organization_id,site_id,project_id,version,ordinal,data) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
			p.OrganizationID, p.SiteID, p.ID, version, i, string(raw)); err != nil {
			return err
		}
	}
	p.state.ArticleVersion = version
	p.resetReview()
	return nil
}

func writeVersion(ctx context.Context, tx *sql.Tx, p contentRecord, brief bool, val any, reason string) (int, error) {
	records, versions := "seo_content_drafts", "seo_content_versions"
	if brief {
		records, versions = "seo_content_briefs", "seo_content_brief_versions"
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return 0, err
	}
	var version int
	err = tx.QueryRowContext(ctx, `INSERT INTO public.`+records+` (organization_id,site_id,project_id,version)
		VALUES ($1,$2,$3,1) ON CONFLICT (organization_id,site_id,project_id) DO UPDATE SET version=public.`+records+`.version+1
		RETURNING version`, p.OrganizationID, p.SiteID, p.ID).Scan(&version)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.`+versions+` (organization_id,site_id,project_id,version,reason,data)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb)`, p.OrganizationID, p.SiteID, p.ID, version, reason, string(raw))
	return version, err
}

func writeContentSource(ctx context.Context, tx *sql.Tx, p contentRecord, doc domain.SourceDocument) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(doc.URL))
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_sources
		(id,organization_id,site_id,project_id,url,url_digest,fingerprint,serp_snapshot_id,data)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9::jsonb)
		ON CONFLICT (organization_id,site_id,project_id,url_digest,fingerprint) DO UPDATE SET
		observed_at=clock_timestamp(),serp_snapshot_id=COALESCE(EXCLUDED.serp_snapshot_id,public.seo_content_sources.serp_snapshot_id),
		data=(EXCLUDED.data - 'id' - 'serp_snapshot_id') ||
		jsonb_build_object('id',public.seo_content_sources.id,'serp_snapshot_id',
			COALESCE(EXCLUDED.serp_snapshot_id,public.seo_content_sources.serp_snapshot_id))`,
		doc.ID, p.OrganizationID, p.SiteID, p.ID, doc.URL, digest[:], doc.Fingerprint, doc.SERPSnapshotID, string(raw))
	return err
}

func writeFacts(ctx context.Context, tx *sql.Tx, p *contentRecord, docs []domain.SourceDocument, facts []domain.ContentFact) error {
	replace := make(map[string]string, len(docs))
	for _, doc := range docs {
		if err := writeContentSource(ctx, tx, *p, doc); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(doc.URL))
		var actual string
		if err := tx.QueryRowContext(ctx, `SELECT id::text FROM public.seo_content_sources WHERE (organization_id,site_id,project_id) = ($1,$2,$3)
			AND url_digest=$4 AND fingerprint=$5`, p.OrganizationID, p.SiteID, p.ID, digest[:], doc.Fingerprint).Scan(&actual); err != nil {
			return err
		}
		replace[doc.ID] = actual
	}
	var ids []string
	for _, fact := range facts {
		for i, evidence := range fact.Evidence {
			if resolved := replace[evidence.SourceID]; resolved != "" {
				fact.Evidence[i].SourceID = resolved
			}
		}
		raw, err := json.Marshal(fact)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_facts (organization_id,site_id,project_id,id,data)
			VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,site_id,project_id,id) DO UPDATE SET data=EXCLUDED.data`,
			p.OrganizationID, p.SiteID, p.ID, fact.ID, string(raw))
		if err != nil {
			return err
		}
		for _, evidence := range fact.Evidence {
			data, err := json.Marshal(evidence)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_fact_evidence (organization_id,site_id,project_id,fact_id,source_id,data)
				VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT DO NOTHING`, p.OrganizationID, p.SiteID, p.ID, fact.ID, evidence.SourceID, string(data)); err != nil {
				return err
			}
		}
		ids = append(ids, fact.ID)
	}
	p.state.FactIDs = ids
	p.resetReview()
	return nil
}

func writeReview(ctx context.Context, tx *sql.Tx, p contentRecord, kind string, review any) error {
	raw, err := json.Marshal(review)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_reviews
		(id,organization_id,site_id,project_id,version,kind,data) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		uuid.NewString(), p.OrganizationID, p.SiteID, p.ID, p.state.ArticleVersion, kind, string(raw))
	return err
}

func preparePublication(ctx context.Context, tx *sql.Tx, p *contentRecord) error {
	article, err := readContentVersion[domain.ContentArticle](ctx, tx, *p, false, p.state.ArticleVersion)
	if err != nil {
		return err
	}
	articleJSON, err := json.Marshal(article)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(articleJSON)
	var cited []string
	for _, section := range article.Sections {
		for _, claim := range section.Claims {
			cited = append(cited, claim.FactIDs...)
		}
	}
	sources, err := contentDecodeList[domain.ContentSource](jsonRows(ctx, tx, `SELECT src.data - 'text'
		FROM public.seo_content_sources src WHERE (src.organization_id,src.site_id,src.project_id)=($1,$2,$3)
		AND EXISTS(SELECT 1 FROM public.seo_content_fact_evidence e
			WHERE (e.organization_id,e.site_id,e.project_id,e.source_id)=(src.organization_id,src.site_id,src.project_id,src.id)
			AND e.fact_id=ANY($4::text[]) AND NOT COALESCE((e.data->>'conflict')::boolean,false))
		ORDER BY src.url,src.id`, p.OrganizationID, p.SiteID, p.ID, cited))
	if err != nil {
		return err
	}
	pub := domain.ContentPublication{ID: uuid.NewString(), Version: p.state.ArticleVersion, Status: "prepared", Article: *article,
		Checksum: hex.EncodeToString(sum[:]), Sources: sources}
	raw, err := json.Marshal(pub)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_publications (id,organization_id,site_id,project_id,version,data)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT DO NOTHING`, pub.ID, p.OrganizationID, p.SiteID, p.ID, pub.Version, string(raw))
	if err == nil {
		p.Status = "prepared"
	}
	return err
}

func (s *Store) ContentApplied(ctx context.Context, job domain.Job) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT applied_result FROM public.seo_content_jobs WHERE organization_id=$1 AND job_id=$2`,
		job.OrganizationID, job.ID).Scan(&raw)
	return json.RawMessage(raw), classifyError(err)
}

func (s *Store) SaveContentStage(ctx context.Context, lease domain.Lease, project string, revision int, out domain.ContentStageResult) (json.RawMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	if err := contentJobProject(ctx, tx, job, project); err != nil {
		return nil, err
	}
	p, err := readContentRecord(ctx, tx, job.OrganizationID, project, "FOR NO KEY UPDATE OF p")
	if err != nil {
		return nil, err
	}
	if p.SiteID != job.SiteID || p.Revision != revision {
		return nil, domain.ErrVersionConflict
	}
	switch job.Kind {
	case domain.ContentPlan:
		p.state.Strategy = out.Strategy
		p.resetReview()
	case domain.ContentBriefJob:
		if out.Brief == nil {
			return nil, domain.ErrContentPrerequisite
		}
		err = writeBrief(ctx, tx, &p, *out.Brief, job.Kind)
	case domain.ContentResearch:
		err = writeFacts(ctx, tx, &p, out.Sources, out.Facts)
	case domain.ContentOutline:
		p.state.Outline = out.Outline
		p.resetReview()
	case domain.ContentDraft, domain.ContentOptimize:
		if out.Article == nil {
			return nil, domain.ErrContentPrerequisite
		}
		err = writeArticle(ctx, tx, &p, *out.Article, job.Kind)
		if out.Writer != "" {
			p.state.LastWriter, p.state.LastWriterID = out.Writer, out.WriterID
		}
	case domain.ContentFactCheck:
		if out.FactCheck == nil {
			return nil, domain.ErrContentPrerequisite
		}
		p.resetReview()
		p.state.FactCheck = out.FactCheck
		err = writeReview(ctx, tx, p, "fact_check", out.FactCheck)
	case domain.ContentReview:
		err = saveQuality(ctx, tx, &p, out.Review, out.Quality)
	case domain.ContentRefresh:
		if out.Refresh == nil {
			return nil, domain.ErrContentPrerequisite
		}
		p.state.Refresh = out.Refresh
		var raw []byte
		raw, err = json.Marshal(out.Refresh)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_refresh_jobs
				(organization_id,site_id,project_id,job_id,snapshot_id,data) VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6::jsonb)`,
				p.OrganizationID, p.SiteID, p.ID, job.ID, out.Refresh.SnapshotID, string(raw))
		}
	case domain.ContentVerify:
		if out.Publication == nil {
			return nil, domain.ErrContentPrerequisite
		}
		raw, e := json.Marshal(out.Publication)
		if e != nil {
			return nil, e
		}
		_, err = tx.ExecContext(ctx, `UPDATE public.seo_content_publications SET data=$5::jsonb
			WHERE organization_id=$1 AND site_id=$2 AND project_id=$3 AND id=$4`,
			p.OrganizationID, p.SiteID, p.ID, out.Publication.ID, string(raw))
	default:
		err = domain.ErrContentPrerequisite
	}
	if err != nil {
		return nil, err
	}
	result := map[string]any{"project_id": p.ID, "revision": p.Revision + 1, "kind": job.Kind,
		"article_version": p.state.ArticleVersion, "brief_version": p.state.BriefVersion,
		"serp_snapshot_id": out.SERPSnapshotID, "facts": len(out.Facts)}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE public.seo_content_jobs SET applied_result=$3::jsonb WHERE organization_id=$1 AND job_id=$2`,
		p.OrganizationID, job.ID, string(raw)); err != nil {
		return nil, err
	}
	if err = saveContentState(ctx, tx, p, "content.stage.apply", result); err != nil {
		return nil, err
	}
	return raw, tx.Commit()
}

func saveQuality(ctx context.Context, tx *sql.Tx, p *contentRecord, review *domain.EditorialReview, quality *domain.ContentQuality) error {
	if review == nil || quality == nil || p.state.FactCheck == nil {
		return domain.ErrContentPrerequisite
	}
	p.Status, p.state.Review, p.state.Quality = "active", review, quality
	if err := writeReview(ctx, tx, *p, "editorial", review); err != nil {
		return err
	}
	raw, err := json.Marshal(quality)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_quality_results
		(id,organization_id,site_id,project_id,version,data) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
		uuid.NewString(), p.OrganizationID, p.SiteID, p.ID, p.state.ArticleVersion, string(raw))
	return err
}

// First-party source IDs are fixed for a URL/fingerprint pair even if a fixture
// or worker repeats the same fetch. Registered sources have no fingerprint yet.
func ContentSourceID(project, url, fingerprint string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{project, url, fingerprint}, "\x1f"))).String()
}

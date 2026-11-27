-- Phase 3 extends the existing isolated SEO database and persistent job queue.
ALTER TABLE public.seo_providers DROP CONSTRAINT seo_providers_kind_check,
    ADD CONSTRAINT seo_providers_kind_check CHECK (kind IN ('openai','dataforseo','anthropic','gemini','deepseek')),
    DROP CONSTRAINT seo_providers_max_output_tokens_check,
    ADD CONSTRAINT seo_providers_max_output_tokens_check CHECK (
        (kind <> 'dataforseo' AND max_output_tokens BETWEEN 16 AND 4096)
        OR (kind = 'dataforseo' AND max_output_tokens = 0)
    );

ALTER TABLE public.seo_jobs
    DROP CONSTRAINT seo_jobs_kind_check,
    DROP CONSTRAINT seo_jobs_kind_provider_check,
    ADD CONSTRAINT seo_jobs_kind_check CHECK (kind IN (
        'site_inventory','site_brief','keyword_discovery','keyword_metrics','keyword_suggestions',
        'keyword_intent','keyword_clustering','serp_analysis','competitor_analysis','keyword_url_mapping',
        'opportunity_scoring','page_extraction','content_strategy','content_brief','content_research',
        'content_outline','content_draft','content_fact_check','content_quality_review',
        'content_seo_optimization','content_refresh','content_publish_verify'
    )),
    ADD CONSTRAINT seo_jobs_kind_provider_check CHECK (kind = 'keyword_intent' OR
        ((provider_id IS NOT NULL) = (kind IN ('site_brief','keyword_discovery','keyword_metrics',
        'keyword_suggestions','serp_analysis','competitor_analysis','content_outline',
        'content_draft','content_quality_review')))
    );

CREATE TABLE public.seo_site_crawl_policies (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    configured_base_url text NOT NULL,
    policy jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_site_inventory (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    url text NOT NULL,
    url_digest bytea NOT NULL CHECK (octet_length(url_digest)=32),
    data jsonb NOT NULL,
    current_snapshot_id uuid,
    discovered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    checked_at timestamptz,
    UNIQUE (organization_id,site_id,id),
    UNIQUE (organization_id,site_id,url_digest),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_site_inventory_latest ON public.seo_site_inventory (organization_id,site_id,checked_at DESC NULLS LAST,id);

CREATE TABLE public.seo_site_page_snapshots (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    page_id uuid NOT NULL,
    fingerprint text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,site_id,page_id,id),
    UNIQUE (organization_id,site_id,page_id,fingerprint),
    FOREIGN KEY (organization_id,site_id,page_id)
        REFERENCES public.seo_site_inventory (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_site_inventory_runs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    job_id uuid NOT NULL,
    discovered_count integer NOT NULL CHECK (discovered_count>=0),
    truncated boolean NOT NULL,
    collected_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_jobs (organization_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_content_projects (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    market_id uuid NOT NULL,
    keyword_id uuid NOT NULL,
    revision integer NOT NULL DEFAULT 1 CHECK (revision>=1),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','approved','prepared')),
    data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,site_id,id),
    UNIQUE (organization_id,site_id,market_id,keyword_id),
    FOREIGN KEY (organization_id,site_id,market_id,keyword_id)
        REFERENCES public.seo_research_site_keywords (organization_id,site_id,market_id,keyword_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_content_jobs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    job_id uuid NOT NULL,
    project_id uuid,
    request jsonb NOT NULL,
    applied_result jsonb,
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_jobs (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_content_job_lookup ON public.seo_content_jobs (organization_id,site_id,project_id,job_id);

CREATE TABLE public.seo_content_briefs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL CHECK (version>0),
    PRIMARY KEY (organization_id,site_id,project_id),
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_brief_versions (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL CHECK (version>0),
    reason text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,project_id,version),
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_briefs (organization_id,site_id,project_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_content_sources (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    url text NOT NULL,
    url_digest bytea NOT NULL CHECK (octet_length(url_digest)=32),
    fingerprint text NOT NULL DEFAULT '',
    serp_snapshot_id uuid,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,site_id,project_id,id),
    UNIQUE (organization_id,site_id,project_id,url_digest,fingerprint),
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,serp_snapshot_id)
        REFERENCES public.seo_serp_snapshots (organization_id,id) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_facts (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    id text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,project_id,id),
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_fact_evidence (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    fact_id text NOT NULL,
    source_id uuid NOT NULL,
    data jsonb NOT NULL,
    PRIMARY KEY (organization_id,site_id,project_id,fact_id,source_id),
    FOREIGN KEY (organization_id,site_id,project_id,fact_id)
        REFERENCES public.seo_content_facts (organization_id,site_id,project_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,project_id,source_id)
        REFERENCES public.seo_content_sources (organization_id,site_id,project_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_content_drafts (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL CHECK (version>0),
    PRIMARY KEY (organization_id,site_id,project_id),
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_versions (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL CHECK (version>0),
    reason text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,project_id,version),
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_drafts (organization_id,site_id,project_id) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_sections (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL,
    ordinal integer NOT NULL,
    data jsonb NOT NULL,
    PRIMARY KEY (organization_id,site_id,project_id,version,ordinal),
    FOREIGN KEY (organization_id,site_id,project_id,version)
        REFERENCES public.seo_content_versions (organization_id,site_id,project_id,version) ON DELETE CASCADE
);

CREATE TABLE public.seo_content_reviews (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL,
    kind text NOT NULL CHECK (kind IN ('fact_check','editorial','approval')),
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,site_id,project_id,version)
        REFERENCES public.seo_content_versions (organization_id,site_id,project_id,version) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_quality_results (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,site_id,project_id,version)
        REFERENCES public.seo_content_versions (organization_id,site_id,project_id,version) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_publications (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    version integer NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,site_id,project_id,version),
    FOREIGN KEY (organization_id,site_id,project_id,version)
        REFERENCES public.seo_content_versions (organization_id,site_id,project_id,version) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_refresh_jobs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    project_id uuid NOT NULL,
    job_id uuid NOT NULL,
    snapshot_id uuid,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_content_jobs (organization_id,job_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_content_model_profiles (
    organization_id uuid NOT NULL,
    provider_id uuid NOT NULL,
    data jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,provider_id),
    FOREIGN KEY (organization_id,provider_id) REFERENCES public.seo_providers (organization_id,id) ON DELETE CASCADE
);
CREATE TABLE public.seo_content_model_calls (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    provider_id uuid NOT NULL,
    job_id uuid NOT NULL,
    lease_token uuid NOT NULL,
    step text NOT NULL,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint)=32),
    state text NOT NULL CHECK (state IN ('reserved','succeeded','failed','uncertain')),
    reserved_tokens bigint NOT NULL CHECK (reserved_tokens>0),
    reserved_micros bigint NOT NULL CHECK (reserved_micros>=0),
    tokens bigint CHECK (tokens>=0),
    cost_micros bigint CHECK (cost_micros>=0),
    data jsonb,
    error_code text,
    reserved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    settled_at timestamptz,
    FOREIGN KEY (organization_id,job_id,provider_id)
        REFERENCES public.seo_jobs (organization_id,id,provider_id) ON DELETE CASCADE
);
CREATE INDEX seo_content_model_budget ON public.seo_content_model_calls (organization_id,provider_id,reserved_at);
CREATE INDEX seo_content_model_job ON public.seo_content_model_calls (organization_id,job_id,step,fingerprint);
CREATE UNIQUE INDEX seo_content_model_active ON public.seo_content_model_calls (organization_id,job_id,step)
    WHERE state='reserved';

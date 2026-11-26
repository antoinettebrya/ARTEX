ALTER TABLE public.seo_jobs DROP CONSTRAINT seo_jobs_kind_check,
    ADD CONSTRAINT seo_jobs_kind_check CHECK (kind IN (
        'site_inventory','site_brief','keyword_discovery','keyword_metrics','keyword_suggestions',
        'keyword_intent','keyword_clustering','serp_analysis','competitor_analysis','keyword_url_mapping',
        'opportunity_scoring','page_extraction','content_strategy','content_brief','content_research',
        'content_outline','content_draft','content_fact_check','content_quality_review',
        'content_seo_optimization','content_refresh','content_publish_verify','cms_connection_test',
        'cms_page_sync','cms_media_upload','cms_publication','cms_publication_verify','gsc_sync',
        'ga4_sync','bing_sync','sitemap_refresh','indexnow_submit','url_inspection','pdf_extraction','browser_extraction'
    ));

CREATE TABLE public.seo_cms_connections (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('wordpress','generic','gsc','ga4','bing','indexnow')),
    configured_site_url text NOT NULL,
    name text NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    settings jsonb NOT NULL,
    credential bytea,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,site_id,id),
    UNIQUE (organization_id,site_id,name),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_cms_capabilities (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_cms_pages (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    resource text NOT NULL,
    remote_id text NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,resource,remote_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_cms_publications (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    package_id uuid NOT NULL,
    project_id uuid NOT NULL,
    request jsonb NOT NULL,
    article jsonb NOT NULL,
    idempotency_key text NOT NULL,
    status text NOT NULL CHECK (status IN ('PENDING','PREPARING','DRAFT_CREATED','SCHEDULED',
        'PUBLISHED','VERIFYING','VERIFIED','FAILED','RETRY_WAIT','CANCELLED','ROLLBACK_REQUIRED','ROLLED_BACK')),
    target_id text NOT NULL DEFAULT '',
    draft_id text NOT NULL DEFAULT '',
    url text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    scheduled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,site_id,id),
    UNIQUE (organization_id,idempotency_key),
    UNIQUE (organization_id,site_id,connection_id,package_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,project_id)
        REFERENCES public.seo_content_projects (organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (package_id) REFERENCES public.seo_content_publications (id) ON DELETE CASCADE
);

CREATE TABLE public.seo_execution_jobs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    job_id uuid NOT NULL,
    connection_id uuid,
    connection_version bigint,
    publication_id uuid,
    request jsonb NOT NULL,
    prior_status text,
    started_write boolean NOT NULL DEFAULT false,
    applied_result jsonb,
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_jobs (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,publication_id)
        REFERENCES public.seo_cms_publications (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_execution_jobs_publication ON public.seo_execution_jobs (organization_id,site_id,publication_id,job_id);

CREATE TABLE public.seo_cms_publication_versions (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    publication_id uuid NOT NULL,
    job_id uuid NOT NULL,
    slot text NOT NULL CHECK (slot IN ('before','after','public')),
    data jsonb NOT NULL,
    raw_encrypted bytea,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,job_id,slot),
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_execution_jobs (organization_id,job_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,publication_id)
        REFERENCES public.seo_cms_publications (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_cms_publication_events (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    publication_id uuid NOT NULL,
    status text NOT NULL,
    action text NOT NULL,
    note text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,site_id,publication_id)
        REFERENCES public.seo_cms_publications (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_cms_publication_events_recent ON public.seo_cms_publication_events (organization_id,site_id,publication_id,created_at DESC);

CREATE TABLE public.seo_cms_media (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    remote_id text NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,remote_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_execution_request_calls (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid,
    job_id uuid,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_jobs (organization_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_execution_request_calls_quota ON public.seo_execution_request_calls (organization_id,site_id,connection_id,requested_at);

CREATE TABLE public.seo_execution_oauth_states (
    state_hash bytea PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    connection_version bigint NOT NULL,
    verifier bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_indexing_records (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    url_hash bytea NOT NULL,
    provider text NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id,site_id,url_hash,provider),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_indexing_submissions (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    job_id uuid NOT NULL,
    provider text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_execution_jobs (organization_id,job_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_sitemap_jobs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    job_id uuid NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_execution_jobs (organization_id,job_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_execution_sitemaps (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    provider text NOT NULL,
    url_hash bytea NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,provider,url_hash),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_page_extraction_jobs (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    job_id uuid NOT NULL,
    data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,job_id),
    FOREIGN KEY (organization_id,site_id) REFERENCES public.seo_sites (organization_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,job_id) REFERENCES public.seo_execution_jobs (organization_id,job_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_gsc_properties (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    property_id text NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,property_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_gsc_daily_metrics (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    day date NOT NULL,
    dimension_hash bytea NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,day,dimension_hash),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_gsc_daily_metrics_lookup ON public.seo_gsc_daily_metrics (organization_id,site_id,day DESC);

CREATE TABLE public.seo_ga4_properties (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    property_id text NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,property_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_ga4_daily_metrics (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    day date NOT NULL,
    dimension_hash bytea NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,day,dimension_hash),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_ga4_daily_metrics_lookup ON public.seo_ga4_daily_metrics (organization_id,site_id,day DESC);

CREATE TABLE public.seo_bing_properties (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    property_id text NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,property_id),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);

CREATE TABLE public.seo_bing_daily_metrics (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    day date NOT NULL,
    dimension_hash bytea NOT NULL,
    data jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id,site_id,connection_id,day,dimension_hash),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections (organization_id,site_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_bing_daily_metrics_lookup ON public.seo_bing_daily_metrics (organization_id,site_id,day DESC);

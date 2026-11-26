ALTER TABLE public.seo_providers
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'openai' CHECK (kind IN ('openai', 'dataforseo')),
    ADD COLUMN daily_cost_micros BIGINT CHECK (daily_cost_micros BETWEEN 1 AND 100000000000),
    DROP CONSTRAINT seo_providers_max_output_tokens_check,
    ADD CONSTRAINT seo_providers_max_output_tokens_check CHECK (
        (kind = 'openai' AND max_output_tokens BETWEEN 16 AND 4096) OR
        (kind = 'dataforseo' AND max_output_tokens = 0)
    ),
    ADD CONSTRAINT seo_research_provider_cost_policy CHECK ((kind = 'dataforseo') = (daily_cost_micros IS NOT NULL));

ALTER TABLE public.seo_jobs
    DROP CONSTRAINT seo_jobs_kind_check,
    DROP CONSTRAINT seo_jobs_check,
    ADD COLUMN priority INT NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 100),
    ADD CONSTRAINT seo_jobs_org_job_unique UNIQUE (organization_id, id),
    ADD CONSTRAINT seo_jobs_kind_check CHECK (kind IN (
        'site_inventory', 'site_brief', 'keyword_discovery', 'keyword_metrics',
        'keyword_suggestions', 'keyword_intent', 'keyword_clustering', 'serp_analysis',
        'competitor_analysis', 'keyword_url_mapping', 'opportunity_scoring'
    )),
    ADD CONSTRAINT seo_jobs_kind_provider_check CHECK (
        kind = 'keyword_intent'
        OR ((provider_id IS NOT NULL) = (kind IN (
            'site_brief', 'keyword_discovery', 'keyword_metrics', 'keyword_suggestions',
            'serp_analysis', 'competitor_analysis'
        )))
    );

CREATE INDEX seo_jobs_research_priority_idx ON public.seo_jobs (priority DESC, available_at, id) WHERE state = 'queued';

-- Market dimensions are shared by all research evidence. Rows refer to both the
-- organization and market so a foreign market cannot be attached to local data.
CREATE TABLE public.seo_research_markets (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES public.seo_organizations (id) ON DELETE CASCADE,
    country_code TEXT NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    location_code INT NOT NULL CHECK (location_code > 0),
    language_code TEXT NOT NULL CHECK (char_length(language_code) BETWEEN 2 AND 20),
    search_engine TEXT NOT NULL CHECK (search_engine IN ('google', 'bing')),
    device TEXT NOT NULL CHECK (device IN ('desktop', 'mobile')),
    provider TEXT NOT NULL DEFAULT 'dataforseo' CHECK (provider = 'dataforseo'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, country_code, location_code, language_code, search_engine, device, provider)
);

CREATE TABLE public.seo_keywords (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES public.seo_organizations (id) ON DELETE CASCADE,
    normalized TEXT NOT NULL CHECK (octet_length(normalized) BETWEEN 1 AND 400),
    keyword TEXT NOT NULL CHECK (octet_length(keyword) BETWEEN 1 AND 400),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, normalized)
);

CREATE TABLE public.seo_research_site_keywords (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, keyword_id),
    FOREIGN KEY (organization_id, site_id) REFERENCES public.seo_sites (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, keyword_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_keyword_seeds (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, keyword_id),
    FOREIGN KEY (organization_id, site_id, market_id, keyword_id)
        REFERENCES public.seo_research_site_keywords (organization_id, site_id, market_id, keyword_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_research_jobs (
    organization_id UUID NOT NULL,
    job_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    request JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, job_id),
    FOREIGN KEY (organization_id, job_id) REFERENCES public.seo_jobs (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, keyword_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE
);

-- One record per actual or potentially billable attempt. A cache hit points to
-- the original call and snapshots without reserving or spending again.
CREATE TABLE public.seo_research_calls (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    provider_id UUID NOT NULL,
    job_id UUID NOT NULL,
    market_id UUID NOT NULL,
    lease_token UUID NOT NULL,
    operation TEXT NOT NULL,
    fingerprint BYTEA NOT NULL CHECK (octet_length(fingerprint) = 32),
    state TEXT NOT NULL CHECK (state IN ('reserved', 'succeeded', 'failed', 'uncertain')),
    reserved_micros BIGINT NOT NULL CHECK (reserved_micros > 0),
    cost_micros BIGINT CHECK (cost_micros >= 0),
    reserved_at TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    collected_at TIMESTAMPTZ,
    provider_task_id TEXT,
    source TEXT,
    error_code TEXT,
    raw_response BYTEA,
    parsed_response JSONB,
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, job_id, provider_id) REFERENCES public.seo_jobs (organization_id, id, provider_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE
);

CREATE INDEX seo_research_calls_budget_idx
    ON public.seo_research_calls (organization_id, provider_id, reserved_at);
CREATE INDEX seo_research_calls_reuse_idx
    ON public.seo_research_calls (organization_id, fingerprint, expires_at DESC) WHERE state = 'succeeded';
CREATE UNIQUE INDEX seo_research_calls_inflight_idx
    ON public.seo_research_calls (organization_id, fingerprint) WHERE state = 'reserved';

CREATE TABLE public.seo_keyword_metrics (
    organization_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    source TEXT NOT NULL,
    snapshot_id UUID NOT NULL,
    metrics JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, market_id, keyword_id, source),
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, keyword_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, snapshot_id) REFERENCES public.seo_research_calls (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_keyword_suggestions (
    organization_id UUID NOT NULL,
    market_id UUID NOT NULL,
    seed_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    operation TEXT NOT NULL,
    snapshot_id UUID NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, market_id, seed_id, keyword_id, operation),
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, seed_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, keyword_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, snapshot_id) REFERENCES public.seo_research_calls (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_serp_snapshots (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    source TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, id) REFERENCES public.seo_research_calls (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, keyword_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE
);

CREATE INDEX seo_serp_snapshots_lookup_idx
    ON public.seo_serp_snapshots (organization_id, market_id, keyword_id, collected_at DESC);

CREATE TABLE public.seo_serp_results (
    organization_id UUID NOT NULL,
    snapshot_id UUID NOT NULL,
    rank INT NOT NULL CHECK (rank BETWEEN 1 AND 10),
    url TEXT NOT NULL,
    result JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, snapshot_id, rank),
    FOREIGN KEY (organization_id, snapshot_id) REFERENCES public.seo_serp_snapshots (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_competitor_pages (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    market_id UUID NOT NULL,
    url TEXT NOT NULL CHECK (octet_length(url) <= 4096),
    url_digest BYTEA NOT NULL CHECK (octet_length(url_digest) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, market_id, url_digest),
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_competitor_page_snapshots (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    page_id UUID NOT NULL,
    source TEXT NOT NULL,
    page JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, id) REFERENCES public.seo_research_calls (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, page_id) REFERENCES public.seo_competitor_pages (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_research_competitors (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    page_id UUID NOT NULL,
    snapshot_id UUID NOT NULL,
    gaps JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, keyword_id, page_id),
    FOREIGN KEY (organization_id, site_id, market_id, keyword_id)
        REFERENCES public.seo_research_site_keywords (organization_id, site_id, market_id, keyword_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, page_id) REFERENCES public.seo_competitor_pages (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, snapshot_id) REFERENCES public.seo_competitor_page_snapshots (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_research_site_pages (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    url_digest BYTEA NOT NULL CHECK (octet_length(url_digest) = 32),
    page JSONB NOT NULL,
    source TEXT NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, site_id, market_id, url_digest),
    FOREIGN KEY (organization_id, site_id) REFERENCES public.seo_sites (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_search_intents (
    organization_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    intent JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, market_id, keyword_id),
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, keyword_id) REFERENCES public.seo_keywords (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_keyword_clusters (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    cluster_key TEXT NOT NULL,
    cluster JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, cluster_key),
    FOREIGN KEY (organization_id, site_id) REFERENCES public.seo_sites (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, market_id) REFERENCES public.seo_research_markets (organization_id, id) ON DELETE CASCADE
);

CREATE TABLE public.seo_keyword_cluster_members (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    cluster_key TEXT NOT NULL,
    keyword_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, cluster_key, keyword_id),
    FOREIGN KEY (organization_id, site_id, market_id, cluster_key)
        REFERENCES public.seo_keyword_clusters (organization_id, site_id, market_id, cluster_key) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, site_id, market_id, keyword_id)
        REFERENCES public.seo_research_site_keywords (organization_id, site_id, market_id, keyword_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_keyword_url_mappings (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    mapping JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, keyword_id),
    FOREIGN KEY (organization_id, site_id, market_id, keyword_id)
        REFERENCES public.seo_research_site_keywords (organization_id, site_id, market_id, keyword_id) ON DELETE CASCADE
);

CREATE TABLE public.seo_keyword_opportunities (
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    market_id UUID NOT NULL,
    keyword_id UUID NOT NULL,
    opportunity JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, site_id, market_id, keyword_id),
    FOREIGN KEY (organization_id, site_id, market_id, keyword_id)
        REFERENCES public.seo_research_site_keywords (organization_id, site_id, market_id, keyword_id) ON DELETE CASCADE
);

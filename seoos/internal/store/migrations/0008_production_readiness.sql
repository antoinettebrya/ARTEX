CREATE TABLE public.seo_gsc_daily_snapshots (
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    connection_version bigint NOT NULL,
    job_id uuid NOT NULL,
    day date NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'complete', 'truncated', 'failed')),
    totals_checked boolean NOT NULL DEFAULT false,
    totals jsonb,
    next_row integer NOT NULL DEFAULT 0 CHECK (next_row BETWEEN 0 AND 50000),
    rows_collected integer NOT NULL DEFAULT 0,
    error_code text,
    started_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, job_id, day),
    FOREIGN KEY (organization_id, job_id)
        REFERENCES public.seo_execution_jobs (organization_id, job_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, site_id, connection_id)
        REFERENCES public.seo_cms_connections (organization_id, site_id, id) ON DELETE CASCADE
);
CREATE INDEX seo_gsc_snapshots_latest ON public.seo_gsc_daily_snapshots
    (organization_id, site_id, connection_id, connection_version, day, started_at DESC, job_id DESC);

CREATE TABLE public.seo_gsc_snapshot_rows (
    organization_id uuid NOT NULL,
    job_id uuid NOT NULL,
    day date NOT NULL,
    dimension_hash bytea NOT NULL,
    data jsonb NOT NULL,
    PRIMARY KEY (organization_id, job_id, day, dimension_hash),
    FOREIGN KEY (organization_id, job_id, day)
        REFERENCES public.seo_gsc_daily_snapshots (organization_id, job_id, day) ON DELETE CASCADE
);

CREATE INDEX seo_jobs_site_history ON public.seo_jobs (organization_id, site_id, created_at DESC, id DESC);
CREATE INDEX seo_jobs_state_history ON public.seo_jobs (organization_id, state, created_at DESC, id DESC);

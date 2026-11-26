CREATE TABLE public.seo_providers (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES public.seo_organizations (id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    model TEXT NOT NULL CHECK (char_length(model) BETWEEN 1 AND 100),
    credential BYTEA NOT NULL,
    timeout_seconds INT NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 30),
    requests_per_minute INT NOT NULL CHECK (requests_per_minute BETWEEN 1 AND 60),
    requests_per_day INT NOT NULL CHECK (requests_per_day BETWEEN 1 AND 10000),
    max_output_tokens INT NOT NULL CHECK (max_output_tokens BETWEEN 16 AND 4096),
    enabled BOOLEAN NOT NULL,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, name)
);

CREATE TABLE public.seo_jobs (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    site_id UUID NOT NULL,
    provider_id UUID,
    kind TEXT NOT NULL CHECK (kind IN ('site_inventory', 'site_brief')),
    idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 120),
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    available_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    error_code TEXT,
    result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, idempotency_key),
    UNIQUE (organization_id, id, provider_id),
    FOREIGN KEY (organization_id, site_id) REFERENCES public.seo_sites (organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, provider_id) REFERENCES public.seo_providers (organization_id, id),
    CHECK ((kind = 'site_brief') = (provider_id IS NOT NULL)),
    CHECK ((state = 'running' AND lease_token IS NOT NULL AND lease_until IS NOT NULL)
        OR (state <> 'running' AND lease_token IS NULL AND lease_until IS NULL)),
    CHECK ((state = 'succeeded') = (result IS NOT NULL))
);

CREATE INDEX seo_jobs_ready_idx ON public.seo_jobs (available_at, id) WHERE state = 'queued';
CREATE INDEX seo_jobs_expired_idx ON public.seo_jobs (lease_until, id) WHERE state = 'running';
CREATE INDEX seo_jobs_org_created_idx ON public.seo_jobs (organization_id, created_at DESC, id DESC);

CREATE TABLE public.seo_provider_calls (
    organization_id UUID NOT NULL,
    provider_id UUID NOT NULL,
    lease_token UUID PRIMARY KEY,
    job_id UUID NOT NULL,
    reserved_at TIMESTAMPTZ NOT NULL,
    output_token_limit INT NOT NULL CHECK (output_token_limit > 0),
    FOREIGN KEY (organization_id, job_id, provider_id) REFERENCES public.seo_jobs (organization_id, id, provider_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, provider_id) REFERENCES public.seo_providers (organization_id, id) ON DELETE CASCADE
);

CREATE INDEX seo_provider_calls_budget_idx ON public.seo_provider_calls (organization_id, provider_id, reserved_at);

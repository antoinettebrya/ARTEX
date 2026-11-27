CREATE TABLE public.seo_integration_applications (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.seo_organizations(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('google','bing')),
    label text NOT NULL,
    client_id text NOT NULL,
    encrypted_client_secret bytea,
    redirect_uri text NOT NULL,
    allowed_scopes text[] NOT NULL,
    config_version bigint NOT NULL DEFAULT 1 CHECK (config_version>0),
    enabled boolean NOT NULL DEFAULT true,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,id,provider)
);
CREATE INDEX seo_integration_applications_org ON public.seo_integration_applications (organization_id,created_at DESC,id);

CREATE TABLE public.seo_integration_accounts (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.seo_organizations(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('google','bing')),
    application_id uuid,
    auth_mode text NOT NULL CHECK (auth_mode IN ('oauth','api_key')),
    label text NOT NULL,
    granted_scopes text[] NOT NULL DEFAULT '{}',
    encrypted_credentials bytea,
    credential_version bigint NOT NULL DEFAULT 1 CHECK (credential_version>0),
    expires_at timestamptz,
    last_refresh_at timestamptz,
    authorization_status text NOT NULL DEFAULT 'REAUTH_REQUIRED'
        CHECK (authorization_status IN ('CONNECTED','EXPIRED','REVOKED','ERROR','REAUTH_REQUIRED')),
    requests_per_minute integer NOT NULL DEFAULT 60 CHECK (requests_per_minute BETWEEN 1 AND 120),
    requests_per_day integer NOT NULL DEFAULT 1000 CHECK (requests_per_day BETWEEN 1 AND 25000),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((auth_mode='oauth' AND application_id IS NOT NULL) OR
           (auth_mode='api_key' AND provider='bing' AND application_id IS NULL)),
    UNIQUE (organization_id,id),
    UNIQUE (organization_id,id,provider),
    FOREIGN KEY (organization_id,application_id,provider)
        REFERENCES public.seo_integration_applications(organization_id,id,provider)
);
CREATE INDEX seo_integration_accounts_org ON public.seo_integration_accounts (organization_id,created_at DESC,id);
CREATE INDEX seo_integration_accounts_app ON public.seo_integration_accounts (organization_id,application_id) WHERE deleted_at IS NULL;

-- The binding and its existing execution connection share an ID. The legacy
-- row remains the routing and observation anchor while new secrets live here.
CREATE TABLE public.seo_site_integration_bindings (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    integration_account_id uuid,
    provider text NOT NULL CHECK (provider IN ('gsc','ga4','bing','indexnow')),
    account_provider text GENERATED ALWAYS AS
        (CASE WHEN provider IN ('gsc','ga4') THEN 'google' WHEN provider='bing' THEN 'bing' END) STORED,
    resource_type text GENERATED ALWAYS AS
        (CASE provider WHEN 'gsc' THEN 'GSC_PROPERTY' WHEN 'ga4' THEN 'GA4_PROPERTY'
         WHEN 'bing' THEN 'BING_SITE' ELSE 'INDEXNOW_HOST' END) STORED,
    external_resource_id text NOT NULL,
    resource_identity text NOT NULL,
    resource jsonb NOT NULL DEFAULT '{}',
    enabled boolean NOT NULL DEFAULT true,
    inherited boolean NOT NULL DEFAULT false,
    last_verified_at timestamptz,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((provider='indexnow')=(integration_account_id IS NULL)),
    UNIQUE (organization_id,id),
    UNIQUE (organization_id,site_id,id),
    FOREIGN KEY (organization_id,site_id,id)
        REFERENCES public.seo_cms_connections(organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,integration_account_id,account_provider)
        REFERENCES public.seo_integration_accounts(organization_id,id,provider)
);
CREATE UNIQUE INDEX seo_integration_resource_once ON public.seo_site_integration_bindings (organization_id,provider,resource_identity)
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX seo_integration_site_provider_once ON public.seo_site_integration_bindings (organization_id,site_id,provider)
    WHERE deleted_at IS NULL;
CREATE INDEX seo_integration_bindings_account ON public.seo_site_integration_bindings(organization_id,integration_account_id,site_id);

CREATE TABLE public.seo_indexnow_site_keys (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    host text NOT NULL,
    encrypted_key bytea,
    directory text NOT NULL DEFAULT '/',
    key_version bigint NOT NULL DEFAULT 1 CHECK (key_version>0),
    verification_status text NOT NULL DEFAULT 'PENDING' CHECK (verification_status IN ('PENDING','VERIFIED','FAILED')),
    verification_error text,
    verified_at timestamptz,
    last_submission_at timestamptz,
    legacy_key boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,site_id,id) REFERENCES public.seo_site_integration_bindings(organization_id,site_id,id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX seo_indexnow_one_active_host ON public.seo_indexnow_site_keys(organization_id,host) WHERE encrypted_key IS NOT NULL;

CREATE TABLE public.seo_integration_oauth_states (
    state_hash bytea PRIMARY KEY,
    organization_id uuid NOT NULL,
    account_id uuid NOT NULL,
    account_version bigint NOT NULL,
    config_version bigint NOT NULL,
    provider text NOT NULL,
    redirect_uri text NOT NULL,
    actor_key text NOT NULL,
    requested_scopes text[] NOT NULL,
    verifier bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    FOREIGN KEY (organization_id,account_id,provider) REFERENCES public.seo_integration_accounts(organization_id,id,provider) ON DELETE CASCADE
);
CREATE INDEX seo_integration_oauth_states_expiry ON public.seo_integration_oauth_states(expires_at);
CREATE INDEX seo_integration_oauth_states_account ON public.seo_integration_oauth_states(organization_id,account_id);

CREATE TABLE public.seo_integration_account_calls (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id uuid NOT NULL,
    account_id uuid NOT NULL,
    operation text NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    diagnostic jsonb,
    FOREIGN KEY (organization_id,account_id) REFERENCES public.seo_integration_accounts(organization_id,id) ON DELETE CASCADE
);
CREATE INDEX seo_integration_calls_account ON public.seo_integration_account_calls(organization_id,account_id,requested_at DESC);

ALTER TABLE public.seo_execution_request_calls
    ADD COLUMN integration_account_id uuid,
    ADD COLUMN operation text,
    ADD CONSTRAINT seo_execution_request_account_fk FOREIGN KEY (organization_id,integration_account_id)
        REFERENCES public.seo_integration_accounts(organization_id,id);
CREATE INDEX seo_execution_request_account_quota ON public.seo_execution_request_calls(organization_id,integration_account_id,requested_at DESC)
    WHERE integration_account_id IS NOT NULL;
CREATE INDEX seo_execution_request_inspection_quota ON public.seo_execution_request_calls(organization_id,site_id,requested_at DESC)
    WHERE operation='gsc_url_inspection';
CREATE INDEX seo_indexing_submission_urls ON public.seo_indexing_submissions USING gin ((data->'urls'));

ALTER TABLE public.seo_execution_jobs
    ADD COLUMN integration_account_id uuid,
    ADD COLUMN integration_account_version bigint,
    ADD CONSTRAINT seo_execution_job_account_fk FOREIGN KEY (organization_id,integration_account_id)
        REFERENCES public.seo_integration_accounts(organization_id,id),
    ADD CHECK ((integration_account_id IS NULL)=(integration_account_version IS NULL));
CREATE INDEX seo_integration_sync_job_lookup ON public.seo_execution_jobs (organization_id,connection_id,job_id);

-- Keep the durable queue as the only source of truth for sync run status.
CREATE VIEW public.seo_integration_sync_runs WITH (security_invoker=true) AS
SELECT j.organization_id,j.id AS id,b.id AS binding_id,b.site_id,b.provider,b.integration_account_id AS account_id,
       j.kind AS operation,j.state AS status,j.error_code,e.request->>'from' AS requested_from,
       e.request->>'to' AS requested_to,j.result AS result_summary,
       coalesce((j.result->>'truncated')::boolean,false) AS truncated,
       j.created_at,j.available_at,j.attempts,
       CASE WHEN j.state IN ('succeeded','failed','cancelled') THEN j.updated_at END AS completed_at
FROM public.seo_site_integration_bindings b
JOIN public.seo_execution_jobs e ON (e.organization_id,e.site_id,e.connection_id)=(b.organization_id,b.site_id,b.id)
JOIN public.seo_jobs j ON (j.organization_id,j.id)=(e.organization_id,e.job_id);

CREATE INDEX seo_integration_binding_requests ON public.seo_execution_request_calls
    (organization_id,site_id,connection_id,requested_at DESC) WHERE connection_id IS NOT NULL;
CREATE INDEX seo_integration_requests_job ON public.seo_execution_request_calls
    (organization_id,job_id,requested_at) WHERE job_id IS NOT NULL;

CREATE VIEW public.seo_integration_health WITH (security_invoker=true) AS
SELECT b.organization_id,b.id AS binding_id,b.site_id,b.provider,b.integration_account_id,
       CASE WHEN a.id IS NOT NULL AND a.authorization_status<>'CONNECTED' THEN a.authorization_status
            WHEN NOT b.enabled THEN 'DISABLED'
            WHEN s.status<>'active' OR c.configured_site_url<>s.base_url THEN 'SITE_CHANGED'
            WHEN a.auth_mode='oauth' AND NOT coalesce(p.enabled,false) THEN 'REAUTH_REQUIRED'
            WHEN k.id IS NOT NULL AND k.verification_status<>'VERIFIED' THEN k.verification_status
            WHEN coalesce(last_failure.requested_at,'-infinity')>coalesce(last_success.requested_at,'-infinity') THEN 'ERROR'
            WHEN last_success.requested_at IS NULL THEN 'UNVERIFIED'
            ELSE 'HEALTHY' END AS status,
       last_success.requested_at AS last_success_at,last_failure.requested_at AS last_failure_at,
       last_failure.diagnostic->>'code' AS last_error_code,
       latest.diagnostic->>'http_status' AS last_http_status,
       latest.diagnostic->>'duration_ms' AS latency_ms,
       latest.requested_at AS checked_at,b.last_verified_at,
       pending.available_at AS next_sync_at,last_sync.completed_at AS last_sync_at,
       c.settings->'requests_per_day' AS requests_per_day,c.settings->'requests_per_minute' AS requests_per_minute,
       coalesce(today.requests,0) AS daily_requests
FROM public.seo_site_integration_bindings b
JOIN public.seo_sites s ON (s.organization_id,s.id)=(b.organization_id,b.site_id)
JOIN public.seo_cms_connections c ON (c.organization_id,c.site_id,c.id)=(b.organization_id,b.site_id,b.id)
LEFT JOIN public.seo_indexnow_site_keys k ON (k.organization_id,k.site_id,k.id)=(b.organization_id,b.site_id,b.id)
LEFT JOIN public.seo_integration_accounts a ON (a.organization_id,a.id)=(b.organization_id,b.integration_account_id)
LEFT JOIN public.seo_integration_applications p ON (p.organization_id,p.id)=(a.organization_id,a.application_id)
LEFT JOIN LATERAL (
    SELECT r.requested_at,r.diagnostic FROM public.seo_execution_request_calls r
    WHERE (r.organization_id,r.site_id,r.connection_id)=(b.organization_id,b.site_id,b.id)
    AND r.diagnostic IS NOT NULL ORDER BY r.requested_at DESC,r.id DESC LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
    SELECT r.requested_at FROM public.seo_execution_request_calls r
    WHERE (r.organization_id,r.site_id,r.connection_id)=(b.organization_id,b.site_id,b.id)
    AND r.diagnostic->>'category'='success' ORDER BY r.requested_at DESC,r.id DESC LIMIT 1
) last_success ON true
LEFT JOIN LATERAL (
    SELECT r.requested_at,r.diagnostic FROM public.seo_execution_request_calls r
    WHERE (r.organization_id,r.site_id,r.connection_id)=(b.organization_id,b.site_id,b.id)
    AND coalesce(r.diagnostic->>'category','success')<>'success' ORDER BY r.requested_at DESC,r.id DESC LIMIT 1
) last_failure ON true
LEFT JOIN LATERAL (
    SELECT r.available_at FROM public.seo_integration_sync_runs r
    WHERE r.organization_id=b.organization_id AND r.binding_id=b.id
    AND r.status='queued' ORDER BY r.available_at LIMIT 1
) pending ON true
LEFT JOIN LATERAL (
    SELECT r.completed_at FROM public.seo_integration_sync_runs r
    WHERE r.organization_id=b.organization_id AND r.binding_id=b.id AND r.status='succeeded'
    AND r.operation IN ('gsc_sync','ga4_sync','bing_sync','indexnow_submit')
    ORDER BY r.completed_at DESC LIMIT 1
) last_sync ON true
LEFT JOIN LATERAL (
    SELECT count(*) AS requests FROM public.seo_execution_request_calls r
    WHERE (r.organization_id,r.site_id,r.connection_id)=(b.organization_id,b.site_id,b.id)
    AND r.requested_at>=date_trunc('day',statement_timestamp(),'UTC')
) today ON true
WHERE b.deleted_at IS NULL;

ALTER TABLE public.seo_gsc_daily_snapshots
    ADD COLUMN search_type text NOT NULL DEFAULT 'web' CHECK (search_type IN ('web','image','video','news','discover','googleNews')),
    ADD COLUMN data_state text NOT NULL DEFAULT 'final' CHECK (data_state IN ('final','all')),
    ADD COLUMN first_incomplete_date date CHECK (data_state='all' OR first_incomplete_date IS NULL);
ALTER TABLE public.seo_gsc_daily_metrics
    ADD COLUMN search_type text NOT NULL DEFAULT 'web' CHECK (search_type IN ('web','image','video','news','discover','googleNews')),
    ADD COLUMN data_state text NOT NULL DEFAULT 'final' CHECK (data_state IN ('final','all')),
    DROP CONSTRAINT seo_gsc_daily_metrics_pkey,
    ADD PRIMARY KEY (organization_id,site_id,connection_id,day,search_type,data_state,dimension_hash);
CREATE INDEX seo_gsc_daily_series_snapshot ON public.seo_gsc_daily_snapshots
    (organization_id,site_id,connection_id,connection_version,search_type,data_state,day,started_at DESC,job_id DESC);

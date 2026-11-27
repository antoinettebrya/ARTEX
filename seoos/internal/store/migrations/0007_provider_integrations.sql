ALTER TABLE public.seo_providers
    ADD COLUMN model_options jsonb CHECK (model_options IS NULL OR jsonb_typeof(model_options)='object'),
    DROP CONSTRAINT seo_providers_kind_check,
    ADD CONSTRAINT seo_providers_kind_check
        CHECK (kind IN ('openai','anthropic','gemini','deepseek','openai_compatible','dataforseo'));

ALTER TABLE public.seo_content_model_calls ADD COLUMN diagnostic jsonb;
ALTER TABLE public.seo_research_calls ADD COLUMN diagnostic jsonb;
CREATE INDEX seo_research_calls_uncertain_idx ON public.seo_research_calls (organization_id,fingerprint,reserved_at DESC)
    WHERE state='uncertain';
ALTER TABLE public.seo_provider_calls
    ADD COLUMN diagnostic jsonb,
    ADD COLUMN outcome jsonb,
    ADD COLUMN state text NOT NULL DEFAULT 'uncertain'
        CHECK (state IN ('reserved','uncertain','failed','succeeded'));
CREATE INDEX seo_provider_calls_job_idx ON public.seo_provider_calls (organization_id,job_id,reserved_at DESC);

ALTER TABLE public.seo_execution_request_calls
    ADD COLUMN id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ADD COLUMN diagnostic jsonb;
CREATE INDEX seo_execution_requests_org_day ON public.seo_execution_request_calls (organization_id,requested_at DESC);
CREATE INDEX seo_indexing_submissions_target ON public.seo_indexing_submissions
    (organization_id,site_id,provider,(data->>'url'),created_at DESC);

-- The owner-facing receipt view contains metadata only. Each source retains its
-- own durable reservation and settlement semantics.
CREATE VIEW public.seo_provider_receipts WITH (security_invoker=true) AS
SELECT c.organization_id,c.provider_id,c.job_id,c.id::text AS call_id,
       c.reserved_at AS occurred_at,c.state,c.reserved_micros,
       COALESCE(c.diagnostic,jsonb_build_object(
           'provider',p.kind,'operation',p.model,'category',c.state,'code',c.error_code,
           'cost_micros',c.cost_micros,'cost_source',CASE WHEN c.cost_micros IS NULL THEN 'unknown' ELSE 'estimated' END,
           'may_be_charged',c.cost_micros IS NULL OR c.cost_micros>0)) AS diagnostic
FROM public.seo_content_model_calls c JOIN public.seo_providers p USING(organization_id)
WHERE p.id=c.provider_id
UNION ALL
SELECT c.organization_id,c.provider_id,c.job_id,c.id::text,c.reserved_at,c.state,c.reserved_micros,
       COALESCE(c.diagnostic,jsonb_build_object(
           'provider','dataforseo','operation',c.operation,'category',c.state,'code',c.error_code,
           'request_id',c.provider_task_id,'cost_micros',c.cost_micros,
           'cost_source',CASE WHEN c.cost_micros IS NULL THEN 'unknown' ELSE 'provider' END,
           'may_be_charged',c.cost_micros IS NULL OR c.cost_micros>0))
FROM public.seo_research_calls c
UNION ALL
SELECT c.organization_id,c.provider_id,c.job_id,c.lease_token::text,c.reserved_at,c.state,0::bigint,
       COALESCE(c.diagnostic,jsonb_build_object('provider',p.kind,'operation',p.model,
           'category',c.state,'cost_source','unknown','may_be_charged',true))
FROM public.seo_provider_calls c JOIN public.seo_providers p USING(organization_id)
WHERE p.id=c.provider_id
UNION ALL
SELECT c.organization_id,c.connection_id,c.job_id,'execution-'||c.id,c.requested_at,
       CASE WHEN c.diagnostic IS NULL THEN 'unsettled' ELSE 'settled' END,0::bigint,
       COALESCE(c.diagnostic,jsonb_build_object('provider',COALESCE(p.kind,'site'),'operation','request',
           'category','unsettled','cost_source','no_metered_fee','cost_micros',0,'may_be_charged',false))
FROM public.seo_execution_request_calls c
LEFT JOIN public.seo_cms_connections p ON (p.organization_id,p.site_id,p.id)=(c.organization_id,c.site_id,c.connection_id)
UNION ALL
SELECT c.organization_id,c.resource_id,NULL::uuid,'test-'||c.id,c.created_at,'settled',0::bigint,c.details->'diagnostic'
FROM public.seo_audit_events c
WHERE c.action='provider.test_result' AND c.details->'diagnostic' IS NOT NULL;

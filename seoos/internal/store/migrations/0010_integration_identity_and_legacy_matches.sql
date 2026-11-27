ALTER TABLE public.seo_integration_accounts
    ADD COLUMN verified_subject text,
    ADD COLUMN identity_verified_at timestamptz,
    ADD CHECK ((verified_subject IS NULL)=(identity_verified_at IS NULL)),
    ADD CHECK (verified_subject IS NULL OR
               (provider='google' AND auth_mode='oauth' AND octet_length(verified_subject) BETWEEN 1 AND 255));

-- Google Domain Properties and GA4 Web streams can cover multiple sites.
-- Keep single-resource ownership for the two services that submit URLs.
DROP INDEX public.seo_integration_resource_once;
CREATE UNIQUE INDEX seo_integration_resource_once
    ON public.seo_site_integration_bindings (organization_id,provider,resource_identity)
    WHERE deleted_at IS NULL AND provider IN ('bing','indexnow');

-- The legacy projection and its ciphertext stay in place. This records the
-- account and binding state before each committed migration or reassignment.
CREATE TABLE public.seo_integration_legacy_migration_receipts (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    site_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    previous_account_id uuid,
    target_account_id uuid,
    previous_connection_version bigint NOT NULL,
    previous_binding_enabled boolean,
    previous_target_scopes text[],
    previous_application_scopes text[],
    previous_application_version bigint,
    action text NOT NULL CHECK (action IN ('MIGRATED','REASSIGNED')),
    match_basis text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,site_id,connection_id)
        REFERENCES public.seo_cms_connections(organization_id,site_id,id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id,previous_account_id) REFERENCES public.seo_integration_accounts(organization_id,id),
    FOREIGN KEY (organization_id,target_account_id) REFERENCES public.seo_integration_accounts(organization_id,id)
);
CREATE INDEX seo_integration_legacy_receipts_connection
    ON public.seo_integration_legacy_migration_receipts (organization_id,connection_id,created_at DESC);

CREATE TABLE public.seo_users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE CHECK (char_length(email) BETWEEN 3 AND 254 AND email = lower(email)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE public.seo_memberships (
    organization_id UUID NOT NULL REFERENCES public.seo_organizations (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES public.seo_users (id),
    role TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    token_digest BYTEA UNIQUE CHECK (octet_length(token_digest) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE public.seo_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES public.seo_organizations (id) ON DELETE CASCADE,
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('bootstrap', 'member', 'system', 'worker')),
    actor_user_id UUID REFERENCES public.seo_users (id),
    action TEXT NOT NULL,
    resource_id UUID NOT NULL,
    details JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK ((actor_kind = 'member') = (actor_user_id IS NOT NULL))
);

CREATE INDEX seo_audit_org_id_idx ON public.seo_audit_events (organization_id, id DESC);

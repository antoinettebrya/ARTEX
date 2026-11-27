CREATE TABLE public.seo_organizations (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE public.seo_sites (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    base_url TEXT NOT NULL CHECK (char_length(base_url) BETWEEN 1 AND 2048),
    cms TEXT NOT NULL CHECK (cms IN ('wordpress', 'generic')),
    country_code TEXT NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    language_tag TEXT NOT NULL CHECK (char_length(language_tag) BETWEEN 2 AND 100),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'archived')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT seo_sites_organization_fk FOREIGN KEY (organization_id)
        REFERENCES public.seo_organizations (id),
    CONSTRAINT seo_sites_organization_id_unique UNIQUE (organization_id, id),
    CONSTRAINT seo_sites_organization_url_unique UNIQUE (organization_id, base_url)
);

CREATE INDEX seo_sites_organization_created_idx
    ON public.seo_sites (organization_id, created_at DESC, id DESC);

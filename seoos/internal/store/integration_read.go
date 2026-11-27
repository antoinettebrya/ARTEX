package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func (s *Store) IntegrationBindingSettings(ctx context.Context, org, id string) (domain.IntegrationBindingInput, error) {
	var out domain.IntegrationBindingInput
	if !domain.ValidID(id) {
		return out, domain.ErrNotFound
	}
	err := s.db.QueryRowContext(ctx, `SELECT b.site_id::text,coalesce(b.integration_account_id::text,''),b.provider,b.external_resource_id,b.enabled,
		coalesce((c.settings->>'allow_writes')::boolean,false),c.version,
		coalesce((c.settings->>'requests_per_minute')::int,30),coalesce((c.settings->>'requests_per_day')::int,500)
		FROM public.seo_site_integration_bindings b JOIN public.seo_cms_connections c
		ON (c.organization_id,c.site_id,c.id)=(b.organization_id,b.site_id,b.id)
		WHERE b.organization_id=$1 AND b.id=$2 AND b.deleted_at IS NULL`, org, id).
		Scan(&out.SiteID, &out.AccountID, &out.Kind, &out.ResourceID, &out.Enabled, &out.AllowWrites,
			&out.Version, &out.RequestsPerMinute, &out.RequestsPerDay)
	return out, classifyError(err)
}

const bindingRead = `
	SELECT jsonb_build_object('id',b.id,'site_id',b.site_id,'site_name',s.name,'site_url',s.base_url,'provider',b.provider,
		'resource_type',b.resource_type,'external_resource_id',b.external_resource_id,'resource',b.resource,'integration_account_id',b.integration_account_id,
		'enabled',b.enabled,'inherited',b.inherited,'last_verified_at',b.last_verified_at,'version',c.version,
		'allow_writes',coalesce(c.settings->'allow_writes','false'::jsonb),'requests_per_minute',c.settings->'requests_per_minute',
		'requests_per_day',c.settings->'requests_per_day','status',h.status,'next_sync_at',h.next_sync_at) AS value,
		b.id::text AS id,b.provider AS provider,b.site_id::text AS site,b.integration_account_id::text AS account,h.status,
		s.name||' '||s.base_url||' '||b.external_resource_id AS label,b.updated_at AS observed
	FROM public.seo_site_integration_bindings b
	JOIN public.seo_sites s ON (s.organization_id,s.id)=(b.organization_id,b.site_id)
	JOIN public.seo_cms_connections c ON (c.organization_id,c.site_id,c.id)=(b.organization_id,b.site_id,b.id)
	JOIN public.seo_integration_health h ON (h.organization_id,h.binding_id)=(b.organization_id,b.id)
	WHERE b.organization_id=$1 AND b.deleted_at IS NULL`

func integrationListSource(section string) (string, bool) {
	switch section {
	case "applications":
		return `SELECT ` + integrationAppData + ` AS value,p.id::text AS id,p.provider,'' AS site,'' AS account,
			CASE WHEN p.enabled THEN 'ENABLED' ELSE 'DISABLED' END AS status,p.label,p.updated_at AS observed
			FROM public.seo_integration_applications p WHERE p.organization_id=$1 AND p.deleted_at IS NULL`, true
	case "accounts":
		return `SELECT ` + integrationAccountData + ` || jsonb_build_object('site_count',
			(SELECT count(DISTINCT b.site_id) FROM public.seo_site_integration_bindings b
			WHERE (b.organization_id,b.integration_account_id)=(a.organization_id,a.id) AND b.deleted_at IS NULL)) AS value,
			a.id::text AS id,a.provider,'' AS site,a.id::text AS account,a.authorization_status AS status,a.label,a.updated_at AS observed
			FROM public.seo_integration_accounts a WHERE a.organization_id=$1 AND a.deleted_at IS NULL`, true
	case "bindings":
		return bindingRead, true
	case "sites":
		return `SELECT jsonb_build_object('id',s.id,'site_id',s.id,'site_name',s.name,'site_url',s.base_url,'status',s.status,
			'bindings',coalesce((SELECT jsonb_agg(r.value ORDER BY r.provider) FROM (` + bindingRead + `) r WHERE r.site=s.id::text),'[]'::jsonb)) AS value,
			s.id::text AS id,'' AS provider,s.id::text AS site,'' AS account,
			CASE WHEN EXISTS(SELECT 1 FROM public.seo_integration_health h WHERE h.organization_id=$1 AND h.site_id=s.id
				AND h.status IN ('ERROR','FAILED','REAUTH_REQUIRED','REVOKED','SITE_CHANGED')) THEN 'ISSUES'
				WHEN EXISTS(SELECT 1 FROM public.seo_site_integration_bindings b WHERE b.organization_id=$1
					AND b.site_id=s.id AND b.deleted_at IS NULL) THEN 'BOUND' ELSE 'UNBOUND' END AS status,
			s.name||' '||s.base_url AS label,s.updated_at AS observed
			FROM public.seo_sites s WHERE s.organization_id=$1`, true
	case "keys":
		return `SELECT jsonb_build_object('id',k.id,'site_id',k.site_id,'site_name',s.name,'host',k.host,'directory',k.directory,
			'key_version',k.key_version,'version',c.version,'allow_writes',coalesce(c.settings->'allow_writes','false'::jsonb),
			'verification_status',k.verification_status,'verification_error',k.verification_error,'verified_at',k.verified_at,
			'last_submission_at',k.last_submission_at,'legacy_key',k.legacy_key) AS value,
			k.id::text AS id,'indexnow' AS provider,k.site_id::text AS site,'' AS account,k.verification_status AS status,
			k.host AS label,k.updated_at AS observed FROM public.seo_indexnow_site_keys k
			JOIN public.seo_sites s ON (s.organization_id,s.id)=(k.organization_id,k.site_id)
			JOIN public.seo_cms_connections c ON (c.organization_id,c.site_id,c.id)=(k.organization_id,k.site_id,k.id)
			WHERE k.organization_id=$1 AND k.encrypted_key IS NOT NULL`, true
	case "health":
		return `SELECT to_jsonb(h)-'organization_id' AS value,h.binding_id::text AS id,h.provider,h.site_id::text AS site,
			h.integration_account_id::text AS account,h.status,s.name||' '||s.base_url AS label,h.checked_at AS observed
			FROM public.seo_integration_health h JOIN public.seo_sites s ON (s.organization_id,s.id)=(h.organization_id,h.site_id)
			WHERE h.organization_id=$1`, true
	case "history":
		return `SELECT to_jsonb(r)-'organization_id' || jsonb_build_object('request_count',
			(SELECT count(*) FROM public.seo_execution_request_calls x WHERE x.organization_id=r.organization_id AND x.job_id=r.id)) AS value,
			r.id::text AS id,r.provider,r.site_id::text AS site,r.account_id::text AS account,r.status,r.operation AS label,r.created_at AS observed
			FROM public.seo_integration_sync_runs r WHERE r.organization_id=$1
		UNION ALL
			SELECT jsonb_build_object('id',a.id,'operation',a.action,'resource_id',a.resource_id,'created_at',a.created_at,'status','audited',
			'provider',coalesce(b.provider,ac.provider,p.provider,a.details->>'provider'),'site_id',coalesce(b.site_id::text,a.details->>'site_id'),
			'account_id',coalesce(b.integration_account_id,ac.id)) AS value,
			'audit-'||a.id::text AS id,coalesce(b.provider,ac.provider,p.provider,a.details->>'provider') AS provider,
			coalesce(b.site_id::text,a.details->>'site_id') AS site,coalesce(b.integration_account_id,ac.id)::text AS account,
			'audited' AS status,a.action AS label,a.created_at AS observed FROM public.seo_audit_events a
			LEFT JOIN public.seo_site_integration_bindings b ON (b.organization_id,b.id)=(a.organization_id,a.resource_id)
			LEFT JOIN public.seo_integration_accounts ac ON (ac.organization_id,ac.id)=(a.organization_id,a.resource_id)
			LEFT JOIN public.seo_integration_applications p ON (p.organization_id,p.id)=(a.organization_id,a.resource_id)
			WHERE a.organization_id=$1 AND a.action LIKE 'integration.%'`, true
	case "usage":
		return `SELECT jsonb_build_object('id',x.id,'site_id',b.site_id,'provider',b.provider,'account_id',x.integration_account_id,
			'operation',x.operation,'requested_at',x.requested_at,'http_status',x.diagnostic->'http_status',
			'code',x.diagnostic->'code','duration_ms',x.diagnostic->'duration_ms','cost_micros',x.diagnostic->'cost_micros') AS value,
			'binding-'||x.id::text AS id,b.provider,b.site_id::text AS site,x.integration_account_id::text AS account,
			x.diagnostic->>'category' AS status,x.operation AS label,x.requested_at AS observed
			FROM public.seo_execution_request_calls x JOIN public.seo_site_integration_bindings b
			ON (b.organization_id,b.site_id,b.id)=(x.organization_id,x.site_id,x.connection_id) WHERE x.organization_id=$1
			UNION ALL SELECT jsonb_build_object('id',x.id,'provider',a.provider,'account_id',a.id,
			'operation',x.operation,'requested_at',x.requested_at,'http_status',x.diagnostic->'http_status',
			'code',x.diagnostic->'code','duration_ms',x.diagnostic->'duration_ms','cost_micros',x.diagnostic->'cost_micros') AS value,
			'account-'||x.id::text AS id,a.provider,'' AS site,a.id::text AS account,x.diagnostic->>'category' AS status,
			x.operation AS label,x.requested_at AS observed FROM public.seo_integration_account_calls x
			JOIN public.seo_integration_accounts a ON (a.organization_id,a.id)=(x.organization_id,x.account_id)
			WHERE x.organization_id=$1`, true
	}
	return "", false
}

func (s *Store) IntegrationRows(ctx context.Context, org, section string, filter domain.IntegrationFilter) ([]json.RawMessage, error) {
	p := domain.PrincipalFromContext(ctx)
	if p.OrganizationID != org || p.Role != domain.RoleOwner && (section == "applications" || section == "accounts" || section == "usage") {
		return nil, domain.ErrExecutionAuthority
	}
	if err := domain.ValidatePage(filter.Limit, filter.Offset); err != nil {
		return nil, err
	}
	if filter.SiteID != "" && !domain.ValidID(filter.SiteID) || filter.AccountID != "" && !domain.ValidID(filter.AccountID) {
		return nil, domain.ErrNotFound
	}
	for _, date := range []string{filter.From, filter.To} {
		if date != "" {
			if _, err := time.Parse(time.DateOnly, date); err != nil {
				return nil, integrationInput("range", "use YYYY-MM-DD")
			}
		}
	}
	if filter.From != "" && filter.To != "" && filter.From > filter.To || len(filter.Query) > 200 || len(filter.Status) > 64 {
		return nil, integrationInput("filter", "use a valid range and bounded filters")
	}
	source, ok := integrationListSource(section)
	if !ok {
		return nil, domain.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `WITH source AS (`+source+`) SELECT value FROM source
		WHERE ($2='' OR provider=$2 OR $2='google' AND provider IN ('gsc','ga4')) AND ($3='' OR site=$3)
		AND ($4='' OR account=$4) AND ($5='' OR status=$5)
		AND (nullif($6,'') IS NULL OR observed>=nullif($6,'')::date::timestamp AT TIME ZONE 'UTC')
		AND (nullif($7,'') IS NULL OR observed<(nullif($7,'')::date+1)::timestamp AT TIME ZONE 'UTC')
		AND ($8='' OR position(lower($8) in lower(label))>0)
		ORDER BY observed DESC NULLS LAST,id LIMIT $9 OFFSET $10`, org, filter.Provider, filter.SiteID, filter.AccountID,
		filter.Status, filter.From, filter.To, filter.Query, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]json.RawMessage, 0)
	for rows.Next() {
		var row json.RawMessage
		if err = rows.Scan(&row); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) IntegrationSummary(ctx context.Context, org string) (json.RawMessage, error) {
	if domain.PrincipalFromContext(ctx).OrganizationID != org {
		return nil, domain.ErrExecutionAuthority
	}
	var data json.RawMessage
	err := s.db.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_object(
		'provider',p.kind,'accounts',(SELECT count(*) FROM public.seo_integration_accounts a WHERE a.organization_id=$1
			AND a.deleted_at IS NULL AND (p.kind='bing' AND a.provider='bing' OR p.scope=ANY(a.granted_scopes))),
		'bound_sites',summary.bound,'last_success_at',summary.success,'errors',summary.errors,
		'healthy_count',summary.healthy) ORDER BY p.sort)
		FROM (VALUES ('gsc',$2::text,1),('ga4',$3::text,2),('bing',NULL::text,3),('indexnow',NULL::text,4)) p(kind,scope,sort)
		CROSS JOIN LATERAL (SELECT count(h.binding_id) AS bound,max(h.last_sync_at) AS success,
			count(h.binding_id) FILTER (WHERE h.status IN ('ERROR','REAUTH_REQUIRED','FAILED','REVOKED','SITE_CHANGED')) AS errors,
			count(h.binding_id) FILTER (WHERE h.status='HEALTHY') AS healthy
			FROM public.seo_integration_health h WHERE h.organization_id=$1 AND h.provider=p.kind) summary`, org, domain.GSCReadScope, domain.GA4ReadScope).Scan(&data)
	return data, err
}

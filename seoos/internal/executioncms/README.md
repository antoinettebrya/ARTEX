# CMS execution adapters

The CMS execution worker binds each connector to one approved public HTTPS site root. Supply the WordPress site URL, including its installation subdirectory when present. The WordPress REST prefix is derived as `/wp-json/wp/v2` underneath that root. WordPress access requires an Application Password from a dedicated account with permissions for the intended posts, pages, and media. TestConnection reads the API discovery index and the authenticated user's actual capabilities. The connector uses the worker's metered transport to apply request budgets and inherits the shared public DNS guard. It observes HTTP redirects in place. Public article and WordPress sitemap reads carry no CMS authorization header. [WordPress authentication](https://developer.wordpress.org/rest-api/using-the-rest-api/authentication/) · [Application Passwords](https://developer.wordpress.org/advanced-administration/security/application-passwords/) · [WordPress users API](https://developer.wordpress.org/rest-api/reference/users/)

Authenticated post and page reads request edit context and preserve raw title, body, and excerpt. These fields are used for recovery markers and exact restoration. New content starts as a draft. PublishPage accepts either an immediate release or a future UTC timestamp. UpdatePage preserves state when its input asks for an edit only and can restore a previously recorded state. The worker is responsible for separate publish authorization and saving a snapshot before the write. The adapter's page mutations are limited to POST and PATCH. [WordPress posts](https://developer.wordpress.org/rest-api/reference/posts/) · [WordPress pages](https://developer.wordpress.org/rest-api/reference/pages/) · [WordPress media](https://developer.wordpress.org/rest-api/reference/media/)

The capability response distinguishes active SEO plugin namespaces from writable schema fields. Yoast's native REST endpoint is read-only. Yoast or Rank Math writes use known string keys explicitly exposed in that resource's WordPress REST edit metadata schema. Site administrators can supply compatible server-side registration when appropriate. The connector records actual public head title, description, and canonical separately from the authenticated draft snapshot. An absent description remains absent. [Yoast REST API](https://developer.yoast.com/customization/apis/rest-api/) · [Rank Math headless support](https://rankmath.com/kb/headless-cms-support/) · [WordPress registered metadata](https://developer.wordpress.org/rest-api/extending-the-rest-api/modifying-responses/)

## Generic HTTP CMS contract

Choose the site's approved HTTPS root as the connection base. An adapter endpoint lives at `/api/artex-cms/v1` below that root. Its JSON contract uses the shared `domain/execution.go` DTOs. These control-plane endpoints authenticate a bearer token. The public URL returned for a published item must serve its actual page to anonymous requests.

| Method and path under the prefix | Request and response |
| --- | --- |
| GET `/site` | Direct CMSSiteInfo object including URL and capability flags |
| GET `/pages` | Query uses `resource=post\|page`, `limit`, and optional `slug`. Return `{"items":[CMSPage]}`, including drafts in exact-slug recovery |
| GET `/pages/{id}` | Query includes resource. Return a direct CMSPage with raw body |
| POST `/pages` | Create a draft from CMSPageInput. Return a direct CMSPage |
| PATCH `/pages/{id}` | Apply the supplied fields and optional state. Return a direct CMSPage |
| POST `/pages/{id}/publish` | Set state `publish` or `future`. Future adds `scheduled_at` in RFC 3339. Apply supplied article fields before release and preserve omitted fields. Return a direct CMSPage |
| GET `/media` | Return `{"items":[CMSMedia]}` within the requested limit |
| POST `/media` | Read raw image or PDF bytes with Content-Type and Content-Disposition. Return a direct CMSMedia |
| GET `/sitemap` | Return Sitemap protocol XML. Return HTTP 404 if unavailable |

Provide `seo_title`, `description`, and `canonical` in capability `seo_fields` only where the adapter can write them. Core CMS states are `draft`, `pending`, `private`, `publish`, and `future`. Return explicit article statuses and raw fields after writes. Currently, the generic page listing retrieves at most 250 entries and each connector bounds a response to 4 MiB.

## Verification

```sh
go test -race -count=1 ./internal/executioncms
bash scripts/test_wordpress.sh
```

The Go HTTP suite covers Basic and bearer authorization, route/schema detection, supported operations, live HTML observations, protected destinations, credential reflection, and propagation of metering errors. The shell fixture uses the local Docker daemon, disposable WordPress and MariaDB containers, and a dedicated editor Application Password created using WP-CLI. The production client logic goes through a test TLS ingress and reads back the WordPress page after publication and restoration. The harness removes only resources it created. A successful fixture run exercises a real WordPress server in the isolated test environment. [Official WordPress Docker image](https://hub.docker.com/_/wordpress) · [WP-CLI Application Password commands](https://developer.wordpress.org/cli/commands/user/application-password/)

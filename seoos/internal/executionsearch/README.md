# Search and analytics providers

The Phase 4 execution worker uses these adapters for connection checks, reporting, inspection and submission. Each adapter limits credentials to the provider's HTTPS API. The caller encrypts credentials, authorizes site and organization access, budgets each HTTP attempt, and schedules retries. The adapters return safe error codes and preserve the provider's `Retry-After` header when present.

| Provider | Required setup | Observations |
| --- | --- | --- |
| Google Search Console | Enable the Search Console API. Register a Google OAuth web application redirect URI and connect an account that can read the Search Console property. | Related properties, sitemap processing status, final web Search Analytics, and URL Inspection. |
| Google Analytics 4 | Enable the Analytics Admin and Data APIs. Register a Google OAuth web application redirect URI and connect an account that can read the GA4 property. | Property discovery and organic-search report rows by date, landing page, country, language and device. |
| Bing Webmaster | Verify the site in Bing Webmaster Tools and generate its API key. Supply the site URL exactly as listed by Bing. | Verified status, search performance, sitemap feeds, URL discovery and crawl evidence, and direct URL submission. |
| IndexNow | Host the matching UTF-8 key file in the site root. An optional custom key file URL authorizes URLs under its directory on the same site. | Submission receipt. HTTP 202 also indicates that key verification is pending. |

Google consent uses OAuth authorization codes, S256 PKCE, offline access, and read-only scopes specific to GSC or GA4. The caller associates the consent state with the organization, site and connection and consumes it once. GA4 property discovery with a blank property lists the authorized account's available property IDs and display names for setup. With a property configured, the adapter retains its own property summary and filters the report by the registered site hostname and path.

GSC pagination uses `startRow` offsets and applies a registered-site page filter even for a domain property. Google Search Console provides top rows and may omit data under its own limits. `position` represents the reported average search position. URL Inspection's index verdict can show inclusion or exclusion. The last crawl is stored only when an inspection provides a valid timestamp. [Search Analytics](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) · [URL Inspection](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect)

GA4 filters on the session default channel group `Organic Search`. `key_events` is the count reported by GA4 and `session_key_event_rate` is a fraction. The adapter marks reports partial when GA4 reports thresholding, sampling, or data loss from the `(other)` row. Cross-domain installations should verify their GA4 hostname and landing-page setup against Analytics reports. [Account summaries](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/accountSummaries/list) · [GA4 Data API](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/properties/runReport) · [Dimensions and metrics](https://developers.google.com/analytics/devguides/reporting/data/v1/api-schema)

Bing Webmaster serves site time series, top query rows and top page rows at separate reporting grains. Adding their clicks and impressions together would double-count activity. The adapter preserves the recorded calendar date and official crawl or discovery timestamps where available. It checks the daily submission quota before direct batches of up to 500 URLs. Valid IndexNow batches can contain up to 10,000 URLs. Submission receipts confirm that the provider accepted the notification. [Bing JSON API](https://learn.microsoft.com/en-us/bingwebmaster/api-protocols) · [Bing submission and quotas](https://blogs.bing.com/webmaster/2019/11/Accessing-Bing-webmaster-tools-api-using-cURL/) · [IndexNow protocol](https://www.indexnow.org/documentation)

Each sync can make up to 32 provider calls under the supplied request limit. GSC and GA4 report pages request up to 1,000 rows. Output retains available data and flags a sync that reaches its request limit before the final page. OAuth refresh is metered separately. Bing throttles can arrive as HTTP 400 with a documented API fault code. [Bing fault format](https://learn.microsoft.com/en-us/bingwebmaster/getting-started) · [Bing fault codes](https://learn.microsoft.com/en-us/dotnet/api/microsoft.bing.webmaster.api.interfaces.apierrorcode)

Run the HTTP contract suite from the repository root.

```sh
go -C seoos test -race -cover ./internal/executionsearch
```

The suite uses local HTTP fixtures addressed through the official host names in an injected transport. Live verification requires an authorized staging site, Google OAuth clients and account access, a Bing API key for a verified site, and a reachable IndexNow key file.

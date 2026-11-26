# DataForSEO HTTP adapter

The adapter sends one JSON task per Live request to the fixed DataForSEO API host with HTTP Basic authentication. The worker supplies stored credentials, limits and request timeouts. The adapter caps each response at 8 MiB and each request at 30 seconds. It returns the measured USD cost as integer microdollars, rounding up any fractional microdollar. The queue handles rate limits and retries and uses its request ledger as the idempotency boundary. The optional provider `tag` correlates supported calls with a ledger entry.

| Operation | Official endpoint | Market dimensions | Reserve USD | Cache TTL |
| --- | --- | --- | ---: | ---: |
| Related terms | [Google Labs Related Keywords](https://docs.dataforseo.com/v3/dataforseo_labs/google/related_keywords/live/) | Location and language | 0.020 | 7 days |
| Related terms | [Bing Labs Related Keywords](https://docs.dataforseo.com/v3/dataforseo_labs-bing-related_keywords-live/) | US (2840), English | 0.020 | 7 days |
| Suggestions | [Google Labs Keyword Suggestions](https://docs.dataforseo.com/v3/dataforseo_labs/google/keyword_suggestions/live/) | Location and language | 0.020 | 7 days |
| Suggestions | [Bing Ads Keywords for Keywords](https://docs.dataforseo.com/v3/keywords_data/bing/keywords_for_keywords/live/) | Location, English/French/German, device | 0.100 | 7 days |
| Keyword overview | [Google Labs Keyword Overview](https://docs.dataforseo.com/v3/dataforseo_labs/google/keyword_overview/live/) | Location and language | 0.030 | 7 days |
| Keyword difficulty | [Bing Labs Bulk Keyword Difficulty](https://docs.dataforseo.com/v3/dataforseo_labs-bing-bulk_keyword_difficulty-live/) | US (2840), English | 0.030 | 7 days |
| Search volume | [Google Ads Search Volume](https://docs.dataforseo.com/v3/keywords_data/google_ads/search_volume/live/) | Location and language | 0.100 | 7 days |
| Search volume | [Bing Ads Search Volume](https://docs.dataforseo.com/v3/keywords_data/bing/search_volume/live/) | Location, English/French/German, device | 0.100 | 7 days |
| Organic SERP | [Google Advanced](https://docs.dataforseo.com/v3/serp/google/organic/live/advanced/), [Bing Advanced](https://docs.dataforseo.com/v3/serp/bing/organic/live/advanced/) | Location, language and device | 0.005 ordinary, 0.025 with paid operators | 24 hours |
| Page metadata | [OnPage Instant Pages](https://docs.dataforseo.com/v3/on_page/instant_pages/) | URL and requested crawl language | 0.001 | 3 days |
| Body structure | [OnPage Content Parsing Live](https://docs.dataforseo.com/v3/on_page/content_parsing/live/) | URL and requested crawl language | 0.001 | 3 days |
| Structured data | [OnPage Microdata](https://docs.dataforseo.com/v3/on_page/microdata/) | URL and previously validated Instant Pages task | 0.000001 | 3 days |

Reservations include headroom for the documented request shapes. Related Keywords uses depth 1 and the Labs discovery endpoints request at most 30 items. SERP requests are bounded to two pages and return available organic items with `rank_group` from 1 through 10. Pricing references cover [Labs](https://dataforseo.com/pricing/dataforseo-labs/dataforseo-google-api), [Google Ads](https://dataforseo.com/pricing/keywords-data/google-ads), [Bing Ads](https://dataforseo.com/pricing/keywords-data/bing-ads), [SERP multipliers](https://dataforseo.com/help-center/serp-api-cost-explained) and [Instant Pages](https://dataforseo.com/pricing/on-page/onpage-api). Google Ads Live has an upstream limit of 12 requests per minute per account. The adapter handles [DataForSEO status codes](https://docs.dataforseo.com/v3/appendix/errors/) and the HTTP `Retry-After` header.

Nullable provider metrics remain nullable. Paid-search competition comes from the advertising source, and organic difficulty comes from the matching search engine's Labs data. Google Ads' categorical competition is retained in the raw receipt, with its separately documented numeric index available in the normalized result. The worker should use the appropriate DataForSEO location code for each requested market.

Instant Pages yields title, description, H1–H6, canonical URL, declared meta keywords, the observed markup flag and available modification dates for the requested HTML page. It enables markup validation so the worker can fetch typed evidence when DataForSEO reports markup.

Content Parsing Live supplies heading levels and counts of primary blocks, secondary blocks and tables per section. Topic labels come from those headings and author names come from explicit fields. Within a group of sections, a question heading beneath a clearly labeled FAQ section is recorded as an observed question.

Microdata retrieval accepts the UUID of a validation-enabled crawl and costs $0 according to the provider. The plan uses a one-microdollar reservation to account for the request through the ledger. The normalized result contains declared Schema.org type names, named entities from explicit schema fields and questions declared by `FAQPage` markup. Validation messages and the full bodies, answers and contact fields remain only in the owner-restricted raw receipts.

Run `go test -race ./internal/researchprovider` from `seoos`. The contract tests validate official request and response shapes using fixed local responses and do not spend API credits.

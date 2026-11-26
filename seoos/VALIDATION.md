# First site-management milestone — validation record

Validated on 2026-11-26 against the new `seoos/` module. Baseline branch head:
`9b039e47d09989f1c233bb60659fc1cb3f773bcf`.

## Executed successfully

| Check | Observed result |
| --- | --- |
| Go toolchain | Go 1.26.3, linux/amd64; official download checksum verified |
| Database | Disposable local PostgreSQL 17.12 |
| `go test -race -count=1 ./...` with `SEOOS_TEST_DATABASE_URL` set | API, configuration, domain and storage packages passed; database tests were executed |
| `go vet ./...` | Passed |
| `go build -trimpath -o bin/seoos ./cmd/seoos` | Passed |
| `python3 scripts/smoke_test.py` | Real HTTP and PostgreSQL lifecycle passed, including restart and graceful shutdown |
| `go list -mod=readonly -deps ./cmd/seoos` | 236 packages; no legacy ARTEX packages outside `seoos/`, no Norma dependency |
| `git diff --check` | Passed before commit |

The suite contains 20 top-level Go tests with additional table-driven cases.
Five tests exercise PostgreSQL: migration startup concurrency/repetition,
organization isolation and lifecycle, concurrent optimistic updates,
conflicting/cancelled writes and pagination, and non-overwriting organization
bootstrap. Another store test checks invalid-DSN credential redaction.

The smoke script launches two configured organizations against the same local
database, verifies authentication and cross-organization read/update isolation,
creates and updates real records, checks duplicate URL/version conflicts, and
restarts the service to verify persisted state.

The first test run caught an unused cancellation-context variable in an
integration test. That test now passes the cancelled context to the write, and
the full suite was rerun successfully. Review also led to regression coverage
for unreserved URL encodings, preserved escaped slashes, IPv6 normalization and
canonical URL length.

## Verification boundaries

- Docker was not available in the local execution environment. Container build
  validation is configured in `.github/workflows/seoos-ci.yml`; this record does
  not claim that GitHub CI or Docker has already passed.
- CI is configured for PostgreSQL 16; local integration evidence is PostgreSQL
  17.12. The CI result is the PostgreSQL 16 compatibility check.
- Existing root Go/Next.js applications were not changed or started, and their
  full regression suites were not run. The Phase 0 audit is still incomplete.
- No WordPress website, external model provider, image-generation service or SEO
  data provider was called. Those integrations and the new frontend remain to be
  implemented.
- This is a tested site-management foundation, not evidence of production
  readiness or 150-site load capacity.

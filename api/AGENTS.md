# AGENTS.md — `api/`

Instructions for any coding agent working in `api/`, the Go backend of the openmentor monorepo.
**Read the repo-root `AGENTS.md` too** — it holds the repo-wide rules (CI gate shape, monorepo
commit rules, decision log, testing bar). Under the AGENTS.md convention the nearest file wins and
does *not* merge with the root one, so the rules below that also appear there are repeated
deliberately.

## What this is

Module `github.com/openmentor-io/openmentor/api`, Go 1.26. Three binaries from one image:

| Binary | Role |
|---|---|
| `cmd/api` | Gin HTTP API. The only one the frontend talks to. |
| `cmd/worker` | Internal-only. Cron plus fire-and-forget HTTP calls from the API. |
| `cmd/migrate` | Runs golang-migrate at deploy time, gated by `service_completed_successfully`. |

This is **live in production with real user data**. Treat every change as reaching real mentors.

## Commands

```bash
make ci               # lint + test-race — what the PR gate runs
make lint             # golangci-lint, version pinned by GOLANGCI_VERSION in api/Makefile
make test             # go test ./...
make migration-check  # migration NUMBERING guard; needs no database
make migration-test   # every migration up → down → up against a throwaway Postgres (needs docker)
make install-tools    # installs the pinned golangci-lint into GOPATH/bin
```

`make lint` refuses any `golangci-lint` whose version isn't `GOLANGCI_VERSION` or that was built
with an older Go than `go.mod` targets — a mismatched binary reports a different set of issues than
CI, which is how CI once went green while `make lint` reported 44 problems. `golangci-lint` already
covers gofmt, `go vet`, staticcheck and gosec, so `ci` does not invoke them again.

## Migrations

- **Line 1 declares the phase**: `-- phase: expand` (only adds — tables, columns, indexes, seed
  rows, widened constraints) or `-- phase: contract` (removes, renames, or narrows, so an older
  image can no longer find what it reads). `infra/rollback.sh` reads these markers out of the
  deployed image to decide whether a rollback may cross a migration boundary, so an unmarked or
  mismarked file silently degrades a production safety check. `cd infra && make check` fails on
  both, and on a missing `.down.sql`.
- **Every migration ships a `.down.sql`, and it documents what it cannot restore.** Down-migrations
  are for a human to run deliberately: `000009_modernise_tags.down.sql` cannot bring back the
  mentor–tag associations its up-migration cascaded away. **Never wire a lossy down-migration into
  anything automatic.**
- **A contract migration ships a release *after* the code that stopped reading the old shape.**
  expand → deploy → contract. `migrate` shares `BACKEND_IMAGE_TAG` with backend and worker, so the
  schema and the image move together or the deploy gate fails half-way.
- **Run `make migration-check` before you push a migration.** Two branches that both add
  `000014_*.sql` are *different files*: git merges them with no conflict, and golang-migrate then
  records one version number for both, so the second is never applied and never reported pending.
  Worse, `Up()` only walks **forward** from the recorded version, so a migration merged below what
  production already applied is never applied *there* while every fresh database and every CI run
  gets it — invisible until a query hits the missing column in production. The script fails on
  duplicate versions, a missing `.up.sql`/`.down.sql` counterpart, a gap in the sequence, and a
  version not greater than the base ref's highest.

## Database writes

- **A write that gates a side effect must require exactly one affected row.** The house pattern is
  in `api/internal/worker/repository.go`: the method returns `(applied bool, err error)` from
  `tag.RowsAffected() > 0`, and the caller sends the email / fires the webhook only when `applied`
  is true. Without it, an overlapping cron pass or a redelivered POST emails the mentor twice.
- **Compare-and-set on the value you read, not just on a status.** `UPDATE … SET status='draft'
  WHERE status='draft'` is *not* exclusive: Postgres re-evaluates the WHERE against the winner's
  committed row, which still matches, so the second caller wins too and overwrites the token the
  first one already emailed. `FinalizeNewMentor` closes it by also matching the confirmation token
  it read (`COALESCE(email_confirmation_token,'') = $9`) and by refusing to touch a token whose
  window is still open.
- **A row and its dependent rows go in one transaction.** `CreateMentor` and `UpdateMentorTags`
  (`internal/repository/mentor_repository.go`) both hold `pool.Begin` across the row write and the
  `mentor_tags` rewrite, so a mentor never exists with half its tags.
- **Prove these against a real Postgres with concurrent callers.** Transaction, single-affected-row
  and single-use guarantees are SQL properties; a mock proves nothing about them. See the `*_db_test.go`
  files (`repository_claim_db_test.go`, `review_invitation_db_test.go`, `session_consume_db_test.go`).

## The nullable-column rule

`mentorSelect` (`internal/repository/mentor_repository.go`) is the one column list every full mentor
read shares. **Every nullable column added there must either be COALESCEd in the query or land in a
pointer field of `models.Mentor`** — pgx fails the *whole row scan* on a NULL into a non-pointer
destination, so one un-COALESCEd column takes out every caller at once. It has: one column broke
login, the public profile page and the catalog together. `airtable_id` is the only legitimate
pointer (nil means "registered natively"). `internal/repository/mentor_nullable_columns_db_test.go`
enumerates the nullable columns from `information_schema` and checks each one against a real
database. The same rule applies to `client_requests`, which is nullable nearly everywhere.

## Logging, PII and capabilities

- **Never log an email, a name, a contact detail or request text.** Not even hashed:
  low-entropy PII must be *masked*, not hashed — a hashed street address in Loki is a membership
  oracle for anyone who can guess addresses.
- **Never log a capability.** `client_requests.id`, login tokens, email-confirmation tokens and
  review tokens are bearer credentials. Use `redact.ID` (`api/pkg/redact`) when an identifier has
  to appear at all.
- **`logger.RedactedError(err)` is the house style, not `zap.Error(err)`.** Error strings carry the
  row ids and payloads the repository layer put in them; `internal/worker` contains zero
  `zap.Error` for exactly that reason.

## Reuse, don't reinvent

| Package | Use it for, and the failure it prevents |
|---|---|
| `pkg/safego` | `safego.Go(task, fn)` instead of a bare `go func()` on any request path. `recover()` is per-goroutine, so Gin's recovery middleware cannot catch a panic in a goroutine it spawned — one such panic killed the process *after* the handler had already returned 200. |
| `pkg/redact` | Query, path, URL, free-text and id redaction. One implementation, so a new sink can't get a weaker one. |
| `pkg/tracing/redact.go` | Strips capabilities from span attributes by wrapping the OTLP **exporter**, not by registering a SpanProcessor: `otelhttp` sets its attributes *after* `tracer.Start`, so a processor runs too early to see them. |
| `pkg/imageclass` | Bounded image decode. `MaxDecodeBytes × maxConcurrentDecodes` is a deliberate share of the API container's `mem_limit` (512 MiB in `infra/docker-compose.yml`), pinned by `TestDecodeBudgetFitsContainer`. Changing a constant means re-deciding that budget. |
| `internal/middleware/admission.go` | Bounds requests **in flight** (not arrival rate) on the big-body endpoints; the slot is taken before the body is read. It is the other half of the same memory budget. |
| `test/dbtest` | DB-backed tests. It takes a session-level Postgres advisory lock because the migrations are not all idempotent, and it **skips** when `OPENMENTOR_TEST_DATABASE_URL` is unset — so check the test actually ran before believing it. |

## Configuration

Config validation is **per binary**: `ValidateForAPI`, `ValidateForWorker`, `ValidateForMigrate`
in `config/config.go`, all fail-fast at startup. **Don't move a requirement one binary needs into
the shared path.** Doing that once forced S3 credentials into `migrate` and `worker`, which then
had to be widened in `infra/env-allowlist.txt` — handing two containers a secret neither reads.
The Docker smoke test in `.github/workflows/ci-api.yml` deliberately uses a separate env file per
binary (`ci-migrate.env`, `ci-api.env`, `ci-worker.env`); a single shared file would only ever
prove the union validates.

Any env-var change lands as one PR across `api/config/config.go`, `infra/.env.example`,
`infra/.env.production.example`, `infra/docker-compose.yml` and `infra/env-allowlist.txt`.

## Branching

Feature branches only — never merge to `main` without explicit permission. Product and
architecture decisions get a row in `docs/migration/DECISIONS.md`.

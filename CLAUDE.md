# CLAUDE.md

Guidance for Claude Code when working in the openmentor monorepo.

## Map

- `web/` — Next.js 16 frontend. Own `CLAUDE.md` with conventions; work from `web/` (`npm run dev/test/lint`, `npx tsc --noEmit`).
- `api/` — Go backend, module `github.com/openmentor-io/openmentor/api`. Three binaries: `cmd/api`, `cmd/worker`, `cmd/migrate`. Verify with `make ci` from `api/` (or `make lint` / `make test`).
- `infra/` — Compose/Traefik deployment + observability. Validate with `docker compose config -q` (needs `.env`; copy from `.env.example`, delete after) and, after touching a service's `environment:`, `./check-service-env.sh`.
- `docs/` — decisions (`docs/migration/DECISIONS.md`), runbooks, design reference. `docs/migration/` is a historical record of the getmentor→openmentor fork; don't "fix" old paths there.
- `brand/` — brand asset pack. Never redraw the logo; reference files verbatim (see `brand/README.md` rules). Served copies live in `web/public/brand`.

## Rules

- Cross-cutting changes (API contract, env vars, compose services) land as ONE commit/PR touching all affected directories — that's the point of the monorepo.
- Env contracts: `infra/.env.example` and `.env.production.example` must stay consistent with what `api/config/config.go` and `web/` actually read.
- Never commit real `.env` files or secrets; templates are `*.example` (root `.gitignore` enforces this — don't weaken it).
- Product/architecture decisions get a row in `docs/migration/DECISIONS.md`.
- CI: `Checks / required-checks` runs on every PR and is the ONLY check that should be required — `CI / Web` and `CI / API` are path-filtered, so requiring their job names deadlocks any PR that doesn't touch that subtree. The gate owns the fast checks exclusively (web lint/type-check/tests, api golangci-lint/tests); the deep workflows keep only what it can't cheaply do (production build, race + coverage floor, gosec SARIF, Docker smoke test). Don't add a fast check to both — that duplication is what made a web PR lint twice and `go vet` run three times.
- Lint/test commands live in `api/Makefile` and `web/Makefile`, and the workflows call those targets — so `make lint` locally and CI run the identical check. Don't inline a command in a workflow that differs from its make target; that is how CI went green while `make lint` reported 44 issues. golangci-lint is pinned via `GOLANGCI_VERSION` in `api/Makefile`, which the workflow reads.
- For every new feature, create a separate git branch; never merge to main without explicit permission.

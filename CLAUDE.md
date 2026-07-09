# CLAUDE.md

Guidance for Claude Code when working in the openmentor monorepo.

## Map

- `web/` — Next.js 16 frontend. Own `CLAUDE.md` with conventions; work from `web/` (`yarn dev/test/lint`, `npx tsc --noEmit`).
- `api/` — Go backend, module `github.com/openmentor-io/openmentor/api`. Three binaries: `cmd/api`, `cmd/worker`, `cmd/migrate`. Verify with `go build ./... && go vet ./... && go test ./...` and `gofmt -l .` from `api/`.
- `infra/` — Compose/Traefik deployment + observability. Validate with `docker compose config -q` (needs a `.env`; copy from `.env.example`, delete after).
- `docs/` — decisions (`docs/migration/DECISIONS.md`), runbooks, design reference. `docs/migration/` is a historical record of the getmentor→openmentor fork; don't "fix" old paths there.
- `brand/` — brand asset pack. Never redraw the logo; reference files verbatim (see `brand/README.md` rules). Served copies live in `web/public/brand`.

## Rules

- Cross-cutting changes (API contract, env vars, compose services) land as ONE commit/PR touching all affected directories — that's the point of the monorepo.
- Env contracts: `infra/.env.example` and `.env.production.example` must stay consistent with what `api/config/config.go` and `web/` actually read.
- Never commit real `.env` files or secrets; templates are `*.example` (root `.gitignore` enforces this — don't weaken it).
- Product/architecture decisions get a row in `docs/migration/DECISIONS.md`.
- CI: `Checks / required-checks` is the required PR gate; `CI / Web` and `CI / API` are path-filtered deep coverage. Keep job names stable — branch protection references them.
- For every new feature, create a separate git branch; never merge to main without explicit permission.

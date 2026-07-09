# 05 — Infrastructure & Rebranding for openmentor.io

## Service replacement map (RU/region-specific → global)

| Today (getmentor) | openmentor.io (recommended default) | Alternatives |
|---|---|---|
| Yandex Cloud VM (ru-central1) | Hetzner Cloud VM (EU) — same Docker-Compose stack works verbatim | AWS Lightsail/EC2, DigitalOcean, Fly.io |
| Yandex Container Registry (`cr.yandex`) | GitHub Container Registry (`ghcr.io`) — repos already on GitHub | ECR, Docker Hub |
| Yandex Managed PostgreSQL | **PG container on the Hetzner VM** (D2 finalized): Hetzner auto-backups + nightly `pg_dump` to S3; scale path = managed PG (Neon/RDS) via `DATABASE_URL` swap | Neon, Supabase, RDS |
| Yandex Cloud Postbox (email) | **Resend** or AWS SES (the email sender — now the Go worker in `openmentor-api` — speaks the SESv2 API; SES is the smallest code change; Postbox IS the SESv2 API) | Postmark, SendGrid (already supported in code) |
| Yandex Object Storage (S3, profile pics) | **AWS S3** (DECIDED D15) — the Go code uses S3-compatible client; endpoint/region config change only; images copied via `openmentor-infra/migration/yandex-to-s3-migration.js` | Cloudflare R2, Backblaze B2 |
| Azure Blob Storage (legacy images) | Not needed — fresh install stores everything in the S3-compatible bucket; remove Azure storage code path (or keep code, unused) | — |
| Azure Functions (`openmentor-func`) | **Azure dropped (D6) — IMPLEMENTED 2026-07-08.** Jobs live in the openmentor-api codebase (`cmd/worker`, branch go-worker) and run as a separate `worker` container from the same image — process-isolated (own crash domain, DB pool cap, mem limit), one language, shared data-access code; auth via `X-Worker-Token` shared secret. The interim step (TS func app as a container on the VM) was SKIPPED — went straight to the Go worker. `openmentor-func` repo deprecated, kept for reference | — |
| Yandex Cloud Monitoring | Drop (Grafana Cloud already covers app metrics); provider-native DB metrics later | — |
| Grafana Cloud | Keep (region-agnostic) | — |
| Cloudflare DNS | Keep for openmentor.io | — |
| Yandex context ads | Removed (03) | — |

## Target topology (D2 + D6, 2026-07-08)

One **active** provider, two passive accounts, ~€10–15/mo:

```
Hetzner VM (only thing operated day-to-day)
└── docker compose: traefik / frontend / api / worker(jobs) / postgres / alloy
AWS (passive, one account):  S3 = images (D15) · SES = email (D1)
Cloudflare (passive):        DNS
ghcr.io:                     container images (replaces cr.yandex)
Grafana Cloud (free tier):   metrics/logs/traces
```

Scale seams: `DATABASE_URL` → managed PG; VM resize; worker container scales/deploys independently of the API. S3/SES scale on their own.

## Rebrand sweep (all repos)

Search-and-replace with review (not blind sed) across `openmentor`, `openmentor-api`, `openmentor-func`, `openmentor-infra`:
- `getmentor.dev` → `openmentor.io` (URLs in code, emails, templates, README, compose labels, CORS origins, cookie domains).
- `GetMentor` → `OpenMentor` (display strings), `getmentor` → `openmentor` (package names: `openmentor/package.json` name, Go module path in `go.mod` + all imports — use `gofmt`-safe module rename, docker image names, compose service labels, `O11Y_*` namespace values).
- `hello@getmentor.dev` → `hello@openmentor.io`.
- Remove `ru.${DOMAIN}` router from Traefik config.
- GitHub Actions: update registry endpoint, image names, repo checkout paths (`openmentor-io/openmentor`, `openmentor-io/openmentor-api`), new secrets.
- `mcp.${DOMAIN}` subdomain: keep or drop (DECISION; default keep — costs nothing).

## New-environment checklist (condensed; full detail in infra README)

1. Domain `openmentor.io` on Cloudflare; A record → VM.
2. VM with Docker; firewall 22/80/443.
3. ghcr.io registry + PAT/OIDC for Actions.
4. PostgreSQL (managed or containerized) → `DATABASE_URL`; run `migrate`.
5. S3-compatible bucket for images → `YANDEX_STORAGE_*` env vars renamed `S3_STORAGE_*` (code change in `openmentor-api/pkg/yandex/` — rename package to `pkg/s3storage/`).
6. Email domain setup: SPF/DKIM/DMARC for openmentor.io at chosen provider; sender `hello@openmentor.io`.
7. ReCAPTCHA keys for openmentor.io (or migrate to Cloudflare Turnstile — DECISION, default: keep ReCAPTCHA v2, new site key).
8. Grafana Cloud stack + new `O11Y_SERVICE_NAMESPACE=openmentor-io`.
9. PostHog project for openmentor (fresh, don't mix with getmentor analytics).
10. GitHub org `openmentor-io`: push `openmentor-api`, `openmentor-infra` repos (web app already at `github.com/openmentor-io/openmentor`; `openmentor-func` deprecated 2026-07-08 — reference only, deploy workflow disabled).

## Deliberately unchanged

Traefik/compose topology, health-check + rollback deploy script, Alloy observability pipeline, migration-runner pattern — all provider-agnostic and proven.

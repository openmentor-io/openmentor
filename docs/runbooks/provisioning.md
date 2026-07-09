# Runbook: Production Provisioning (P6.4)

The path from "code complete" to a live openmentor.io. Ordered so nothing blocks:
SES production access (step 2.4) and DNS propagation are the only slow items — start them first.
Tags: **[console]** provider web UI (owner) · **[terminal]** CLI (owner or Claude) · **[code]** repo change (Claude).

Region convention: **eu-central-1** everywhere in AWS (matches env template examples).

## 0. Prerequisites

- [ ] `openmentor.io` registered, nameservers on Cloudflare
- [ ] AWS account (root MFA on; day-to-day via an IAM admin user)
- [ ] Hetzner Cloud account
- [ ] Grafana Cloud account (EU stack), PostHog account (EU cloud)
- [ ] GitHub repo `openmentor-io/openmentor` pushed, branch protection on (`Checks / required-checks`)
- [ ] Ko-fi account (for the donate page URL)

## 1. Decide: fresh start or carry getmentor data?

- [ ] **Decision needed.** Fresh start = nothing to do here. Carrying data over is a
  small project of its own: the schema diverged (columns renamed/dropped:
  `preferred_contact`, no `tg_secret`/`telegram_chat_id`, no sponsor tags), so it
  needs a transform script (old dump → new schema) plus the image copy
  (`infra/migration/yandex-to-s3-migration.js`, now targeting the S3 images bucket).
  If carrying: do it between steps 7.1 and 7.2. Ask Claude to build the transform.

## 2. AWS (one account, eu-central-1)

### 2.1 S3
- [ ] [console/terminal] Bucket `openmentor-images` — public read for objects (or serve via
  Cloudflare later), CORS allowing GET from `https://openmentor.io`
- [ ] [console/terminal] Bucket `openmentor-db-backups` — private, no public access
- [ ] Values → `S3_STORAGE_BUCKET`, `S3_STORAGE_ENDPOINT=https://s3.eu-central-1.amazonaws.com`,
  `S3_STORAGE_REGION=eu-central-1`, `NEXT_PUBLIC_S3_STORAGE_ENDPOINT=s3.eu-central-1.amazonaws.com`,
  `NEXT_PUBLIC_S3_STORAGE_BUCKET=openmentor-images`, `BACKUP_S3_BUCKET=openmentor-db-backups`

### 2.2 ECR (D19)
- [ ] [terminal] `aws ecr create-repository --repository-name openmentor-frontend`
      and `--repository-name openmentor-backend`; add a lifecycle policy (keep last ~20 images)
- [ ] Registry URL = `<ACCOUNT_ID>.dkr.ecr.eu-central-1.amazonaws.com`

### 2.3 IAM (scoped users; access keys into env/secrets, never the repo)
- [ ] `openmentor-app` — S3 RW on `openmentor-images` → `S3_STORAGE_ACCESS_KEY/SECRET_KEY`
- [ ] `openmentor-backup` — S3 RW on `openmentor-db-backups` → `BACKUP_AWS_ACCESS_KEY_ID/SECRET`
- [ ] `openmentor-mailer` — `ses:SendEmail`/`SendTemplatedEmail` → `SES_ACCESS_KEY_ID/SECRET_ACCESS_KEY`
- [ ] `openmentor-vm` — ECR pull (`ecr:GetAuthorizationToken` + read on both repos) → used by deploy over SSH
- [ ] CI push to ECR: prefer **GitHub OIDC role** (no long-lived keys) with ECR push on both repos;
      fallback: `openmentor-ci` user with keys in GitHub secrets

### 2.4 SES (START EARLY — production-access review takes up to ~24h)
- [ ] [console] Verify domain identity `openmentor.io`; add the 3 DKIM CNAMEs in Cloudflare
- [ ] [console] Cloudflare TXT records: SPF `v=spf1 include:amazonses.com ~all`,
      DMARC `_dmarc` → `v=DMARC1; p=quarantine; rua=mailto:hello@openmentor.io`
- [ ] [console] Request production access (exit sandbox) — use-case: transactional mentorship-platform email
- [ ] Values → `SES_REGION=eu-central-1` (+ keys from 2.3); `MODERATORS_EMAIL` set;
      inbound mail for hello@/privacy@ (Cloudflare Email Routing → your inbox)

## 3. Cloudflare

- [ ] [console] DNS: `A openmentor.io → <VM IP>` and `CNAME www → openmentor.io` — **DNS-only (grey
  cloud) first**; consider proxying after ACME works
- [ ] [console] API token, scope `Zone:DNS:Edit` on the zone only → `CLOUDFLARE_DNS_API_TOKEN` (Traefik ACME DNS-01)
- [ ] [console] **Turnstile**: create widget for `openmentor.io` →
  `NEXT_PUBLIC_TURNSTILE_SITE_KEY` + `TURNSTILE_SECRET_KEY` (replaces the CI test keys)
- [ ] [console] Optional: `a.openmentor.io` PostHog reverse proxy (CSP already allows it); otherwise
  PostHog uses `eu.i.posthog.com` directly

## 4. Hetzner

- [ ] [console] VM: CPX21 (3 vCPU/4 GB) or CX32, Ubuntu 24.04, EU location, your SSH key;
  enable **automated backups**
- [ ] [console] Firewall: inbound 22 (your IP if feasible), 80, 443; outbound open
- [ ] [terminal] Harden + prep:
  ```bash
  adduser deploy && usermod -aG docker,sudo deploy   # key-only SSH, disable root login + passwords
  apt update && apt install -y docker.io docker-compose-v2 awscli && systemctl enable docker
  mkdir -p /opt/openmentor/infra && chown deploy /opt/openmentor/infra
  docker volume create openmentor-postgres-data
  ```
- [ ] `VM_SSH_HOST/USER/KEY` noted for GitHub secrets + `.env.production`

## 5. Observability & analytics

- [ ] [console] Grafana Cloud (EU): note Prometheus/Loki/Tempo/Pyroscope endpoints + IDs, create RW
  API key → all `GCLOUD_*` vars
- [ ] [terminal] Import dashboards: `cd infra/grafana && make build` → upload; alert rules after first deploy
- [ ] [console] PostHog (EU): project → `NEXT_PUBLIC_POSTHOG_KEY`, `NEXT_PUBLIC_POSTHOG_HOST=https://eu.i.posthog.com`,
  `POSTHOG_API_KEY` (api/worker), `POSTHOG_HOST`; sync dashboards later (`infra/posthog/dashboards/sync.mjs`)
- [ ] [console] **GTM (`GTM-NBGRPCZ`): remove the Mixpanel tag** (leftover manual task from D18)

## 6. Code task — ECR swap **[code: Claude, ~1 branch]**

- [ ] Replace `cr.yandex/${YANDEX_REGISTRY_ID}` image prefix with `${ECR_REGISTRY}` in compose;
  swap registry login in `deploy.sh` (local push + remote pull over SSH: `aws ecr get-login-password | docker login`),
  `rollback.sh`, and `.github/workflows/deploy.yml` (`aws-actions/configure-aws-credentials` + `amazon-ecr-login`,
  OIDC role); env templates gain `ECR_REGISTRY`/`AWS_REGION`, lose `YANDEX_REGISTRY_ID`/`YANDEX_SA_KEY`;
  retire `infra/migration/` if step 1 chose fresh start. **Blocked only on knowing the AWS account ID.**

## 7. Secrets assembly & first deploy

### 7.1 Build `.env.production` from `infra/.env.production.example`
- [ ] Generate strong values (`openssl rand -hex 32`): `INTERNAL_MENTORS_API` (= web's
  `GO_API_INTERNAL_TOKEN`), `MENTORS_API_LIST_AUTH_TOKEN`, `WORKER_AUTH_TOKEN`, `METRICS_AUTH_TOKEN`;
  `JWT_SECRET` (`openssl rand -base64 64`); `POSTGRES_PASSWORD` (`openssl rand -base64 24`) +
  matching `DATABASE_URL=postgres://openmentor:<pw>@postgres:5432/openmentor?sslmode=disable`
- [ ] Fill everything from steps 2–5 (S3/SES/Turnstile/Cloudflare/Grafana/PostHog/backup vars,
  `DOMAIN=openmentor.io`, `LETSENCRYPT_EMAIL`)
- [ ] GitHub Actions secrets for `deploy.yml`: `VM_SSH_HOST/USER/KEY`, `DOMAIN`,
  ECR auth (OIDC role ARN or keys), `NEXT_PUBLIC_S3_STORAGE_ENDPOINT/BUCKET`, `NEXT_PUBLIC_TURNSTILE_SITE_KEY`

### 7.2 Deploy
- [ ] [terminal] `cd infra && ./deploy.sh all` (first run: uploads env, syncs infra/, builds+pushes both
  images, migrate → backend → worker → frontend with health checks + rollback)
- [ ] [terminal] Seed the first moderator (there is no signup for admins):
  ```sql
  INSERT INTO moderators (name, email, role) VALUES ('Georgiy Mogelashvili', 'you@example.com', 'admin');
  ```
  via `docker exec -it openmentor-postgres psql -U openmentor openmentor`
- [ ] (If carrying getmentor data: restore transformed dump + run image copy now — see step 1)

### 7.3 Smoke test (P7.3)
- [ ] `https://openmentor.io` renders the catalog (empty is fine); TLS cert valid
- [ ] Register a test mentor at `/bementor` (real Turnstile) → confirmation email arrives (SES)
- [ ] Moderator magic-link login at `/admin/login` → approve the mentor → approval email arrives
- [ ] Mentor appears in catalog; contact them; mentee + mentor + moderators emails arrive
- [ ] Mentor magic-link login → dashboard → move request through statuses → session-complete email
- [ ] Leave a review via the emailed link → mentor gets the review notification
- [ ] Worker crons: `curl -X POST -H "X-Worker-Token: …" http://localhost:8090/jobs/cron/randomize-sort-order`
  (on the VM) returns a summary
- [ ] Grafana: dashboards receiving metrics/logs/traces; PostHog: events arriving
- [ ] Email deliverability: send a flow email to mail-tester.com, expect ≥9/10 (DKIM/SPF/DMARC pass)
- [ ] Backup: `docker exec openmentor-postgres-backup /backup.sh once` → object lands in the backups bucket

## 8. Post-launch hygiene

- [ ] Restore drill within the first week (docs/runbooks/postgres-backup-restore.md) — then quarterly
- [ ] Accept/sign DPAs: AWS, Hetzner, Cloudflare, PostHog, Grafana, Google (GTM)
- [ ] Grafana alert rules (availability, error rate, latency) + a notification channel
- [ ] External uptime check (e.g. UptimeRobot) on `https://openmentor.io/api/healthcheck` via frontend
- [ ] Replace donate-page Ko-fi placeholder with the real URL
- [ ] Cloudflare proxy (orange-cloud) if desired, after confirming ACME renewals work
- [ ] Calendar reminder: professional legal review before scale (docs/legal/review-2026-07-09.md)

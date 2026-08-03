# Outstanding decisions — 2026-08 audit

Five things the remediation cannot settle on its own: they need production
access, an account owner, or a judgement call about acceptable risk. Nobody
should guess at them, and none of them should block the code fixes.

Each entry: **the question**, why it matters, what to check, and what each
answer implies.

---

## 1. How many outstanding review invitations exist in production?

**Why it matters.** The review link mailed to a mentee is
`/review?requestId=<client_requests.id>`, and that UUID is a bearer capability:
`SubmitReview` gates only on Turnstile, the UUID, and "this request is done and
has no review yet". No email, no session, no signature. Every such request has a
live, unexpirable, unrevocable capability outstanding. Audit item **H4** replaces
this with a real token — and the migration shape depends entirely on how many
live capabilities exist.

**What to check.** Run `diagnostics.sql` D4 (or `./diagnostics.sh` and read the
`D4` summary line). It reports outstanding capabilities, total completed
requests, and the percentage.

**What the answers imply.**

| D4 result | Implication |
|---|---|
| A small count (tens) | Ship the new token in one migration and **reissue** those invitations, invalidating the old links. Simpler and safer than a dual-read window: a mentee whose old link stops working gets a fresh email. |
| A large count (dev showed **138 of 141 = 98 %**) | A clean cutover would silently break almost every outstanding invitation. Do a proper expand → dual-read → cutover → contract sequence. That complexity is earned at that scale, and not before. |

**Related, and larger than it looks.** The same UUID is emitted to PostHog as a
`distinct_id` (`request:<uuid>`) from **seven** call sites — not just review
submission but the contact form, mentor request status updates, and four worker
jobs (`grep -rn RequestDistinctID api/`). So in practice a `request:<uuid>`
person record exists for roughly every client request that has ever been
touched, whether or not it has an outstanding review capability. D4 sizes the
*redesign*; it does not size the *cleanup* in §3.

---

## 2. Does a restorable pre-corruption `pg_dump` exist?

**Why it matters.** D2's clobbered prices were overwritten in place with no audit
trail, so a dump is the only source that gives back the exact old values
(`data-repair.md` §D2). And audit item **P8** is that backup failures are
completely silent — the sidecar logs a `FAILURE` line and nothing alerts on it.
So whether backups work is genuinely unknown until someone checks.

**What to check.** Both halves, in order — the second is the one that counts:

1. A dump **exists** and predates the corruption:
   ```bash
   aws s3 ls "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/" | sort
   docker logs openmentor-postgres-backup --tail 20   # SUCCESS line < 24 h old?
   ```
   Retention is `BACKUP_RETENTION_DAYS`, default 30 — anything older is pruned.
2. It **restores** into a scratch database: the procedure in
   `../postgres-backup-restore.md` §(c), or the `pg-pricecheck` recipe in
   `data-repair.md` §D2.3. A truncated or aborted upload lists in S3 perfectly
   well.

**What the answers imply.**

- **A dump restores.** Use it for the price recovery. Still record the drill
  (date, filename, row counts, time-to-restore) — it is the first evidence the
  backups work at all.
- **No dump restores.** Price recovery falls back to recomputing from the
  getmentor import (`data-repair.md` §D2.4), which only covers imported mentors
  and depends on a conversion rate that is not recorded in the database. For
  everyone else, asking the mentors is the honest option. **And the backup
  alerting plus the restore rehearsal stop being scheduled work and become
  urgent** — you have just discovered you have no recovery point.
- **No dump exists at all** (e.g. `BACKUP_S3_BUCKET` was never set, and the
  sidecar has been warning into a log nobody reads). Same as above, plus fix the
  configuration today.

---

## 3. Grafana Cloud and PostHog retention windows — and does the plan support deletion?

**Why it matters.** Because the system is live, review capability UUIDs and
magic-link tokens are **already** in the telemetry backends. The code fixes stop
the leak; they do not un-send what was sent. Whether you can purge, or can only
wait out retention, determines whether the operational half of audit item **P14**
is actionable at all.

**What to check.**

- **Grafana Cloud:** the retention window for Loki (logs) and Tempo (traces) on
  your plan, and whether the plan offers log/trace deletion for a label or time
  range. Capability UUIDs land in `url.path` span attributes and in request log
  lines on both the Go and Next.js sides.
- **PostHog:** the event retention window, and specifically **person deletion**.
  This is the sharp problem: `request:<uuid>` was used as a `distinct_id`, so
  these are *person* records, not an event property. Dropping a property is a
  different operation from deleting a person, and the two are gated differently.
  Check what your plan supports before promising anyone a purge.
- **Scope:** see §1 — assume roughly one `request:<uuid>` person per client
  request, not one per outstanding review capability.

**What the answers imply.**

- **Deletion is supported.** Do it as incident response: purge the affected
  persons/events and the affected Loki/Tempo ranges, then invalidate live
  magic-link and confirmation tokens (that part you control, and it is cheap —
  users just request a new link).
- **Deletion is not supported, or only in bulk.** Then the exposure ages out on
  the retention clock, and the mitigation is entirely on the other side: rotate
  and invalidate every token that leaked, and ship **H4** so the capability
  values in the backends stop being useful. Say plainly, in whatever record you
  keep, that historical telemetry still contains them.
- Either way, this touches the retention statements in `docs/legal/ropa.md` and
  the retain-while-relevant policy (DECISIONS **D13**). If a documented window
  turns out to be wrong, correct the document.

**Not verifiable from this repository.** Retention and plan tier are account
settings; nothing in `grafana/`, `infra/alloy/` or `infra/posthog/` records them.
Somebody has to open both consoles.

---

## 4. Ko-fi account ownership and the correct URL

**Why it matters.** The URL is a placeholder, and it is on every page. A
donations link that 404s (or, worse, points at somebody else's Ko-fi) on launch
day is both an embarrassment and a way to send money to a stranger.

**What to check.** Two files, both marked TODO, both referencing DECISIONS
**D4** ("exact account/link TBD by owner"):

- `web/src/config/donates.ts` — `linkUrl: 'https://ko-fi.com/openmentor'`,
  rendered by `web/src/pages/donate.tsx`.
- `.github/FUNDING.yml` — `ko_fi: openmentor` (and `github: openmentor-io`),
  which drives GitHub's "Sponsor" button.

`/donate` is linked from the homepage (`web/src/pages/index.tsx:342`), the nav
header, the footer, the FAQ and the About page — so this is reachable from
effectively every page, not just one.

**What the answers imply.**

- **The account exists and is yours.** Set the real handle in both files in one
  commit, drop both TODO comments, and check the GitHub Sponsor button renders.
- **It does not exist yet.** Either create it before launch, or remove the
  donations promotion until it does. Shipping a placeholder is the one option
  that is worse than both.
- **The handle is taken by someone else.** Pick a different handle; do not leave
  the link pointing at them. Verify by loading the URL, not by assuming.

---

## 5. Single-VM availability — accepted risk, but rehearse it once

**Why it matters.** Running everything on one Hetzner VM (DECISIONS **D2**) is a
documented, accepted risk and the right call at this scale — the alternative
buys availability with a large complexity bill nothing here justifies. The gap
is not the architecture; it is that **complete VM loss has never been
rehearsed**, and nothing outside the VM would notice if the VM went away.

**What to check.**

- Hetzner VM auto-backups are actually **enabled** (Cloud Console → server →
  Backups). `../postgres-backup-restore.md` §(b) documents the restore but not
  whether the snapshots exist.
- Whether any **external** uptime check exists. There is none in this
  repository: `grafana/alerting/alert-rules.yaml` has no synthetic/blackbox
  probe, and every alert path runs *through* Grafana Alloy on the VM being
  monitored. If the VM is gone, so is the thing that would tell you.

**What the answers imply.**

- **Rehearse once, end to end**, on a scratch server built from a snapshot:
  restore, confirm the site serves and the database is intact, and write down the
  actual time it took. That number is your real RTO; the ~30 min in the runbook
  is an estimate.

  **Do not point the public DNS record at the rehearsal server while the
  original VM is still serving.** DNS propagation and resolver caching are not
  atomic: for as long as both answers are live you have **two writable copies of
  production**. New mentorship requests, profile edits and reviews land on
  whichever database the visitor's resolver happened to return, the two diverge
  immediately, and everything written to the scratch copy is thrown away when the
  drill ends. There is no merge back — `client_requests` and `reviews` are
  append-only from the user's side, so a lost write is a lost request from a real
  mentee.

  Verify without touching public DNS:

  ```bash
  # Resolve openmentor.io to the rehearsal server for THIS machine only.
  # /etc/hosts wins over DNS, so the browser and curl exercise the real
  # hostname, real TLS SNI and the real Traefik routing.
  echo "<rehearsal-ip> openmentor.io www.openmentor.io" | sudo tee -a /etc/hosts

  # Or skip hosts editing entirely:
  curl -sS --resolve "openmentor.io:443:<rehearsal-ip>" https://openmentor.io/ -o /dev/null -w '%{http_code}\n'

  # Undo the hosts entry the moment you are done.
  ```

  A certificate warning is expected and fine — the rehearsal server has no
  certificate for the real hostname unless you also gave it the ACME data, and
  chasing one by solving a public challenge from the scratch box is another way
  to disturb production. If you want a clean TLS path, give the rehearsal server
  its **own** hostname (`drill.openmentor.io`) with its own record and its own
  certificate; nothing about the restore depends on the name being the live one.

  **The only safe way to point real DNS at it is a coordinated failover**: stop
  the production writers first (`docker compose stop backend worker frontend` on
  the original VM, or shut the VM down), confirm they are stopped, *then* move the
  record. That is a real cutover with real downtime — plan it as one, or do not do
  it. If you do, note that this is also the moment to check the DNS TTL: a long
  TTL is the difference between minutes and hours of split traffic.
- **Add an external uptime check** (any third-party HTTP monitor hitting
  `https://openmentor.io` and the API health endpoint from outside). This also
  compensates for audit item **P15** — a broken edge currently reports a
  successful deploy — until that is fixed.
- **If the rehearsal is painful**, that is the signal to revisit D2 (managed
  Postgres first, per the scale path in the backup runbook), not before.

---

## Notes

- None of these blocks the code fixes. §1 and §2 gate *repair* work; §3 gates
  *cleanup*; §4 and §5 are launch readiness.
- Record each answer where the decision lives: product/architecture answers get
  a row in `docs/migration/DECISIONS.md`; drill results and restore evidence go
  in the ops tracker alongside the quarterly restore test.

# Grafana observability as code

Dashboards and alert rules for the `openmentor` Grafana Cloud stack, versioned
as plain JSON/YAML (the old jsonnet/grafonnet pipeline in `infra/grafana/` was
retired in favour of Grafana **Git Sync**).

## Layout

```
grafana/
├── dashboards/                 # Dashboard JSON — Git Sync source of truth
│   ├── om-overview.json        # OpenMentor · Overview
│   ├── om-frontend.json        # OpenMentor · Frontend (Next.js)
│   ├── om-backend.json         # OpenMentor · Backend API (Go)
│   ├── om-worker.json          # OpenMentor · Worker & Email
│   └── om-database-infra.json  # OpenMentor · Database & Infra
├── alerting/
│   ├── alert-rules.yaml            # Alert rules — versioned source of record
│   └── notification-policies.yaml  # Routing tree — versioned source of record
├── slo/
│   └── slos.yaml                   # SLOs — versioned source of record (see file header)
└── README.md
```

All dashboards live in the Grafana folder **openmentor** (uid
`repository-7b3d712`, created by Git Sync — there is no folder with uid
`openmentor`), are tagged `openmentor`, and carry no volatile fields (`id`,
`version`) so the files diff cleanly. Verified live 2026-08-03: all five are
present in that folder.

## Dashboards: Git Sync is the source of truth

The JSON files in `grafana/dashboards/` are authoritative. Edit them here (or
edit in the Grafana UI and let Git Sync push a commit/PR back) — do not treat
ad-hoc UI-only edits as durable.

### One-time Git Sync setup (Grafana UI)

> **Already connected** — read back from the live stack 2026-08-03:
> repository `repository-7b3d712`, GitHub, branch `main`, path
> `grafana/dashboards`, sync enabled every 3600s, target `folder`,
> workflows `[branch]` (UI saves return as a branch/PR). A dashboard merged to
> `main` therefore reaches Grafana within the hour with no operator action.
> The steps below are for rebuilding the connection, not for first-time setup.
>
> Note the path — `grafana/dashboards` — which is exactly why the alert rules
> in `alerting/` are not covered. See the next section.

The UI wording shifts between versions; the flow is:

1. In Grafana, open **Administration → Provisioning** (sometimes labelled
   **Git Sync** or **Provisioning → Repositories**).
2. Connect the GitHub repository `github.com/openmentor-io/openmentor`
   (installs/authorizes the Grafana GitHub App or a fine-grained PAT with
   `contents:read/write` and `pull requests:write` on the repo).
3. Branch: `main`. Path: `grafana/dashboards/`.
4. Choose the sync behaviour: dashboards saved in the UI are written back to
   the repo (directly or via pull request), and merges to `main` sync into
   Grafana. Enable "pull request" mode if you want UI edits reviewed.
5. Finish the wizard; Grafana imports the five dashboards from the path above
   into the provisioned folder.

After Git Sync is connected, dashboards provisioned from the repo are managed
by it — the manual `update_dashboard`/API pushes used for the initial import
are no longer needed.

## Alert rules

> ### ✅ APPLIED 2026-08-04: all 14 rules are live (re-applied same day — read on)
>
> Applied after the 2026-08-04 deploy outage (migrate config-validation
> failure, site 404) paged nobody — the rules had been desired-state only
> since 2026-08-03. Applied atomically via
> `PUT /api/v1/provisioning/folder/openmentor-alerts/rule-groups/openmentor` with
> `X-Disable-Provenance: true`; contact points, the fan-out policy tree, the
> backup gauges, `up` and `pg_up` were all verified live first. Nothing syncs
> this file automatically — if you edit it, re-apply the group.

Alert rules are Grafana-managed rules in the **OpenMentor Alerts** folder (uid
`openmentor-alerts`). The first apply on 2026-08-04 targeted the manual
"OpenMentor" folder (uid `fd2fpl`); by ~15:45 UTC that folder — and all 14
rules with it — had been deleted from the stack, most plausibly by a human
tidying what looked like a duplicate of the Git Sync dashboards folder.
Deleting a Grafana folder silently deletes its alert rules; the new folder
name is deliberately un-duplicate-looking. The rules are NOT in the Git Sync
dashboards folder `repository-7b3d712`: Grafana
refuses to store alert rules in Git Sync-managed folders ("cannot store rules
in folder managed by Git Sync"), which is also why the Grafana UI's "Import
alert rules" button cannot ingest `alerting/alert-rules.yaml` (that importer
takes Prometheus/Mimir rule YAML, not the provisioning format; it fails with
"missing or invalid groups array"). Rule group `openmentor`, evaluated every 1m. The versioned
source of record is [`alerting/alert-rules.yaml`](alerting/alert-rules.yaml)
(Grafana alerting provisioning format, `apiVersion: 1`). Apply it via:

- the Grafana provisioning API
  (`POST /api/v1/provisioning/alert-rules` per rule, or
  `PUT /api/v1/provisioning/folder/openmentor-alerts/rule-groups/openmentor`
  for the group; header `X-Disable-Provenance: true` so they stay editable in
  the UI), or
- the Grafana Cloud MCP (`alerting_manage_rules`, operation `create`).

If you change a rule in the UI, mirror the change into the YAML file.

**Order matters when applying.** The default notification policy is live and
fans out to `telegram`, `slack` and `Discord` with `repeat_interval: 4h`, so a
rule that is true the moment it lands pages immediately and keeps paging. The
rules with `noDataState: Alerting` (ServiceDown, PostgresDown,
DatabaseBackupStale, and any future one) fire when their series is simply
absent — and **DatabaseBackupPipelineAbsent fires for the same reason with
`noDataState: OK`**, because its `absent()` query *is* the alarm: it returns 1
precisely while the gauge is missing. `openmentor_db_backup_first_start_timestamp_seconds`,
`openmentor_db_backup_last_success_timestamp_seconds` and
`openmentor_db_backup_max_age_seconds` do not exist in Prometheus yet — the
`postgres-backup` sidecar that publishes them ships with the audit P8 change;
re-verified 2026-08-03 that
`absent(openmentor_db_backup_last_success_timestamp_seconds{deployment="production"})`
returns `{deployment="production"} 1` against the live tenant. So apply both
backup rules **after** that deploy, or apply them with `isPaused: true` and
unpause once the gauges appear
(`docs/runbooks/postgres-backup-restore.md`).

Desired set: ServiceDown, HighErrorRate, HighLatencyP99, ContainerHighCPU,
ContainerHighMemory, GoroutineLeak, ContactFormFailures,
ReviewSubmissionFailures, EmailSendFailures, PostgresDown, DBErrorRate,
DBLatencyP95, DatabaseBackupStale, DatabaseBackupPipelineAbsent.

Notes:

- **DatabaseBackupStale** watches
  `openmentor_db_backup_last_success_timestamp_seconds`, falling back to
  `openmentor_db_backup_first_start_timestamp_seconds` until a dump has ever
  succeeded (the sidecar's one grace window on a fresh volume). Both are
  written by the `postgres-backup` sidecar into a shared volume and scraped by
  Alloy's textfile collector (`prometheus.exporter.unix "backup_metrics"`). The
  sidecar's daemon loop deliberately swallows per-run failures so a transient
  error can't kill it, which makes staleness of those gauges the only off-VM
  signal that nightly dumps have stopped — hence `NoData=Alerting` here too.
  Panels: the "Postgres Backups" row on `om-database-infra`. Until this rule is
  actually applied, the only signal is the container healthcheck — which now at
  least fails the deploy gates (`infra/deploy-remote.sh` and
  `infra/rollback.sh` read `.State.Health.Status` and exit 2 on `unhealthy`
  without rolling application images back), but still reaches nobody who is not
  watching a deploy.

  Two properties of the query are load-bearing, and
  `infra/alert-consistency-test.sh` (part of `cd infra && make check`) fails if
  either is lost — in the rule **or** in the panels, which have to carry both
  properties for the same reasons:

  - **One instance per deployment.** `./deploy.sh --staging` uploads the same
    `.env.production` to the staging VM, so both VMs remote-write these gauges
    into this tenant with `APP_ENV=production`; a global `max()` would report
    only the newest backup anywhere and a healthy staging pipeline would hold
    the rule Normal while production dumps rot. The rule therefore uses
    `max by (deployment)`, where `deployment` comes from `DEPLOYMENT_NAME` —
    written into the VM's `.env` by `deploy.sh` from the deploy target and
    stamped on the target by `infra/alloy/config.alloy`. `instance` cannot serve
    that purpose: it is the Alloy container id and changes on every recreate.
    Before that Alloy config reaches a VM the label is empty, i.e. exactly one
    instance — today's behaviour, not a regression.
  - **The threshold is the sidecar's own window.** The sidecar publishes
    `openmentor_db_backup_max_age_seconds` from `BACKUP_MAX_AGE_HOURS` and the
    rule subtracts it, so changing that variable in `.env.production` moves the
    alert with it. The `> 0` threshold reads "seconds past the configured
    window"; there is no duration in the rule to keep in sync.

  The "Postgres Backups" panels carry both properties too: the stat plots the
  rule's own expression (seconds past the published window, red at 0) and the
  timeseries draws that window as a dashed line next to the per-deployment age.
  A panel that maxed globally would show a fresh staging dump while production
  dumps rot — the person triaging reads the row, not the rule. Note the
  asymmetry in how the two halves ship: the dashboard is **Git-Synced hourly**,
  so a merge to `main` changes live Grafana with no operator action, while the
  rule above still has to be applied by hand.
- **DatabaseBackupPipelineAbsent** exists because `max by (deployment)` buys
  per-deployment instances at the cost of per-deployment `NoData`.
  `noDataState` is a property of the whole query result, not of label values: if
  production stops remote-writing while staging keeps going, the grouped query
  still returns staging's row, so DatabaseBackupStale is **Normal, not NoData**,
  and production's alert instance simply stops existing. Grafana treats that as
  a missing series and resolves it after `missing_series_evals_to_resolve`
  evaluations — meaning a production page already firing would auto-**resolve**
  the moment the pipeline died. That setting cannot close the gap; it only
  delays the resolve. Verified on this tenant 2026-08-03 with the rule's own
  shape over `up` and a job name that does not exist: exactly one row comes
  back, for the surviving job, never an empty result.

  So presence is asserted explicitly:
  `absent(<gauge>{deployment="production"})` per gauge, unioned with `or`.
  `absent()` propagates equality matchers, so each term yields
  `{deployment="production"} 1` and the rule gets a per-deployment instance; a
  regex matcher would collapse to one unlabelled row that only fires once
  *every* deployment is gone. One term per gauge, not one per deployment,
  because DatabaseBackupStale joins its three gauges on `deployment` — a sidecar
  too old to publish `openmentor_db_backup_max_age_seconds` drops production out
  of it just as completely. `noDataState: OK` is right here and nowhere else in
  the file: an empty result means everything expected is publishing.

  **Which deployments are expected is a hand-maintained list** in the rule's own
  comment (`expected-deployments: production` /
  `absence-not-expected: staging, local`), because it cannot be derived from
  metrics — that is the whole problem. `infra/alert-consistency-test.sh` case 6
  cross-checks it against every `DEPLOYMENT_NAME` value `deploy.sh` and
  `deploy-dev.sh` can write, so adding a deploy target fails `make check` until
  it is classified.

  **What this still cannot detect**, deliberately:
  - A staging backup pipeline dying. `./deploy.sh --staging` creates and
    destroys that VM, so asserting its presence would page on every intentional
    teardown. Staging dumps are unprotected by design.
  - A sidecar that keeps publishing a *frozen* textfile (dead daemon, file still
    on the volume). The series stay present, so absence says nothing —
    DatabaseBackupStale is what catches that, which is why both rules are needed.
  - A dump that succeeds but is corrupt, truncated, or unrestorable. Only the
    quarterly restore drill covers that
    (`docs/runbooks/postgres-backup-restore.md`).
  - Anything at all while Grafana's own alerting engine is the failure — or
    while an edit to `alerting/alert-rules.yaml` has not been re-applied. Both
    rules are live on the stack (applied 2026-08-04); nothing syncs later edits
    to this file, so "in the repo" and "on the stack" can still diverge.
- **PostgresDown** watches `pg_up`, shipped continuously by the Database
  Observability pipeline (live since 2026-07-18; setup in
  `docs/runbooks/database-observability.md`). `NoData=Alerting`, so the
  exporter disappearing also pages. Candidate follow-up: postgres panels
  (connections, TPS, locks) on `om-database-infra`.
- **ContainerHighCPU/Memory** key off cAdvisor's `name` label, and that label
  **does** exist — re-verified against the live tenant 2026-08-04:
  `container_cpu_usage_seconds_total` and `container_memory_working_set_bytes`
  carry `name="openmentor-backend"`, `"grafana-alloy"`, `"traefik"`, … alongside
  the host cgroup slices, which the rules' `name=~` matcher filters out. The
  earlier note here — that cAdvisor exposed only host slices — was wrong, and it
  is why both rules were left with unreachable thresholds (audit H13). **No
  cAdvisor mount or flag change is needed.** Both rules were rewritten in the
  same pass and are **desired state until an operator re-applies the group**:
  - CPU is now a share of the whole VM (`/ machine_cpu_cores`, which is 4 here),
    not of one core. The old `rate(...) * 100 > 90` fired at 22.5% of the machine
    while claiming "CPU above 90%".
  - Memory is now a ratio of each container's own `mem_limit` (85%), not an
    absolute 1 GiB — which was above the *largest* limit in the stack (768m), so
    the kernel OOM-killed every container before that rule could ever fire
    (verified live: 6 container series, none above 1 GiB). The divisors are
    written out in the rule because Adaptive Metrics has
    `container_spec_memory_limit_bytes` aggregated on this tenant and strips
    `name` from it, so the obvious `/ on (name) group_left ()` form errors out.
    `infra/alert-fireability-test.sh` checks each divisor against
    `infra/docker-compose.yml` so they cannot drift from the real limits.
  - `noDataState: OK` stays on both, and is now correct rather than a fig leaf:
    an empty result means the containers are not running, and `up` carries
    `job="prometheus.scrape.cadvisor"`, so **ServiceDown** covers cAdvisor
    itself disappearing.
- **DBErrorRate / DBLatencyP95** are grouped by `service_name`
  (`openmentor-api`, `openmentor-worker`). Ungrouped they summed both services
  together, and since the API does far more database work than the worker at
  every hour, a worker failing *every* query stayed under 5% of the combined
  denominator and never paged. A merged p95 is likewise a p95 of neither.
  Also desired state until re-applied.

## Notifications

Three contact points exist on the stack (created manually in the UI, since
they hold secrets and are never provisioned from this repo): **telegram**,
**slack**, **email**.

Alerts route through the **default notification policy**. The intended tree —
every alert fans out to all three contact points — is versioned in
[`alerting/notification-policies.yaml`](alerting/notification-policies.yaml):

```
Root: email  [group_by: grafana_folder, alertname | wait 30s · interval 5m · repeat 4h]
├─ (catch-all) → telegram   continue: true
├─ (catch-all) → slack      continue: true
└─ (catch-all) → email
```

The catch-all children chained with `continue: true` are what make every
alert hit all three receivers (the parent receiver only applies when no child
matches).

Apply changes via `PUT /api/v1/provisioning/policies` with
`X-Disable-Provenance: true` (exact curl in the YAML file), or edit in the UI
at **Alerting → Notification policies** and mirror the change back into the
YAML. Note the Grafana Cloud MCP **cannot** write notification policies (its
write scope covers dashboards and alert rules only), so unlike alert rules
this piece is applied by hand.

## Datasource UIDs (stack `openmentor`)

| Datasource | Type       | UID                    |
|------------|------------|------------------------|
| Metrics    | Prometheus | `grafanacloud-prom`     |
| Logs       | Loki       | `grafanacloud-logs`     |
| Traces     | Tempo      | `grafanacloud-traces`   |
| Profiles   | Pyroscope  | `grafanacloud-profiles` |

Services are identified by `service_name` ∈ `openmentor-frontend`,
`openmentor-api`, `openmentor-worker`; HTTP metrics share
`http_server_request_*` names with `http_request_method` / `http_route` /
`http_response_status_code` labels across all three services. Some panels
reference metrics that ship with the current code but have no series yet
(`db_client_*`, `s3_storage_*`, `openmentor_worker_cron_*`, several
`openmentor_*` business counters); they populate automatically after the next
deploy.

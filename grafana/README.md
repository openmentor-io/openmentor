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

> ### ⚠️ NOT APPLIED: the stack currently has ZERO alert rules
>
> Verified 2026-08-03 against the live stack — all three read paths agree and
> none of them is a permissions artefact (the same token lists the five
> dashboards and reads the notification policy tree):
>
> ```
> GET /api/v1/provisioning/alert-rules      -> []
> GET /api/ruler/grafana/api/v1/rules       -> {}
> GET /api/prometheus/grafana/api/v1/rules  -> {"groups":[]}
> ```
>
> So every rule below is **desired state, not live state**: nothing pages
> today, for anything. Applying them is an operator action (see below) — this
> file cannot do it, and Git Sync does not cover alert rules.

Alert rules are Grafana-managed rules in the `openmentor` folder — uid
`repository-7b3d712`, the folder Git Sync created; there is **no** folder with
uid `openmentor` — rule group `openmentor`, evaluated every 1m. The versioned
source of record is [`alerting/alert-rules.yaml`](alerting/alert-rules.yaml)
(Grafana alerting provisioning format, `apiVersion: 1`). Apply it via:

- the Grafana provisioning API
  (`POST /api/v1/provisioning/alert-rules` per rule, or
  `PUT /api/v1/provisioning/folder/repository-7b3d712/rule-groups/openmentor`
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
  - Anything at all while Grafana's own alerting engine or this repo's
    unapplied-rules gap is the failure. Neither rule is on the stack today.
- **PostgresDown** watches `pg_up`, shipped continuously by the Database
  Observability pipeline (live since 2026-07-18; setup in
  `docs/runbooks/database-observability.md`). `NoData=Alerting`, so the
  exporter disappearing also pages. Candidate follow-up: postgres panels
  (connections, TPS, locks) on `om-database-infra`.
- **ContainerHighCPU/Memory** key off cAdvisor's `name` label. cAdvisor
  currently exposes only host cgroup slices (no per-container series), so these
  stay quiet until per-container metrics appear; the Host row on
  `om-database-infra` covers the gap meanwhile.

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

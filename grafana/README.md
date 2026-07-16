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

All dashboards live in the Grafana folder **OpenMentor** (uid `openmentor`),
are tagged `openmentor`, and carry no volatile fields (`id`, `version`) so the
files diff cleanly.

## Dashboards: Git Sync is the source of truth

The JSON files in `grafana/dashboards/` are authoritative. Edit them here (or
edit in the Grafana UI and let Git Sync push a commit/PR back) — do not treat
ad-hoc UI-only edits as durable.

### One-time Git Sync setup (Grafana UI)

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

Alert rules are Grafana-managed rules in folder `openmentor`, rule group
`openmentor` (evaluated every 1m). The versioned source of record is
[`alerting/alert-rules.yaml`](alerting/alert-rules.yaml) (Grafana alerting
provisioning format, `apiVersion: 1`). Git Sync does **not** cover alert
rules — apply them via:

- the Grafana provisioning API
  (`POST /api/v1/provisioning/alert-rules`, header `X-Disable-Provenance: true`
  so they stay editable in the UI), or
- the Grafana Cloud MCP (`alerting_manage_rules`) — this is how the current
  set was created.

If you change a rule in the UI, mirror the change into the YAML file.

Current set: ServiceDown, HighErrorRate, HighLatencyP99, ContainerHighCPU,
ContainerHighMemory, GoroutineLeak, CacheHitRatioLow, ContactFormFailures,
ReviewSubmissionFailures, EmailSendFailures, PostgresDown, DBErrorRate,
DBLatencyP95.

Notes:

- **PostgresDown** watches `pg_up`. The Alloy config defines
  `prometheus.exporter.postgres`, but no `pg_*` metrics exist in the stack yet,
  so the rule uses `NoData=OK` to stay quiet. Once the exporter ships, flip it
  to `NoData=Alerting` and consider adding postgres panels (connections, TPS,
  locks) to `om-database-infra`.
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

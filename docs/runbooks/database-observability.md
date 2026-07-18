# Runbook: Grafana Cloud Database Observability (PostgreSQL)

Per-query insights for the production Postgres (query details/samples,
EXPLAIN plans, schema details) in Grafana Cloud → **Observability →
Databases**. Collection is done by Grafana Alloy's
`database_observability.postgres` component (config already in
`infra/alloy/config.alloy`, section "Database Observability"); metrics go
out via Prometheus remote-write, query events via Loki with
`job="integrations/db-o11y"`.

Docs: <https://grafana.com/docs/grafana-cloud/monitor-applications/database-observability/>

## Requirements (already satisfied by the repo)

- PostgreSQL ≥ 14 — compose runs `postgres:16.14-alpine`.
- Alloy ≥ 1.17.0 — compose pins `grafana/alloy:v1.17.1`.
- Direct DB connection (no PgBouncer) — Alloy connects to `postgres:5432`
  on the compose network.
- `pg_stat_statements` settings — set via the `postgres` service `command`
  in `infra/docker-compose.yml` (`shared_preload_libraries`,
  `compute_query_id=on`, `pg_stat_statements.track=all`,
  `track_activity_query_size=4096`). **Only applies after the postgres
  container is recreated** (~10–20 s downtime).
- DSN delivery — `deploy.sh` writes
  `/opt/openmentor/infra/alloy-secrets/postgres_secret_openmentor` from
  `POSTGRES_OBS_DSN` in `.env.production`.

## One-time setup (manual, on production)

1. Generate a password for the monitoring user, then set in
   `infra/.env.production`:

   ```bash
   POSTGRES_OBS_DSN=postgres://grafana_monitoring:<PASSWORD>@postgres:5432/openmentor?sslmode=disable
   ```

2. Deploy so the new postgres command flags and Alloy image/config land,
   and the postgres container is recreated with `pg_stat_statements`
   preloaded:

   ```bash
   cd infra && ./deploy.sh all
   ```

3. Create the extension and the monitoring user (`db.sh` opens psql on the
   prod container):

   ```bash
   ./db.sh -c "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"
   ./db.sh -c "CREATE USER grafana_monitoring WITH PASSWORD '<PASSWORD>'"
   ./db.sh -c "GRANT pg_monitor TO grafana_monitoring"
   ./db.sh -c "GRANT pg_read_all_stats TO grafana_monitoring"
   ./db.sh -c "GRANT pg_read_all_data TO grafana_monitoring"   -- schema_details / explain_plans
   ./db.sh -c "ALTER ROLE grafana_monitoring SET pg_stat_statements.track = 'none'"
   ```

4. Restart Alloy so it picks up the secret written in step 2 (deploy
   already restarts it; otherwise `docker restart grafana-alloy`).

## Verification

```bash
# Preload took effect
./db.sh -c "SHOW shared_preload_libraries"
./db.sh -c "SELECT count(*) FROM pg_stat_statements"   # as superuser

# Alloy is happy (no db-o11y errors)
ssh <vm> docker logs --since 5m grafana-alloy 2>&1 | grep -i "database_observability\|postgres"
```

In Grafana Cloud (telemetry appears within a few minutes):

- Explore → Prometheus: `pg_up{instance="db-openmentor-pg"}` and
  `pg_stat_statements_calls_total` return series.
- Explore → Loki: `{job="integrations/db-o11y"}` shows query events.
- **Observability → Databases** lists the `openmentor` database.

## Follow-ups once data flows

- Flip the **PostgresDown** alert rule to `NoData=Alerting`
  (`grafana/alerting/alert-rules.yaml` documents this; it was parked at
  `NoData=OK` while no `pg_*` metrics existed).
- Consider postgres panels (connections, TPS, cache hit, locks) on the
  `om-database-infra` dashboard.

## Notes

- Query text is **redacted by default** (bind parameters stripped) before
  leaving the database; `disable_query_redaction` stays off.
- The `logs` collector is intentionally disabled: the containerized
  Postgres logs to stdout, which the Docker log driver already ships.
- The monitoring user's own activity is excluded from stats
  (`exclude_users = ["grafana_monitoring"]` +
  `pg_stat_statements.track = 'none'` on the role).
- Rollback: remove the `command` block from the postgres service and
  redeploy — the extension and user are inert without it.

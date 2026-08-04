# Runbook: Container Hardening and the Docker Socket Proxy

**Trigger:** deploying the H7 change (`DECISIONS.md` D66), or diagnosing
something that behaves differently because of it. This is the one change in the
stack that can 404 the whole site from a *monitoring* mistake, so the checks
below are not optional decoration.

**Who can run this:** someone with SSH access to the VM and `infra/.env.production`.

## What changed

| | Before | After |
|---|---|---|
| traefik → Docker | `/var/run/docker.sock` bind-mounted (`:ro`, which does not restrict API calls) | `tcp://docker-socket-proxy:2375`, on a dedicated `internal` network |
| traefik flags | `--api.dashboard=true` | removed (was never routed) |
| cadvisor mounts | `/`, `/var/run`, `/sys`, `/var/lib/docker`, `/dev/disk` | the two sockets it opens, `/sys`, `/var/lib/docker`, `/dev/disk` — the **host root filesystem is gone** |
| every service | no `cap_drop`, no `no-new-privileges` | `no-new-privileges:true` + `cap_drop: [ALL]` everywhere, with the minimum `cap_add` each image actually needs, and `read_only` on traefik / cadvisor / alloy / postgres-backup |

## Pre-deploy checks (on the VM)

| # | Check | Why |
|---|---|---|
| 1 | `ls -l /var/run/docker.sock /run/containerd/containerd.sock` — both exist | cAdvisor's mounts are now exact paths instead of all of `/var/run`. cAdvisor v0.55's **docker** factory builds a containerd client and fails to register without `containerd.sock`, which silently costs every `name=` and `container_label_*` label — and both cAdvisor alert rules have `noDataState: OK`, so nothing would page. If the containerd socket lives elsewhere on this host, fix the path in `docker-compose.yml` before deploying |
| 2 | `docker info --format '{{.ServerVersion}}'` | traefik v3.7 negotiates the API version, so ≥ 29 is fine; the proxy passes `/version` through unchanged |
| 3 | Nobody else is deploying | this deploy recreates **every** container |

## Deploy

```bash
cd infra && ./deploy.sh infra
```

Expect a full-stack recreate: every service's definition changed
(`security_opt`/`cap_drop`), so compose recreates all of them, including
`postgres` (~10-20 s of database downtime) and `traefik`. There is a short
window of 502/404 at the edge. This is not a rolling deploy.

`deploy.sh` step 9 already fails the run if `https://$DOMAIN/api/healthcheck`
does not return 200, so a routing regression cannot pass silently.

## Post-deploy verification

```bash
# 1. Traefik actually reached the filtered API (not "cannot connect")
docker logs --since 5m traefik 2>&1 | grep -i "provider connection established"

# 2. The H7 acceptance criterion: container creation through the endpoint
#    Traefik sees is refused. 403 = correct.
docker run --rm --network openmentor-docker-api curlimages/curl:8.11.1 \
  -s -o /dev/null -w '%{http_code}\n' -X POST \
  http://openmentor-docker-socket-proxy:2375/v1.51/containers/create

# 3. ...while the four endpoints Traefik needs still answer 200
for p in /_ping /version /v1.51/containers/json '/v1.51/events?since=0&until=1'; do
  docker run --rm --network openmentor-docker-api curlimages/curl:8.11.1 \
    -s -o /dev/null -w "$p %{http_code}\n" \
    "http://openmentor-docker-socket-proxy:2375$p"
done

# 4. cAdvisor still has Docker metadata (this is check 1's real payoff)
docker run --rm --network openmentor-network curlimages/curl:8.11.1 \
  -s http://cadvisor:8080/metrics | grep -c 'container_label_com_docker_compose_service'

# 5. Everything is up and the sidecar is healthy
docker compose ps
docker inspect -f '{{.State.Health.Status}}' openmentor-postgres-backup
```

Then, in Grafana (within ~5 minutes):

- `container_cpu_usage_seconds_total{name=~"openmentor-.*"}` returns series —
  this is what breaks if the containerd socket path is wrong.
- `sum by (device) (container_fs_usage_bytes{id="/"})` still populates the *Host
  Filesystem Usage* panel. The root-partition series now comes via the
  `/var/lib/docker` mount instead of `/rootfs`; the junk `/rootfs/...` device
  series are gone, which is a reduction in cardinality, not a loss.
- A certificate renewal still works. Nothing about ACME changed (the DNS-01
  challenge and the `traefik-letsencrypt-certificates` volume are untouched, and
  an `internal:` network cannot take over a container's default route — verified),
  but this is the slowest failure to notice, so check
  `docker logs traefik | grep -i acme` after the next renewal.

## Rollback

Per item, smallest first. All of these are `infra/docker-compose.yml` edits
followed by `./deploy.sh infra --yes`.

| Symptom | Revert |
|---|---|
| Site 404s on every route | Re-add `- /var/run/docker.sock:/var/run/docker.sock:ro` to `traefik.volumes` and delete `--providers.docker.endpoint=...` from **both** `docker-compose.yml` and `docker-compose.dev.yml`. That restores the pre-H7 behaviour exactly |
| Container CPU/Memory panels blank, no `name=` labels | Replace cAdvisor's two socket mounts with `- /var/run:/var/run:ro` |
| Host Filesystem Usage blank | Re-add `- /:/rootfs:ro` to cAdvisor |
| A container will not start with a capability error | Remove `cap_drop`/`read_only` from that one service; keep `no-new-privileges` (it cannot cause this) |
| Everything | `git revert` the commit and `./deploy.sh infra` |

## Known risks this change introduces

1. **The socket proxy is in the serving path.** Verified behaviour: while the
   proxy is unreachable, Traefik's docker provider supplies no configuration and
   every request 404s; when the proxy comes back, routing recovers within
   seconds without touching Traefik. Hence `restart: always`, a healthcheck, and
   a 64 MiB limit against a ~5 MiB working set. Do not lower that limit.
2. **No alert covers "the edge 404s".** `ServiceDown` watches `up` for scrape
   targets, and the proxy is not one; the frontend, backend and worker all keep
   reporting `up=1` while Traefik has no routes. This gap already existed for
   Traefik itself — an edge probe is the fix, and it is not part of this change.
   Diagnostic in the meantime: site 404s + every target green ⇒ look at the
   proxy and at `docker logs traefik`.
3. **cAdvisor still holds root-equivalent access** through `docker.sock` and
   `containerd.sock`. Trimming mounts does not change that, and a filtering proxy
   cannot either while its docker factory requires containerd. Replacing cAdvisor
   is the real fix (D66).
4. **Traefik, cAdvisor, Alloy and the socket proxy still run as uid 0** (the app
   containers drop to 1001 in their images). `cap_drop: [ALL]` +
   `no-new-privileges` means that root has no capabilities, which is most of the
   value, but it is not `user:`. Going further is not a flag: Traefik would need
   its entrypoints moved off :80/:443 (Docker grants no *ambient* capabilities,
   so a non-root process cannot bind a privileged port even with
   `cap_add: NET_BIND_SERVICE`) **and** the existing root-owned
   `traefik-letsencrypt-certificates` volume chowned, or ACME silently stops
   being able to write `acme.json`. Alloy and cAdvisor need the docker socket's
   group. Each is its own change with its own verification.
5. **The filtered API still leaks secrets to whoever can reach it.**
   `GET /containers/{id}/json` returns every container's environment and
   `GET /containers/{id}/archive` reads files out of any container — both are part
   of the `CONTAINERS` group that Traefik's provider requires. This is why the
   proxy sits on `openmentor-docker-api` with only Traefik attached, and why
   moving Traefik to the file provider (no daemon access at all) is the
   follow-up.

@AGENTS.md

# Claude Code — `infra/`

The imported `infra/AGENTS.md` holds the substance. The repo-root `CLAUDE.md` is already in context
above this file (Claude concatenates root → deepest), so nothing here repeats it.

- **Use plan mode for anything under `infra/` or `grafana/`.** These scripts run as root on the
  production VM and the compose file is the production topology; a wrong change here is an
  incident, not a failed test.
- **Never run `deploy.sh`, `rollback.sh`, `deploy-remote.sh` or `db.sh` yourself, and never apply
  an alert rule group.** Propose the command and let the operator run it. Reading a script is fine;
  executing one reaches production.
- `make check` is the safe verification and needs no credentials — it falls back to
  `.env.example` when the machine has no `.env`, and only ever reads key names.
- When you edit a block that `deploy-remote.sh` and `rollback.sh` both carry, edit **both** copies
  in the same pass; `deploy-transition-test.sh` compares them byte for byte, and finding out from
  the test costs another round trip.

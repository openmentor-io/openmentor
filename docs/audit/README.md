# Audit and remediation (2026-08)

## Start here

**[`2026-08-remediation-plan.md`](./2026-08-remediation-plan.md)** — the authoritative, self-contained
remediation plan. It needs no other context: system orientation, operating constraints, 16 immediate
work items with acceptance criteria, three later phases, and the decision gates that need the owner.

**[`verification/README.md`](./verification/README.md)** — reproduce any finding yourself in a local dev
stack. Every high-severity claim in the plan was verified by executing code; this directory tells you how,
and preserves the proof programs.

**[`../runbooks/audit-2026-08/`](../runbooks/audit-2026-08/README.md)** — the operator half. Read-only
diagnostics to run against production (D1–D4), the repair runbook for what they find, and the decisions
that need the owner. The plan above is for whoever writes the code; that directory is for whoever holds
the production credentials.

## Source material (evidence — do not act on directly)

| File | What it is |
|---|---|
| `2026-08-02-full-repository-audit.md` | External advisor's audit. Ran `deadcode`, `knip`, `jscpd`, `govulncheck`, coverage, Compose expansion, OpenTelemetry exporter probes. Strongest on trust boundaries, state integrity, operational recovery. |
| `2026-08-02-repository-audit-remediation.md` | The external advisor's companion 29-task plan. |
| `2026-08-pre-launch-audit.md` | Internal audit. Strongest on CI/CD injection, a broken login path, rate-limiter behaviour under the real topology, repo hygiene. |

**These three documents each contain claims that later verification overturned.** They are kept as a
record of what was examined. Section 4 of the remediation plan lists every corrected claim — read that
before acting on anything in them.

Notable examples: the internal audit's "auth token leaked into trace headers" is a *latent*
misconfiguration, not an active leak; the external audit's "rollback runs old binaries against a new
schema" has the mechanism backwards; and the external plan's durable-queue and 4-phase review-token
tasks are deliberately scaled back (see §12 of the plan for the reasoning).

## Quick orientation

- **System is live** with modest real traffic; a wider launch event is still ahead. Short planned
  downtime is acceptable, extended downtime is not.
- **Run the §6 diagnostics first.** Two verified defects may have already damaged production data.
- **Phase 0 is ~7–10 focused days**, dominated by the email-rendering fix (`P6`) and secret isolation
  (`P10`).
- **Phase 0 migrations must be additive only** until `H10` is fixed, because `rollback.sh` is inoperable
  across a migration boundary.

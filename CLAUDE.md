@AGENTS.md

# Claude Code

Everything above applies. This section is Claude Code specific.

- **`AGENTS.md` is the whole instruction set** — one file at the repo root, imported above. There
  are no `api/`, `web/` or `infra/` instruction files to load on demand, which is deliberate:
  a nested `CLAUDE.md` only enters context when Claude happens to read a file in that directory,
  is not re-injected after `/compact`, and never reaches a subagent that the main conversation
  hasn't already pulled it into.
- **`Explore` and `Plan` subagents never load `CLAUDE.md` at all**, and no setting changes that.
  Any rule that must reach one has to be restated in the delegation prompt — an Explore agent
  does not know the env allowlist exists, that `test/dbtest` skips silently, or that
  `grafana/alerting/` is not synced.
- **Use worktrees for parallel work.** Several branches off `main` at once is the normal shape
  here. Note `push.default = upstream` is set globally and a worktree created with
  `git worktree add -b <branch> origin/main` inherits an upstream of `refs/heads/main` — so a
  bare `git push` targets `main`. Clear the upstream (`git config --unset branch.<b>.merge`) or
  always push with an explicit refspec.
- **Prefer plan mode for anything touching `api/migrations/`, `infra/`, `grafana/`,
  `api/config/config.go`, or an auth path.** Those are the places where a wrong change is a
  production incident rather than a failed test — and the migration numbering mistake in
  particular cannot be caught by reading the diff; run `make migration-check` instead.
- **Verify claims against `main` rather than trusting a plan or a summary**, including one I
  wrote. Feature work lands continuously and has repeatedly changed the shape of a finding.
- Keep this file under ~200 lines. Path-scoped instructions belong in `.claude/rules/` with
  `paths:` frontmatter so they load only when Claude touches matching files; repeatable
  procedures belong in a skill, not here.
- Block-level HTML comments are stripped before this file enters context, so use them for notes
  meant only for human maintainers.

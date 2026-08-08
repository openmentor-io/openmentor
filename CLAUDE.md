@AGENTS.md

# Claude Code

Everything above applies. This section is Claude Code specific.

- **Per-subtree instructions are additive here**, unlike the AGENTS.md convention: Claude
  concatenates every `CLAUDE.md` from the repo root down to the working directory, and loads
  subdirectory files on demand when it reads files in them. So `api/CLAUDE.md` supplements this
  file rather than replacing it.
- **Use worktrees for parallel work.** Several branches off `main` at once is the normal shape
  here. Note `push.default = upstream` is set globally and a worktree created with
  `git worktree add -b <branch> origin/main` inherits an upstream of `refs/heads/main` — so a
  bare `git push` targets `main`. Clear the upstream (`git config --unset branch.<b>.merge`) or
  always push with an explicit refspec.
- **Prefer plan mode for anything touching `api/migrations/`, `infra/`, or an auth path.** Those
  are the places where a wrong change is a production incident rather than a failed test.
- **Delegate breadth to subagents**, and give each one the constraints for its subtree — an
  agent that has not read `infra/AGENTS.md` will not know the env allowlist exists.
- **Verify claims against `main` rather than trusting a plan or a summary**, including one I
  wrote. Feature work lands continuously and has repeatedly changed the shape of a finding.
- Keep each `CLAUDE.md` under ~200 lines. Path-scoped instructions belong in `.claude/rules/`
  with `paths:` frontmatter so they load only when Claude touches matching files; repeatable
  procedures belong in a skill, not here.
- Block-level HTML comments are stripped before this file enters context, so use them for notes
  meant only for human maintainers.

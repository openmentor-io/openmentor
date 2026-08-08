@AGENTS.md

# Claude Code — `api/`

The imported `api/AGENTS.md` holds the substance. The repo-root `CLAUDE.md` is already in context
above this file (Claude concatenates root → deepest), so nothing here repeats it.

- **Use plan mode before touching `migrations/`, `config/config.go`, or an auth path.** Those are
  the places where a wrong change is a production incident rather than a failed test, and the
  migration numbering mistake in particular cannot be caught by reading the diff — run
  `make migration-check` instead.
- **DB-backed tests skip silently.** `test/dbtest` calls `t.Skipf` when
  `OPENMENTOR_TEST_DATABASE_URL` is unset, so `go test ./...` goes green having proved none of the
  concurrency guarantees. Read the `--- SKIP` lines before reporting a `*_db_test.go` as passing.
- **Don't report `make ci` as passing from a partial run.** It is `lint test-race`; the race
  detector is where the goroutine bugs surface, and it is the slow half people cut.
- Prefer a fake at the repository boundary over a mock of the query. A mock that silently accepted
  NULL into a `*string` is why the whole nullable-column defect class survived review.

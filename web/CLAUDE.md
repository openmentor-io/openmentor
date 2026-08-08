@AGENTS.md

# Claude Code — `web/`

The imported `web/AGENTS.md` holds the substance. The repo-root `CLAUDE.md` is already in context
above this file (Claude concatenates root → deepest), so nothing here repeats it.

- **Never run `prettier --write` across the package**, and don't "fix formatting while you're in
  there". 131 files under `src/` disagree with Prettier, so a broad write produces a diff nobody
  can review and hides the actual change. The pre-commit hook already formats what you staged.
- **Do not create `.md` files summarising a job run** anywhere under `web/`.
- Verify UI work in a browser (`npm run dev`) and say what you looked at. `make ci` type-checks
  and renders nothing — the Wysiwyg toolbar regression that shipped under a green suite came from
  a test that mocked the component wholesale.
- Adding an API route touches three places: the handler under `src/pages/api/`, its
  `withObservability(...)` wrapper, and `KNOWN_ROUTES` in `src/lib/with-observability.ts`. Do all
  three in one edit pass; the CI test that catches the third is easy to hit late.

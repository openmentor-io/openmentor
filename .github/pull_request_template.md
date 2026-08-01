## What changed

<!-- One or two sentences. What does this PR do? -->

## Why

<!-- The reasoning, especially if it isn't obvious from the diff.
     Link the issue if there is one: Fixes #123 -->

## How it was tested

<!-- What you actually ran or clicked through. "make ci passes" is fine for
     pure refactors; anything user-facing deserves a note on what you verified
     in a browser. -->

## Checklist

- [ ] `make ci` passes in every directory I touched (`web/`, `api/`)
- [ ] Cross-cutting change? API contract, env var, or Compose service changes to
      `web/`, `api/` and `infra/` are all in **this** PR
- [ ] New or changed env var? Added to `infra/.env.example`,
      `infra/.env.production.example`, the component `.env.example`, and (for web)
      `src/types/env.d.ts`
- [ ] No real `.env` file, secret, token or credential is in the diff
- [ ] UI change? Follows `docs/design-reference/design-system.md` — no Tailwind
      `gray-*`, the one button system, tokens for radii/shadows/pastels
- [ ] Decides something about the product or architecture? Added a row to
      `docs/migration/DECISIONS.md`

<!-- Not every box applies to every PR — strike out the ones that don't.
     First time here? Read CONTRIBUTING.md; and thanks for contributing. -->

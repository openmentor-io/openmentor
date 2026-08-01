.PHONY: help install lint lint-fix typecheck test test-watch build ci pre-commit clean

# Mirrors api/Makefile so `make lint` (and `make ci`) mean the same thing in
# either half of the monorepo. Each target is a thin wrapper over the npm
# script — .github/workflows/ci-web.yml calls these targets, so there is one
# definition of each check rather than one here and another in the workflow.

help:
	@echo "Available targets:"
	@echo "  install    - Install dependencies (clean install from the lockfile)"
	@echo "  lint       - Run ESLint over the whole package"
	@echo "  lint-fix   - Run ESLint with --fix"
	@echo "  typecheck  - Run the TypeScript compiler (no emit)"
	@echo "  test       - Run Jest tests"
	@echo "  test-watch - Run Jest in watch mode"
	@echo "  build      - Production Next.js build"
	@echo "  ci         - Run all CI checks (lint + typecheck + test + build)"
	@echo "  pre-commit - Run the fast checks (lint + typecheck + test)"
	@echo "  clean      - Remove build output"

install:
	@npm ci --no-audit --no-fund

# The `lint` script is `eslint .` — the whole package, not just src/. Build
# scripts and dotfile configs are Node CommonJS and are configured as such in
# eslint.config.js; scoping the lint to src/ is how they went unchecked.
lint:
	@echo "Running ESLint..."
	@npm run lint

lint-fix:
	@echo "Running ESLint with --fix..."
	@npx eslint . --fix

typecheck:
	@echo "Running TypeScript type check..."
	@npx tsc --noEmit

test:
	@echo "Running tests..."
	@npm test

test-watch:
	@npm test -- --watch

build:
	@echo "Building production bundle..."
	@npm run build

# Everything CI runs, in CI's order.
ci: lint typecheck test build
	@echo ""
	@echo "✅ All CI checks passed!"

pre-commit: lint typecheck test
	@echo "✅ Pre-commit checks passed!"

clean:
	@rm -rf .next out

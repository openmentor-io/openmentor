.PHONY: help check compose-config env-allowlist shellcheck backup-tests deploy-tests alert-tests

# The fast infra checks, in one place. .github/workflows/checks.yml calls these
# targets (as web/ and api/ do), so `make check` here runs exactly what the
# merge gate runs — no command is spelled out twice.
#
# Interpolation source: a real .env when the machine has one, otherwise the
# template. Only key names and structure are checked, so placeholder values are
# fine, and nothing is ever copied over an existing .env.
ENV_FILE := $(shell [ -f .env ] && echo .env || echo .env.example)

help:
	@echo "Available targets:"
	@echo "  check          - All fast infra checks (what CI runs)"
	@echo "  compose-config - Validate the prod and dev compose renders"
	@echo "  env-allowlist  - Per-service env allowlist + secret ownership"
	@echo "  shellcheck     - shellcheck every script in infra/"
	@echo "  backup-tests   - postgres-backup sidecar behaviour tests"
	@echo "  deploy-tests   - blocks the deploy scripts share (.env.runtime, backup gate)"
	@echo "  alert-tests    - backup alert + panels vs Alloy labels vs sidecar gauges"

check: compose-config env-allowlist shellcheck backup-tests deploy-tests alert-tests
	@echo "✅ Infra checks passed!"

compose-config:
	@echo "Validating compose renders (env from $(ENV_FILE))..."
	@docker compose --env-file $(ENV_FILE) -f docker-compose.yml config -q
	@docker compose --env-file $(ENV_FILE) -f docker-compose.yml -f docker-compose.dev.yml config -q

# --self-test also proves the check still bites, so a broken check cannot pass
# as an empty one.
env-allowlist:
	@./check-service-env.sh --self-test

# These scripts run as root on the production VM and are piped over ssh, where a
# quoting slip is a live-system incident rather than a stack trace (H12).
#
# The file list and the severity live in ../scripts/shellcheck-all.sh, which the
# merge gate also calls for the scripts OUTSIDE infra/ — one definition, so CI
# and this target can't drift apart on either.
shellcheck:
	@../scripts/shellcheck-all.sh infra

backup-tests:
	@./postgres-backup/backup-test.sh

deploy-tests:
	@./deploy-transition-test.sh

# Reads ../grafana/alerting and ../grafana/dashboards; the checks.yml infra
# filter covers both, so a Grafana-only PR still runs this.
alert-tests:
	@./alert-consistency-test.sh

.PHONY: help check compose-config env-allowlist backup-tests deploy-tests rollback-tests alert-tests alert-fireability-tests

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
	@echo "  backup-tests   - postgres-backup sidecar behaviour tests"
	@echo "  deploy-tests   - blocks the deploy scripts share (.env.runtime, backup gate)"
	@echo "  rollback-tests - rollback.sh migration-boundary guard + expand/contract policy"
	@echo "  alert-tests    - backup alert + panels vs Alloy labels vs sidecar gauges"
	@echo "  alert-fireability-tests - every alert rule can actually fire"

check: compose-config env-allowlist backup-tests deploy-tests rollback-tests alert-tests alert-fireability-tests
	@echo "✅ Infra checks passed!"

compose-config:
	@echo "Validating compose renders (env from $(ENV_FILE))..."
	@docker compose --env-file $(ENV_FILE) -f docker-compose.yml config -q
	@docker compose --env-file $(ENV_FILE) -f docker-compose.yml -f docker-compose.dev.yml config -q

# --self-test also proves the check still bites, so a broken check cannot pass
# as an empty one.
env-allowlist:
	@./check-service-env.sh --self-test

backup-tests:
	@./postgres-backup/backup-test.sh

deploy-tests:
	@./deploy-transition-test.sh

# Reads ../api/migrations as well as rollback.sh: the guard reads the phase
# markers out of the backend image, so the policy and the script are one check.
rollback-tests:
	@./rollback-migration-guard-test.sh

# Reads ../grafana/alerting and ../grafana/dashboards; the checks.yml infra
# filter covers both, so a Grafana-only PR still runs this.
alert-tests:
	@./alert-consistency-test.sh

# Every rule in ../grafana/alerting/alert-rules.yaml must be able to fire: the
# labels it keys on must exist, and its threshold must be reachable (H13).
alert-fireability-tests:
	@./alert-fireability-test.sh

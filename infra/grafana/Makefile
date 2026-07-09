# Grafonnet Dashboard Builder
# Requires: jsonnet, jsonnet-bundler (jb)
#
# Install dependencies:
#   brew install jsonnet jsonnet-bundler   # macOS
#   go install github.com/google/go-jsonnet/cmd/jsonnet@latest  # or via Go
#   go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@latest

.PHONY: all clean install build

DASHBOARDS_SRC := $(wildcard dashboards/*.jsonnet)
DASHBOARDS_OUT := $(patsubst dashboards/%.jsonnet,dist/%.json,$(DASHBOARDS_SRC))
ALERTS_SRC := $(wildcard alerts/*.jsonnet)
ALERTS_OUT := $(patsubst alerts/%.jsonnet,dist/alerts/%.json,$(ALERTS_SRC))

all: install build

# Install Grafonnet dependencies
install:
	@echo "Installing Grafonnet dependencies..."
	jb install

# Build all dashboards and alerts
build: $(DASHBOARDS_OUT) $(ALERTS_OUT)
	@echo "Build complete. Output in dist/"

# Build individual dashboard
dist/%.json: dashboards/%.jsonnet lib/*.libsonnet | dist
	@echo "Building $<..."
	jsonnet -J vendor -J lib -o $@ $<

# Build individual alert rule and convert to Prometheus YAML
dist/alerts/%.json: alerts/%.jsonnet lib/*.libsonnet | dist/alerts
	@echo "Building $<..."
	jsonnet -J vendor -J lib -o $@ $<
	@echo "Converting to Prometheus YAML..."
	@./scripts/json-to-prometheus-yaml.sh $@ $(patsubst %.json,%-prometheus.yaml,$@)

# Create output directories
dist:
	mkdir -p dist

dist/alerts:
	mkdir -p dist/alerts

# Clean build artifacts
clean:
	rm -rf dist/

# Development: watch and rebuild
watch:
	@echo "Watching for changes..."
	fswatch -o dashboards/ alerts/ lib/ | xargs -n1 -I{} make build

# Validate all jsonnet files
lint:
	@echo "Validating jsonnet files..."
	@for f in $(DASHBOARDS_SRC) $(ALERTS_SRC); do \
		echo "Checking $$f..."; \
		jsonnet -J $(JSONNET_PATH) --lint $$f || exit 1; \
	done
	@echo "All files valid."

# Show help
help:
	@echo "OpenMentor Grafana Dashboard Builder"
	@echo ""
	@echo "Usage:"
	@echo "  make install    Install Grafonnet dependencies (run once)"
	@echo "  make build      Build all dashboards and alerts to dist/"
	@echo "  make clean      Remove build artifacts"
	@echo "  make lint       Validate jsonnet syntax"
	@echo "  make watch      Watch for changes and rebuild"
	@echo ""
	@echo "Output:"
	@echo "  dist/*.json                      Dashboard JSON files (import to Grafana)"
	@echo "  dist/alerts/*.json               Alert rule JSON files (Grafana API format)"
	@echo "  dist/alerts/*-prometheus.yaml    Alert rules (Prometheus YAML for UI import)"

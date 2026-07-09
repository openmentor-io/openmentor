# OpenMentor Grafana Dashboards

This directory contains Grafana dashboards and alert rules defined as code using [Grafonnet](https://grafana.github.io/grafonnet/index.html) (Jsonnet-based templating).

## Prerequisites

Install the required tools:

```bash
# macOS
brew install jsonnet jsonnet-bundler

# Or via Go
go install github.com/google/go-jsonnet/cmd/jsonnet@latest
go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@latest
```

## Quick Start

```bash
cd infra/grafana

# Install Grafonnet dependencies (one-time)
make install

# Build all dashboards and alerts
make build

# Output will be in dist/
ls dist/
```

## Project Structure

```
grafana/
├── Makefile              # Build commands
├── jsonnetfile.json      # Grafonnet dependencies
├── lib/                  # Shared libraries
│   ├── config.libsonnet  # Data sources, thresholds, colors
│   └── panels.libsonnet  # Reusable panel definitions
├── dashboards/           # Dashboard definitions
│   ├── overview.jsonnet              # Main overview dashboard
│   ├── frontend-deep-dive.jsonnet    # Next.js detailed metrics
│   ├── backend-deep-dive.jsonnet     # Go API detailed metrics
│   └── infrastructure-deep-dive.jsonnet  # Container metrics
├── alerts/               # Alert rule definitions
│   └── alerts.jsonnet    # All alert rules
└── dist/                 # Build output (gitignored)
    ├── overview.json
    ├── frontend-deep-dive.json
    ├── backend-deep-dive.json
    ├── infrastructure-deep-dive.json
    └── alerts/
        └── alerts.json
```

## Dashboards

### Overview Dashboard (`openmentor-overview`)
**Purpose:** Daily monitoring, quick health check

- Key stats: request rates, error rates, latency (P99)
- Traffic & performance: request rates, latency percentiles, HTTP status codes
- Business metrics: page views, contact submissions, top mentor profiles
- Infrastructure: container CPU/memory, runtime metrics
- Error logs: recent errors from all services

### Frontend Deep Dive (`openmentor-frontend-deep-dive`)
**Purpose:** Investigation when frontend issues occur

- Request performance: by route, method, with heatmaps
- SSR performance: duration by page, status breakdown
- Node.js runtime: event loop lag, heap memory, GC, CPU
- Business metrics: page views, mentor profile views table
- Error analysis: error rates by route, error tables, logs

### Backend Deep Dive (`openmentor-backend-deep-dive`)
**Purpose:** Investigation when backend issues occur

- Request performance: by route, method, with heatmaps
- External dependencies: PostgreSQL latency/errors, S3 object storage metrics
- Cache performance: hit ratio, operations rate, cache size
- Go runtime: goroutines, heap memory, GC
- Business metrics: contact forms, profile updates, picture uploads
- Error analysis: error rates, error tables, logs

### Infrastructure Deep Dive (`openmentor-infra-deep-dive`)
**Purpose:** Resource monitoring and capacity planning

- Container overview: running containers, total CPU/memory, network rates
- Container CPU: usage by container, user vs system, throttling
- Container memory: usage, working set, cache
- Container network: receive/transmit rates, errors, dropped packets
- Container disk I/O: read/write rates
- Grafana Alloy: scrape targets, duration, samples

## Alerts

Alert rules are grouped by category:

| Group | Alerts |
|-------|--------|
| **Availability** | Frontend/Backend service down |
| **Performance** | High error rate (>5%), high latency (P99) |
| **Infrastructure** | High CPU (>90%), high memory (>1GB), high goroutines, event loop lag |
| **Dependencies** | PostgreSQL high latency/errors, low cache hit ratio |

Alert severity levels:
- `critical`: Requires immediate attention
- `warning`: Should be investigated soon

## Configuration

Edit `lib/config.libsonnet` to customize:

```jsonnet
{
  // Data sources - update these to match your Grafana Cloud
  datasources: {
    prometheus: 'grafanacloud-glamcoder-prom',
    loki: 'grafanacloud-glamcoder-logs',
    tempo: 'grafanacloud-glamcoder-traces',
    pyroscope: 'grafanacloud-glamcoder-profiles',
  },

  // Thresholds for alerts and visualizations
  thresholds: {
    errorRate: { warning: 0.01, critical: 0.05 },
    latencyP99: { warning: 1, critical: 3 },
    // ...
  },
}
```

## Importing to Grafana

### Dashboards

1. Build the dashboards: `make build`
2. In Grafana, go to **Dashboards** → **Import**
3. Upload or paste the JSON from `dist/*.json`
4. Select the appropriate folder and import

### Alert Rules

1. Build the alerts: `make build`
2. In Grafana, go to **Alerting** → **Alert rules**
3. Use the Grafana API or Terraform to import, or manually create based on the JSON

Alternatively, use Grafana's provisioning:

```yaml
# /etc/grafana/provisioning/dashboards/openmentor.yaml
apiVersion: 1
providers:
  - name: OpenMentor
    folder: OpenMentor
    type: file
    options:
      path: /path/to/dist
```

## Development

```bash
# Watch for changes and rebuild
make watch

# Validate syntax
make lint

# Clean build artifacts
make clean
```

## Notification Channels

The alerts are configured to use:
- Email
- Telegram

Configure these in Grafana Cloud under **Alerting** → **Contact points**.

## Trace Links

Dashboards include links to Tempo for trace investigation. Click on a trace link to open Grafana Explore with traces filtered by service name.

## Profiling Quick Links

Continuous profiling is Explore-first in phase one. Use Grafana Explore with the Pyroscope datasource:

- Profiles datasource: `grafanacloud-glamcoder-profiles`
- Primary selector: `service_name="openmentor-api"`
- Helpful filters: `environment="production"` or `environment="staging"`

Recommended starting views:
- CPU flame graph for `openmentor-api`
- Allocation profile (`alloc_space`, `alloc_objects`) for memory pressure checks
- Mutex/block profiles during latency spikes

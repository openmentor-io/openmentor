// OpenMentor Alert Rules
// Prometheus alerting rules for Grafana Cloud Alerting
//
// Categories:
// 1. Availability - service up/down
// 2. Performance - latency, error rates
// 3. Infrastructure - CPU, memory, runtime
// 4. Business - unusual patterns
// 5. Dependencies - cache health

local config = import '../lib/config.libsonnet';

// Helper to build a standard alert rule with reduce + threshold
local alertRule(uid, title, expr, forDuration='5m', noDataState='OK', execErrState='Error', annotations={}, labels={}) = {
  uid: uid,
  title: title,
  condition: 'C',
  data: [
    {
      refId: 'A',
      relativeTimeRange: { from: 600, to: 0 },
      datasourceUid: config.datasources.prometheus,
      model: {
        expr: expr,
        instant: true,
        refId: 'A',
      },
    },
    {
      refId: 'B',
      datasourceUid: '__expr__',
      model: {
        type: 'reduce',
        expression: 'A',
        reducer: 'last',
        refId: 'B',
      },
    },
    {
      refId: 'C',
      datasourceUid: '__expr__',
      model: {
        type: 'threshold',
        expression: 'B',
        conditions: [
          {
            evaluator: { type: 'gt', params: [0] },
            operator: { type: 'and' },
            reducer: { type: 'last' },
          },
        ],
        refId: 'C',
      },
    },
  ],
  noDataState: noDataState,
  execErrState: execErrState,
  'for': forDuration,
  annotations: annotations,
  labels: labels,
};

// Alert rule group definition for Grafana Cloud
{
  apiVersion: 1,
  groups: [
    // ==========================================
    // Availability Alerts
    // ==========================================
    {
      name: 'openmentor-availability',
      folder: 'OpenMentor',
      interval: '1m',
      rules: [
        alertRule(
          'frontend-down',
          'Frontend Service Down',
          'up{job=~".*nextjs.*"} == 0',
          '2m',
          'Alerting',
          'Alerting',
          {
            summary: 'Frontend service is down',
            description: 'The Next.js frontend service has been unreachable for more than 2 minutes.',
          },
          {
            severity: 'critical',
            service: 'frontend',
            team: 'openmentor',
          },
        ),
        alertRule(
          'backend-down',
          'Backend Service Down',
          'up{job=~".*go-api.*"} == 0',
          '2m',
          'Alerting',
          'Alerting',
          {
            summary: 'Backend service is down',
            description: 'The Go API backend service has been unreachable for more than 2 minutes.',
          },
          {
            severity: 'critical',
            service: 'backend',
            team: 'openmentor',
          },
        ),
      ],
    },

    // ==========================================
    // Performance Alerts
    // ==========================================
    {
      name: 'openmentor-performance',
      folder: 'OpenMentor',
      interval: '1m',
      rules: [
        alertRule(
          'frontend-high-error-rate',
          'Frontend High Error Rate',
          |||
            (
              sum(rate(http_server_request_total{http_response_status_code=~"5..",service_name="openmentor-frontend"}[5m])) /
              sum(rate(http_server_request_total{service_name="openmentor-frontend"}[5m]))
            ) * 100 > 5
          |||,
          '5m',
          'OK',
          'Error',
          {
            summary: 'Frontend error rate is above 5%',
            description: 'The frontend service has an error rate of {{ $value | printf "%.2f" }}% over the last 5 minutes.',
          },
          {
            severity: 'critical',
            service: 'frontend',
            team: 'openmentor',
          },
        ),
        alertRule(
          'backend-high-error-rate',
          'Backend High Error Rate',
          |||
            (
              sum(rate(http_server_request_total{http_response_status_code=~"5..",service_name="openmentor-api"}[5m])) /
              sum(rate(http_server_request_total{service_name="openmentor-api"}[5m]))
            ) * 100 > 5
          |||,
          '5m',
          'OK',
          'Error',
          {
            summary: 'Backend error rate is above 5%',
            description: 'The backend service has an error rate of {{ $value | printf "%.2f" }}% over the last 5 minutes.',
          },
          {
            severity: 'critical',
            service: 'backend',
            team: 'openmentor',
          },
        ),
        alertRule(
          'frontend-high-latency',
          'Frontend High Latency (P99)',
          'histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-frontend"}[5m])) by (le)) > 3',
          '5m',
          'OK',
          'Error',
          {
            summary: 'Frontend P99 latency is above 3 seconds',
            description: 'The frontend service P99 latency is {{ $value | printf "%.2f" }}s, which is above the 3s threshold.',
          },
          {
            severity: 'warning',
            service: 'frontend',
            team: 'openmentor',
          },
        ),
        alertRule(
          'backend-high-latency',
          'Backend High Latency (P99)',
          'histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-api"}[5m])) by (le)) > 1',
          '5m',
          'OK',
          'Error',
          {
            summary: 'Backend P99 latency is above 1 second',
            description: 'The backend service P99 latency is {{ $value | printf "%.2f" }}s, which is above the 1s threshold.',
          },
          {
            severity: 'warning',
            service: 'backend',
            team: 'openmentor',
          },
        ),
      ],
    },

    // ==========================================
    // Infrastructure Alerts
    // ==========================================
    {
      name: 'openmentor-infrastructure',
      folder: 'OpenMentor',
      interval: '1m',
      rules: [
        alertRule(
          'container-high-cpu',
          'Container High CPU Usage',
          'sum by (name) (rate(container_cpu_usage_seconds_total{name=~"openmentor.*"}[5m])) * 100 > 90',
          '5m',
          'OK',
          'Error',
          {
            summary: 'Container CPU usage is above 90%',
            description: 'Container {{ $labels.name }} has CPU usage of {{ $value | printf "%.1f" }}%.',
          },
          {
            severity: 'warning',
            service: 'infrastructure',
            team: 'openmentor',
          },
        ),
        alertRule(
          'container-high-memory',
          'Container High Memory Usage',
          'container_memory_usage_bytes{name=~"openmentor.*"} > 1e9',
          '5m',
          'OK',
          'Error',
          {
            summary: 'Container memory usage is above 1GB',
            description: 'Container {{ $labels.name }} is using {{ $value | humanize1024 }}B of memory.',
          },
          {
            severity: 'warning',
            service: 'infrastructure',
            team: 'openmentor',
          },
        ),
        alertRule(
          'backend-high-goroutines',
          'Backend High Goroutine Count',
          'process_runtime_go_goroutines{service_name="openmentor-api"} > 1000',
          '5m',
          'OK',
          'Error',
          {
            summary: 'Backend goroutine count is unusually high',
            description: 'The backend service has {{ $value }} goroutines, which may indicate a goroutine leak.',
          },
          {
            severity: 'warning',
            service: 'backend',
            team: 'openmentor',
          },
        ),
        alertRule(
          'frontend-high-event-loop-lag',
          'Frontend High Event Loop Lag',
          'nodejs_eventloop_lag_seconds{runtime="nodejs",service_name="openmentor-frontend"} > 0.1',
          '5m',
          'OK',
          'Error',
          {
            summary: 'Frontend event loop lag is above 100ms',
            description: 'The Node.js event loop lag is {{ $value | printf "%.3f" }}s, indicating the event loop is blocked.',
          },
          {
            severity: 'warning',
            service: 'frontend',
            team: 'openmentor',
          },
        ),
      ],
    },

    // ==========================================
    // Dependency Alerts
    // ==========================================
    {
      name: 'openmentor-dependencies',
      folder: 'OpenMentor',
      interval: '1m',
      rules: [
        alertRule(
          'cache-low-hit-ratio',
          'Cache Low Hit Ratio',
          |||
            (
              sum(rate(cache_hits_total{service_name="openmentor-api"}[5m])) /
              (sum(rate(cache_hits_total{service_name="openmentor-api"}[5m])) + sum(rate(cache_misses_total{service_name="openmentor-api"}[5m])))
            ) * 100 < 80
          |||,
          '10m',
          'OK',
          'Error',
          {
            summary: 'Cache hit ratio is below 80%',
            description: 'The cache hit ratio is {{ $value | printf "%.1f" }}%, which may indicate cache issues or increased database load.',
          },
          {
            severity: 'warning',
            service: 'backend',
            team: 'openmentor',
          },
        ),
      ],
    },

    // ==========================================
    // Business Alerts
    // ==========================================
    {
      name: 'openmentor-business',
      folder: 'OpenMentor',
      interval: '1m',
      rules: [
        alertRule(
          'high-contact-form-failures',
          'High Contact Form Failure Rate',
          |||
            (
              sum(rate(openmentor_contact_form_submissions_total{service_name="openmentor-api",status!="success"}[10m])) /
              sum(rate(openmentor_contact_form_submissions_total{service_name="openmentor-api"}[10m]))
            ) * 100 > 50
          |||,
          '10m',
          'OK',
          'Error',
          {
            summary: 'Contact form failure rate is above 50%',
            description: 'Over 50% of contact form submissions are failing. This may indicate captcha issues or backend errors.',
          },
          {
            severity: 'warning',
            service: 'backend',
            team: 'openmentor',
          },
        ),
        alertRule(
          'high-review-submission-failures',
          'High Review Submission Failure Rate',
          |||
            (
              sum(rate(openmentor_review_submissions_total{service_name="openmentor-api",status!="success"}[10m])) /
              sum(rate(openmentor_review_submissions_total{service_name="openmentor-api"}[10m]))
            ) * 100 > 50
          |||,
          '10m',
          'OK',
          'Error',
          {
            summary: 'Review submission failure rate is above 50%',
            description: 'Over 50% of review submissions are failing. Check captcha and database connectivity.',
          },
          {
            severity: 'warning',
            service: 'backend',
            team: 'openmentor',
          },
        ),
      ],
    },
  ],
}

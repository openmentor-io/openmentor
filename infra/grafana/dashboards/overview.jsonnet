// OpenMentor Overview Dashboard
// High-level overview: health, performance, and business metrics

local g = import 'grafonnet-latest/main.libsonnet';
local config = import '../lib/config.libsonnet';

g.dashboard.new('OpenMentor Overview')
+ g.dashboard.withDescription('High-level overview of OpenMentor application health, performance, and business metrics')
+ g.dashboard.withUid('openmentor-overview')
+ g.dashboard.withTags(config.tags)
+ g.dashboard.withTimezone('browser')
+ g.dashboard.withEditable(true)
+ g.dashboard.time.withFrom(config.timeRange.from)
+ g.dashboard.time.withTo(config.timeRange.to)
+ g.dashboard.withRefresh(config.refresh)
+ g.dashboard.graphTooltip.withSharedCrosshair()

+ g.dashboard.withLinks([
  g.dashboard.link.dashboards.new('Deep Dive Dashboards', ['openmentor', 'deep-dive'])
  + g.dashboard.link.dashboards.options.withAsDropdown(true)
  + g.dashboard.link.dashboards.options.withKeepTime(true),
])

+ g.dashboard.withAnnotations([
  g.dashboard.annotation.withName('Alerts')
  + g.dashboard.annotation.withDatasource({ type: 'prometheus', uid: config.datasources.prometheus })
  + g.dashboard.annotation.withEnable(true)
  + g.dashboard.annotation.withIconColor(config.colors.danger)
  + g.dashboard.annotation.withExpr('ALERTS{alertstate="firing"}'),
])

+ g.dashboard.withPanels([
  // ROW: Key Stats
  g.panel.row.new('Key Stats') + g.panel.row.gridPos.withY(0),

  // Frontend Request Rate
  g.panel.stat.new('Frontend Requests/s')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(http_server_request_total{service_name="openmentor-frontend"}[5m]))')
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('reqps')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(0) + g.panel.stat.gridPos.withY(1),

  // Backend Request Rate
  g.panel.stat.new('Backend Requests/s')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(http_server_request_total{service_name="openmentor-api"}[5m]))')
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('reqps')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(4) + g.panel.stat.gridPos.withY(1),

  // Frontend Error Rate
  g.panel.stat.new('Frontend Error Rate')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(http_server_request_total{service_name="openmentor-frontend",http_response_status_code=~"5.."}[5m])) / sum(rate(http_server_request_total{service_name="openmentor-frontend"}[5m])) * 100')
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('percent')
  + g.panel.stat.standardOptions.withDecimals(2)
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 1 },
    { color: config.colors.danger, value: 5 },
  ])
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(8) + g.panel.stat.gridPos.withY(1),

  // Backend Error Rate
  g.panel.stat.new('Backend Error Rate')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(http_server_request_total{service_name="openmentor-api",http_response_status_code=~"5.."}[5m])) / sum(rate(http_server_request_total{service_name="openmentor-api"}[5m])) * 100')
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('percent')
  + g.panel.stat.standardOptions.withDecimals(2)
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 1 },
    { color: config.colors.danger, value: 5 },
  ])
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(12) + g.panel.stat.gridPos.withY(1),

  // Frontend P99 Latency
  g.panel.stat.new('Frontend P99 Latency')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-frontend"}[5m])) by (le))')
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('s')
  + g.panel.stat.standardOptions.withDecimals(2)
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 1 },
    { color: config.colors.danger, value: 3 },
  ])
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(16) + g.panel.stat.gridPos.withY(1),

  // Backend P99 Latency
  g.panel.stat.new('Backend P99 Latency')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-api"}[5m])) by (le))')
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('s')
  + g.panel.stat.standardOptions.withDecimals(2)
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 0.5 },
    { color: config.colors.danger, value: 1 },
  ])
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(20) + g.panel.stat.gridPos.withY(1),

  // ROW: Traffic & Performance
  g.panel.row.new('Traffic & Performance') + g.panel.row.gridPos.withY(5),

  // Request Rate by Service
  g.panel.timeSeries.new('Request Rate by Service')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(http_server_request_total{service_name="openmentor-frontend"}[5m]))')
    + g.query.prometheus.withLegendFormat('Frontend'),
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(http_server_request_total{service_name="openmentor-api"}[5m]))')
    + g.query.prometheus.withLegendFormat('Backend'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('right')
  + g.panel.timeSeries.options.legend.withCalcs(['mean', 'max', 'last'])
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(6),

  // Latency Percentiles
  g.panel.timeSeries.new('Latency Percentiles')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.50, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-frontend"}[5m])) by (le))')
    + g.query.prometheus.withLegendFormat('Frontend p50'),
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-frontend"}[5m])) by (le))')
    + g.query.prometheus.withLegendFormat('Frontend p99'),
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.50, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-api"}[5m])) by (le))')
    + g.query.prometheus.withLegendFormat('Backend p50'),
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket{service_name="openmentor-api"}[5m])) by (le))')
    + g.query.prometheus.withLegendFormat('Backend p99'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('right')
  + g.panel.timeSeries.options.legend.withCalcs(['mean', 'max'])
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(6),

  // HTTP Status Codes - Frontend
  g.panel.timeSeries.new('Frontend HTTP Status Codes')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_response_status_code) (rate(http_server_request_total{service_name="openmentor-frontend"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{http_response_status_code}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.fieldConfig.defaults.custom.withStacking({ mode: 'normal' })
  + g.panel.timeSeries.fieldConfig.defaults.custom.withFillOpacity(80)
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(14),

  // HTTP Status Codes - Backend
  g.panel.timeSeries.new('Backend HTTP Status Codes')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_response_status_code) (rate(http_server_request_total{service_name="openmentor-api"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{http_response_status_code}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.fieldConfig.defaults.custom.withStacking({ mode: 'normal' })
  + g.panel.timeSeries.fieldConfig.defaults.custom.withFillOpacity(80)
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(14),

  // ROW: Business Metrics
  g.panel.row.new('Business Metrics') + g.panel.row.gridPos.withY(22),

  // Page Views by Page
  g.panel.timeSeries.new('Page Views by Page')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (page) (rate(nextjs_page_views_total{service_name="openmentor-frontend"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{page}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('right')
  + g.panel.timeSeries.gridPos.withW(8) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(23),

  // Contact Form Submissions
  g.panel.timeSeries.new('Contact Form Submissions')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_contact_form_submissions_total{service_name="openmentor-api"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.gridPos.withW(8) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(8) + g.panel.timeSeries.gridPos.withY(23),

  // View -> Contact Conversion Rate (key business funnel metric)
  g.panel.stat.new('View -> Contact Conversion')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(increase(nextjs_page_views_total{page=~"mentor-contact"}[$__rate_interval])) / sum(increase(openmentor_mentor_profile_views_total[$__rate_interval]))')
    + g.query.prometheus.withInstant(true)
    + g.query.prometheus.withLegendFormat('Conversion'),
  ])
  + g.panel.stat.standardOptions.withUnit('percentunit')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.options.withGraphMode('area')
  + g.panel.stat.gridPos.withW(8) + g.panel.stat.gridPos.withH(8) + g.panel.stat.gridPos.withX(16) + g.panel.stat.gridPos.withY(23),

  // ROW: Product Metrics
  g.panel.row.new('Product Metrics') + g.panel.row.gridPos.withY(31),

  // Mentor Profile Views
  g.panel.timeSeries.new('Mentor Profile Views')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(openmentor_mentor_profile_views_total{service_name="openmentor-frontend"}[5m]))')
    + g.query.prometheus.withLegendFormat('Profile Views'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.gridPos.withW(6) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(32),

  // Mentor Registrations
  g.panel.timeSeries.new('Mentor Registrations')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_mentor_registrations_total{service_name="openmentor-api"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.gridPos.withW(6) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(6) + g.panel.timeSeries.gridPos.withY(32),

  // Review Submissions
  g.panel.timeSeries.new('Review Submissions')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_review_submissions_total{service_name="openmentor-api"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.gridPos.withW(6) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(32),

  // Mentor Searches
  g.panel.timeSeries.new('Mentor Searches')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (search_type) (rate(openmentor_mentor_searches_total{service_name="openmentor-frontend"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{search_type}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.gridPos.withW(6) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(18) + g.panel.timeSeries.gridPos.withY(32),

  // ROW: Infrastructure
  g.panel.row.new('Infrastructure') + g.panel.row.gridPos.withY(40),

  // Container CPU Usage
  g.panel.timeSeries.new('Container CPU Usage')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_cpu_usage_seconds_total{name=~"openmentor-frontend|openmentor-backend|grafana-alloy"}[5m])) * 100')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('percent')
  + g.panel.timeSeries.standardOptions.withMin(0)
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('right')
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(41),

  // Container Memory Usage
  g.panel.timeSeries.new('Container Memory Usage')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'container_memory_usage_bytes{name=~"openmentor-frontend|openmentor-backend|grafana-alloy"}')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('bytes')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('right')
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(41),

  // ROW: Error Logs
  g.panel.row.new('Recent Errors')
  + g.panel.row.withCollapsed(true)
  + g.panel.row.gridPos.withY(49)
  + g.panel.row.withPanels([
    g.panel.logs.new('Error Logs (All Services)')
    + g.panel.logs.queryOptions.withDatasource('loki', config.datasources.loki)
    + g.panel.logs.queryOptions.withTargets([
      g.query.loki.new(config.datasources.loki, '{service_name=~"openmentor-.*"} |~ "(?i)error" | json'),
    ])
    + g.panel.logs.options.withShowTime(true)
    + g.panel.logs.options.withWrapLogMessage(true)
    + g.panel.logs.options.withEnableLogDetails(true)
    + g.panel.logs.options.withSortOrder('Descending')
    + g.panel.logs.gridPos.withW(24) + g.panel.logs.gridPos.withH(10) + g.panel.logs.gridPos.withX(0) + g.panel.logs.gridPos.withY(50),
  ]),
])

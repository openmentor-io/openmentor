// OpenMentor Frontend Deep Dive Dashboard
// Detailed Next.js frontend metrics for investigation

local g = import 'grafonnet-latest/main.libsonnet';
local config = import '../lib/config.libsonnet';

local tsDefaults = g.panel.timeSeries.options.legend.withDisplayMode('table')
                   + g.panel.timeSeries.options.legend.withPlacement('right')
                   + g.panel.timeSeries.options.legend.withCalcs(['mean', 'max', 'last']);

// Service filter
local svc = 'service_name="openmentor-frontend"';

g.dashboard.new('OpenMentor Frontend Deep Dive')
+ g.dashboard.withDescription('Detailed Next.js frontend metrics for performance investigation and debugging')
+ g.dashboard.withUid('openmentor-frontend-deep-dive')
+ g.dashboard.withTags(config.tags + ['deep-dive', 'frontend'])
+ g.dashboard.withTimezone('browser')
+ g.dashboard.withEditable(true)
+ g.dashboard.time.withFrom(config.timeRange.from)
+ g.dashboard.time.withTo(config.timeRange.to)
+ g.dashboard.withRefresh(config.refresh)
+ g.dashboard.graphTooltip.withSharedCrosshair()

+ g.dashboard.withVariables([
  g.dashboard.variable.query.new('route')
  + g.dashboard.variable.query.withDatasource('prometheus', config.datasources.prometheus)
  + g.dashboard.variable.query.queryTypes.withLabelValues('http_route', 'http_server_request_total{service_name="openmentor-frontend"}')
  + g.dashboard.variable.query.withRefresh('time')
  + g.dashboard.variable.query.selectionOptions.withMulti(true)
  + g.dashboard.variable.query.selectionOptions.withIncludeAll(true),
])

+ g.dashboard.withLinks([
  g.dashboard.link.dashboards.new('Overview', ['openmentor'])
  + g.dashboard.link.dashboards.options.withKeepTime(true),
])

+ g.dashboard.withPanels([
  // ===== ROW: Request Performance =====
  g.panel.row.new('Request Performance') + g.panel.row.gridPos.withY(0),

  g.panel.timeSeries.new('Request Rate by Route')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_route) (rate(http_server_request_total{%s,http_route=~"$route"}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{http_route}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(1),

  g.panel.timeSeries.new('Latency by Route (p95)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_request_duration_seconds_bucket{%s,http_route=~"$route"}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('{{http_route}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(1),

  g.panel.timeSeries.new('HTTP Status Codes')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_response_status_code) (rate(http_server_request_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{http_response_status_code}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.fieldConfig.defaults.custom.withStacking({ mode: 'normal' })
  + g.panel.timeSeries.fieldConfig.defaults.custom.withFillOpacity(80)
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(9),

  // Frontend active_requests has http_route label (unlike backend)
  g.panel.timeSeries.new('Active Requests')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_route) (http_server_active_requests{%s})' % svc)
    + g.query.prometheus.withLegendFormat('{{http_route}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('short')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(9),

  // ===== ROW: SSR Performance =====
  g.panel.row.new('Server-Side Rendering') + g.panel.row.gridPos.withY(17),

  g.panel.timeSeries.new('SSR Duration by Page (p95)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le, page) (rate(nextjs_ssr_duration_seconds_bucket{%s}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('{{page}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(18),

  g.panel.timeSeries.new('Page Views')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (page) (rate(nextjs_page_views_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{page}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(18),

  // ===== ROW: Business Metrics =====
  g.panel.row.new('Business Metrics') + g.panel.row.gridPos.withY(26),

  // Mentor Profile Views (total, no per-mentor breakdown)
  g.panel.timeSeries.new('Mentor Profile Views')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'rate(openmentor_mentor_profile_views_total{%s}[5m])' % svc)
    + g.query.prometheus.withLegendFormat('Profile Views'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(27),

  // Mentor Searches
  g.panel.timeSeries.new('Mentor Searches')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (search_type) (rate(openmentor_mentor_searches_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{search_type}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(27),

  // ===== ROW: Node.js Runtime =====
  g.panel.row.new('Node.js Runtime') + g.panel.row.gridPos.withY(35),

  g.panel.timeSeries.new('Event Loop Lag')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'nodejs_eventloop_lag_seconds{runtime="nodejs",%s}' % svc)
    + g.query.prometheus.withLegendFormat('Lag'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(8) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(36),

  g.panel.timeSeries.new('Heap Memory')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'nodejs_heap_size_used_bytes{runtime="nodejs",%s}' % svc)
    + g.query.prometheus.withLegendFormat('Used'),
    g.query.prometheus.new(config.datasources.prometheus, 'nodejs_heap_size_total_bytes{runtime="nodejs",%s}' % svc)
    + g.query.prometheus.withLegendFormat('Total'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('bytes')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(8) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(8) + g.panel.timeSeries.gridPos.withY(36),

  g.panel.timeSeries.new('Process CPU')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'rate(process_cpu_user_seconds_total{runtime="nodejs",%s}[5m]) * 100' % svc)
    + g.query.prometheus.withLegendFormat('User'),
    g.query.prometheus.new(config.datasources.prometheus, 'rate(process_cpu_system_seconds_total{runtime="nodejs",%s}[5m]) * 100' % svc)
    + g.query.prometheus.withLegendFormat('System'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('percent')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(8) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(16) + g.panel.timeSeries.gridPos.withY(36),

  // ===== ROW: Error Analysis =====
  g.panel.row.new('Error Analysis') + g.panel.row.gridPos.withY(44),

  g.panel.timeSeries.new('Error Rate by Route')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_route) (rate(http_server_request_total{%s,http_response_status_code=~"5.."}[5m])) / sum by (http_route) (rate(http_server_request_total{%s}[5m])) * 100' % [svc, svc])
    + g.query.prometheus.withLegendFormat('{{http_route}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('percent')
  + tsDefaults
  + g.panel.timeSeries.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 1 },
    { color: config.colors.danger, value: 5 },
  ])
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(45),

  // Errors by Route & Status Table
  g.panel.table.new('Errors by Route & Status (1h)')
  + g.panel.table.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.table.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'topk(20, sum by (http_route, http_response_status_code) (increase(http_server_request_total{%s,http_response_status_code=~"[45].."}[1h])))' % svc)
    + g.query.prometheus.withInstant(true)
    + g.query.prometheus.withFormat('table'),
  ])
  + g.panel.table.gridPos.withW(12) + g.panel.table.gridPos.withH(8) + g.panel.table.gridPos.withX(12) + g.panel.table.gridPos.withY(45),

  g.panel.logs.new('Frontend Error Logs')
  + g.panel.logs.queryOptions.withDatasource('loki', config.datasources.loki)
  + g.panel.logs.queryOptions.withTargets([
    g.query.loki.new(config.datasources.loki, '{service_name="openmentor-frontend"} |~ "(?i)error" | json'),
  ])
  + g.panel.logs.options.withShowTime(true)
  + g.panel.logs.options.withWrapLogMessage(true)
  + g.panel.logs.options.withEnableLogDetails(true)
  + g.panel.logs.options.withSortOrder('Descending')
  + g.panel.logs.gridPos.withW(24) + g.panel.logs.gridPos.withH(10) + g.panel.logs.gridPos.withX(0) + g.panel.logs.gridPos.withY(53),
])

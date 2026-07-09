// OpenMentor Backend Deep Dive Dashboard
// Detailed Go API backend metrics for investigation

local g = import 'grafonnet-latest/main.libsonnet';
local config = import '../lib/config.libsonnet';

// Helper for common timeseries config
local tsDefaults = g.panel.timeSeries.options.legend.withDisplayMode('table')
                   + g.panel.timeSeries.options.legend.withPlacement('right')
                   + g.panel.timeSeries.options.legend.withCalcs(['mean', 'max', 'last']);

// Helper to set grid position
local pos(w, h, x, y) = g.panel.timeSeries.gridPos.withW(w) + g.panel.timeSeries.gridPos.withH(h) + g.panel.timeSeries.gridPos.withX(x) + g.panel.timeSeries.gridPos.withY(y);

// Service filter
local svc = 'service_name="openmentor-api"';

g.dashboard.new('OpenMentor Backend Deep Dive')
+ g.dashboard.withDescription('Detailed Go API backend metrics for performance investigation and debugging')
+ g.dashboard.withUid('openmentor-backend-deep-dive')
+ g.dashboard.withTags(config.tags + ['deep-dive', 'backend'])
+ g.dashboard.withTimezone('browser')
+ g.dashboard.withEditable(true)
+ g.dashboard.time.withFrom(config.timeRange.from)
+ g.dashboard.time.withTo(config.timeRange.to)
+ g.dashboard.withRefresh(config.refresh)
+ g.dashboard.graphTooltip.withSharedCrosshair()

// Template variables
+ g.dashboard.withVariables([
  g.dashboard.variable.query.new('route')
  + g.dashboard.variable.query.withDatasource('prometheus', config.datasources.prometheus)
  + g.dashboard.variable.query.queryTypes.withLabelValues('http_route', 'http_server_request_total{service_name="openmentor-api"}')
  + g.dashboard.variable.query.withRefresh('time')
  + g.dashboard.variable.query.selectionOptions.withMulti(true)
  + g.dashboard.variable.query.selectionOptions.withIncludeAll(true),
])

// Links
+ g.dashboard.withLinks([
  g.dashboard.link.dashboards.new('Overview', ['openmentor'])
  + g.dashboard.link.dashboards.options.withKeepTime(true),
])

// Panels
+ g.dashboard.withPanels([
  // ===== ROW: Request Performance =====
  g.panel.row.new('Request Performance') + g.panel.row.gridPos.withY(0),

  // Request Rate by Route
  g.panel.timeSeries.new('Request Rate by Route')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_route) (rate(http_server_request_total{%s,http_route=~"$route"}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{http_route}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(12, 8, 0, 1),

  // Latency by Route (p95)
  g.panel.timeSeries.new('Latency by Route (p95)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_request_duration_seconds_bucket{%s,http_route=~"$route"}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('{{http_route}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + pos(12, 8, 12, 1),

  // HTTP Status Codes
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
  + pos(12, 8, 0, 9),

  // Active Requests (only has http_request_method label on backend)
  g.panel.timeSeries.new('Active Requests')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (http_request_method) (http_server_active_requests{%s})' % svc)
    + g.query.prometheus.withLegendFormat('{{http_request_method}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('short')
  + tsDefaults
  + pos(12, 8, 12, 9),

  // ===== ROW: Cache Performance =====
  g.panel.row.new('Cache Performance') + g.panel.row.gridPos.withY(17),

  // Cache Hit Ratio
  g.panel.timeSeries.new('Cache Hit Ratio')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (cache_name) (rate(cache_hits_total{%s}[5m])) / (sum by (cache_name) (rate(cache_hits_total{%s}[5m])) + sum by (cache_name) (rate(cache_misses_total{%s}[5m]))) * 100' % [svc, svc, svc])
    + g.query.prometheus.withLegendFormat('{{cache_name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('percent')
  + g.panel.timeSeries.standardOptions.withMin(0)
  + g.panel.timeSeries.standardOptions.withMax(100)
  + tsDefaults
  + pos(8, 8, 0, 18),

  // Cache Hits/Misses Rate
  g.panel.timeSeries.new('Cache Operations Rate')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (cache_name) (rate(cache_hits_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{cache_name}} hits'),
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (cache_name) (rate(cache_misses_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{cache_name}} misses'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('ops')
  + tsDefaults
  + pos(8, 8, 8, 18),

  // Cache Size
  g.panel.timeSeries.new('Cache Size (Items)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'cache_entries{%s}' % svc)
    + g.query.prometheus.withLegendFormat('{{cache_name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('short')
  + tsDefaults
  + pos(8, 8, 16, 18),

  // ===== ROW: Storage Operations =====
  g.panel.row.new('External Storage') + g.panel.row.gridPos.withY(26),

  // S3 Storage Operations
  g.panel.timeSeries.new('S3 Storage Operations')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (operation, status) (rate(s3_storage_operation_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{operation}} ({{status}})'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('ops')
  + tsDefaults
  + pos(12, 8, 0, 27),

  // S3 Storage Latency
  g.panel.timeSeries.new('S3 Storage Latency (p95)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le, operation) (rate(s3_storage_operation_duration_seconds_bucket{%s}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('{{operation}} p95'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + pos(12, 8, 12, 27),

  // ===== ROW: Auth Metrics =====
  g.panel.row.new('Authentication') + g.panel.row.gridPos.withY(35),

  // Login Requests
  g.panel.timeSeries.new('Login Requests')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_mentor_auth_login_requests_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 0, 36),

  // Verify Requests
  g.panel.timeSeries.new('Token Verify Requests')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_mentor_auth_verify_requests_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 8, 36),

  // Auth Duration
  g.panel.timeSeries.new('Auth Latency (p95)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le) (rate(openmentor_mentor_auth_login_duration_seconds_bucket{%s}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('Login p95'),
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le) (rate(openmentor_mentor_auth_verify_duration_seconds_bucket{%s}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('Verify p95'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + pos(8, 8, 16, 36),

  // ===== ROW: Business Metrics =====
  g.panel.row.new('Business Metrics') + g.panel.row.gridPos.withY(44),

  // Contact Form Submissions
  g.panel.timeSeries.new('Contact Form Submissions')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_contact_form_submissions_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 0, 45),

  // Profile Updates
  g.panel.timeSeries.new('Profile Updates')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_profile_updates_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 8, 45),

  // Profile Picture Uploads
  g.panel.timeSeries.new('Profile Picture Uploads')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_profile_picture_uploads_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 16, 45),

  // ===== ROW: Mentor Registrations & Reviews =====
  g.panel.row.new('Registrations & Reviews') + g.panel.row.gridPos.withY(53),

  // Mentor Registrations
  g.panel.timeSeries.new('Mentor Registrations')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_mentor_registrations_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 0, 54),

  // Review Submissions
  g.panel.timeSeries.new('Review Submissions')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (status) (rate(openmentor_review_submissions_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 8, 54),

  // Review Submission Duration
  g.panel.timeSeries.new('Review Submission Latency (p95)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'histogram_quantile(0.95, sum by (le) (rate(openmentor_review_submission_duration_seconds_bucket{%s}[5m])))' % svc)
    + g.query.prometheus.withLegendFormat('p95'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('s')
  + tsDefaults
  + pos(8, 8, 16, 54),

  // ===== ROW: Request Management =====
  g.panel.row.new('Request Management') + g.panel.row.gridPos.withY(62),

  // Request Status Updates
  g.panel.timeSeries.new('Request Status Updates')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (from_status, to_status) (rate(openmentor_mentor_requests_status_updates_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{from_status}} → {{to_status}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 0, 63),

  // Request Declines by Reason
  g.panel.timeSeries.new('Request Declines by Reason')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (reason) (rate(openmentor_mentor_requests_declines_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{reason}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 8, 63),

  // Review Eligibility Checks
  g.panel.timeSeries.new('Review Eligibility Checks')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (result) (rate(openmentor_review_checks_total{%s}[5m]))' % svc)
    + g.query.prometheus.withLegendFormat('{{result}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('reqps')
  + tsDefaults
  + pos(8, 8, 16, 63),

  // ===== ROW: Go Runtime =====
  g.panel.row.new('Go Runtime') + g.panel.row.gridPos.withY(72),

  // Goroutines
  g.panel.timeSeries.new('Goroutines')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'process_runtime_go_goroutines{%s}' % svc)
    + g.query.prometheus.withLegendFormat('Goroutines'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('short')
  + tsDefaults
  + pos(12, 8, 0, 73),

  // Heap Memory
  g.panel.timeSeries.new('Heap Memory')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'process_runtime_go_mem_heap_alloc_bytes{%s}' % svc)
    + g.query.prometheus.withLegendFormat('Heap Allocated'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('bytes')
  + tsDefaults
  + pos(12, 8, 12, 73),

  // ===== ROW: Error Analysis =====
  g.panel.row.new('Error Analysis') + g.panel.row.gridPos.withY(81),

  // Error Rate by Route
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
  + pos(12, 8, 0, 82),

  // Errors by Route & Status Table
  g.panel.table.new('Errors by Route & Status (1h)')
  + g.panel.table.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.table.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'topk(20, sum by (http_route, http_response_status_code) (increase(http_server_request_total{%s,http_response_status_code=~"[45].."}[1h])))' % svc)
    + g.query.prometheus.withInstant(true)
    + g.query.prometheus.withFormat('table'),
  ])
  + g.panel.table.gridPos.withW(12)
  + g.panel.table.gridPos.withH(8)
  + g.panel.table.gridPos.withX(12)
  + g.panel.table.gridPos.withY(82),

  // Backend Error Logs
  g.panel.logs.new('Backend Error Logs')
  + g.panel.logs.queryOptions.withDatasource('loki', config.datasources.loki)
  + g.panel.logs.queryOptions.withTargets([
    g.query.loki.new(config.datasources.loki, '{service_name="openmentor-api"} |~ "(?i)error" | json'),
  ])
  + g.panel.logs.options.withShowTime(true)
  + g.panel.logs.options.withWrapLogMessage(true)
  + g.panel.logs.options.withEnableLogDetails(true)
  + g.panel.logs.options.withSortOrder('Descending')
  + g.panel.logs.gridPos.withW(24)
  + g.panel.logs.gridPos.withH(10)
  + g.panel.logs.gridPos.withX(0)
  + g.panel.logs.gridPos.withY(90),
])

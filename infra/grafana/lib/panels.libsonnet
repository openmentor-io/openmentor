// OpenMentor Panel Library
// Reusable panel definitions for dashboards
//
// All metrics use service_name label for filtering (no metric name prefixes).
// Backend: service_name="openmentor-api"
// Frontend: service_name="openmentor-frontend"
//
// HTTP metrics labels: http_request_method, http_route, http_response_status_code

local g = import 'grafonnet-latest/main.libsonnet';
local config = import 'config.libsonnet';

{
  // ============================================
  // STAT PANELS
  // ============================================

  // Generic stat panel
  stat(title, query, unit='short', colorMode='value', thresholds=null)::
    g.panel.stat.new(title)
    + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
    + g.panel.stat.queryOptions.withTargets([
      g.query.prometheus.new(config.datasources.prometheus, query)
      + g.query.prometheus.withInstant(true),
    ])
    + g.panel.stat.standardOptions.withUnit(unit)
    + g.panel.stat.options.withColorMode(colorMode)
    + (if thresholds != null then
         g.panel.stat.standardOptions.thresholds.withSteps(thresholds)
       else {}),

  // Request rate stat
  requestRateStat(title, serviceName)::
    self.stat(
      title,
      'sum(rate(http_server_request_total{service_name="%s"}[5m]))' % serviceName,
      'reqps'
    ),

  // Error rate percentage stat
  errorRateStat(title, serviceName)::
    self.stat(
      title,
      |||
        sum(rate(http_server_request_total{service_name="%s",http_response_status_code=~"5.."}[5m])) /
        sum(rate(http_server_request_total{service_name="%s"}[5m])) * 100
      ||| % [serviceName, serviceName],
      'percent',
      'value',
      [
        { color: config.colors.success, value: null },
        { color: config.colors.warning, value: config.thresholds.errorRate.warning * 100 },
        { color: config.colors.danger, value: config.thresholds.errorRate.critical * 100 },
      ]
    ),

  // ============================================
  // TIME SERIES PANELS
  // ============================================

  // Generic time series panel
  timeseries(title, queries, unit='short', legend='bottom')::
    g.panel.timeSeries.new(title)
    + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
    + g.panel.timeSeries.queryOptions.withTargets(queries)
    + g.panel.timeSeries.standardOptions.withUnit(unit)
    + g.panel.timeSeries.options.legend.withDisplayMode('table')
    + g.panel.timeSeries.options.legend.withPlacement(legend)
    + g.panel.timeSeries.options.legend.withCalcs(['mean', 'max', 'last']),

  // Request rate time series
  requestRateTimeseries(title, serviceName, groupBy='http_response_status_code')::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'sum by (%s) (rate(http_server_request_total{service_name="%s"}[5m]))' % [groupBy, serviceName]
        )
        + g.query.prometheus.withLegendFormat('{{%s}}' % groupBy),
      ],
      'reqps'
    ),

  // Latency percentiles time series
  latencyTimeseries(title, serviceName, routeFilter='')::
    local extraFilter = if routeFilter != '' then ',http_route="%s"' % routeFilter else '';
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'histogram_quantile(0.50, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="%s"%s}[5m])))' % [serviceName, extraFilter]
        )
        + g.query.prometheus.withLegendFormat('p50'),
        g.query.prometheus.new(
          config.datasources.prometheus,
          'histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="%s"%s}[5m])))' % [serviceName, extraFilter]
        )
        + g.query.prometheus.withLegendFormat('p95'),
        g.query.prometheus.new(
          config.datasources.prometheus,
          'histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{service_name="%s"%s}[5m])))' % [serviceName, extraFilter]
        )
        + g.query.prometheus.withLegendFormat('p99'),
      ],
      's'
    ),

  // Error rate time series
  errorRateTimeseries(title, serviceName)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          |||
            sum(rate(http_server_request_total{service_name="%s",http_response_status_code=~"5.."}[5m])) /
            sum(rate(http_server_request_total{service_name="%s"}[5m])) * 100
          ||| % [serviceName, serviceName]
        )
        + g.query.prometheus.withLegendFormat('Error Rate'),
      ],
      'percent'
    )
    + g.panel.timeSeries.standardOptions.thresholds.withSteps([
      { color: config.colors.success, value: null },
      { color: config.colors.warning, value: config.thresholds.errorRate.warning * 100 },
      { color: config.colors.danger, value: config.thresholds.errorRate.critical * 100 },
    ])
    + g.panel.timeSeries.fieldConfig.defaults.custom.withThresholdsStyle({ mode: 'line' }),

  // HTTP status codes breakdown
  statusCodesTimeseries(title, serviceName)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'sum by (http_response_status_code) (rate(http_server_request_total{service_name="%s"}[5m]))' % serviceName
        )
        + g.query.prometheus.withLegendFormat('{{http_response_status_code}}'),
      ],
      'reqps'
    ),

  // ============================================
  // INFRASTRUCTURE PANELS
  // ============================================

  // Container CPU usage
  containerCpuTimeseries(title, containerNames)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'sum by (name) (rate(container_cpu_usage_seconds_total{name=~"%s"}[5m])) * 100' % std.join('|', containerNames)
        )
        + g.query.prometheus.withLegendFormat('{{name}}'),
      ],
      'percent'
    )
    + g.panel.timeSeries.standardOptions.withMin(0)
    + g.panel.timeSeries.standardOptions.withMax(100),

  // Container memory usage
  containerMemoryTimeseries(title, containerNames)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'container_memory_usage_bytes{name=~"%s"}' % std.join('|', containerNames)
        )
        + g.query.prometheus.withLegendFormat('{{name}}'),
      ],
      'bytes'
    ),

  // Go runtime goroutines
  goroutinesTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'process_runtime_go_goroutines{service_name="openmentor-api"}'
        )
        + g.query.prometheus.withLegendFormat('Goroutines'),
      ],
      'short'
    ),

  // Go heap memory
  goHeapTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'process_runtime_go_mem_heap_alloc_bytes{service_name="openmentor-api"}'
        )
        + g.query.prometheus.withLegendFormat('Heap Allocated'),
      ],
      'bytes'
    ),

  // Node.js event loop lag
  nodeEventLoopTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'nodejs_eventloop_lag_seconds{runtime="nodejs",service_name="openmentor-frontend"}'
        )
        + g.query.prometheus.withLegendFormat('Event Loop Lag'),
      ],
      's'
    ),

  // Node.js heap memory
  nodeHeapTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'nodejs_heap_size_used_bytes{runtime="nodejs",service_name="openmentor-frontend"}'
        )
        + g.query.prometheus.withLegendFormat('Heap Used'),
        g.query.prometheus.new(
          config.datasources.prometheus,
          'nodejs_heap_size_total_bytes{runtime="nodejs",service_name="openmentor-frontend"}'
        )
        + g.query.prometheus.withLegendFormat('Heap Total'),
      ],
      'bytes'
    ),

  // ============================================
  // BUSINESS METRICS PANELS
  // ============================================

  // Page views
  pageViewsTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'sum by (page) (rate(nextjs_page_views_total{service_name="openmentor-frontend"}[5m]))'
        )
        + g.query.prometheus.withLegendFormat('{{page}}'),
      ],
      'reqps'
    ),

  // Mentor profile views (total rate, no per-mentor breakdown)
  mentorProfileViewsTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'rate(openmentor_mentor_profile_views_total{service_name="openmentor-frontend"}[5m])'
        )
        + g.query.prometheus.withLegendFormat('Profile Views'),
      ],
      'reqps'
    ),

  // Contact form submissions
  contactSubmissionsTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          'sum by (status) (rate(openmentor_contact_form_submissions_total{service_name="openmentor-api"}[5m]))'
        )
        + g.query.prometheus.withLegendFormat('{{status}}'),
      ],
      'reqps'
    ),

  // ============================================
  // EXTERNAL DEPENDENCIES PANELS
  // ============================================

  // Cache hit ratio
  cacheHitRatioTimeseries(title)::
    self.timeseries(
      title,
      [
        g.query.prometheus.new(
          config.datasources.prometheus,
          |||
            sum by (cache_name) (rate(cache_hits_total{service_name="openmentor-api"}[5m])) /
            (sum by (cache_name) (rate(cache_hits_total{service_name="openmentor-api"}[5m])) +
             sum by (cache_name) (rate(cache_misses_total{service_name="openmentor-api"}[5m]))) * 100
          |||
        )
        + g.query.prometheus.withLegendFormat('{{cache_name}}'),
      ],
      'percent'
    )
    + g.panel.timeSeries.standardOptions.withMin(0)
    + g.panel.timeSeries.standardOptions.withMax(100),

  // ============================================
  // LOG PANELS
  // ============================================

  // Error logs panel
  errorLogsPanel(title, serviceName='')::
    g.panel.logs.new(title)
    + g.panel.logs.queryOptions.withDatasource('loki', config.datasources.loki)
    + g.panel.logs.queryOptions.withTargets([
      g.query.loki.new(
        config.datasources.loki,
        if serviceName != '' then
          '{service_name="%s"} |~ "(?i)error" | json' % serviceName
        else
          '{service_name=~"openmentor-.*"} |~ "(?i)error" | json'
      ),
    ])
    + g.panel.logs.options.withShowTime(true)
    + g.panel.logs.options.withWrapLogMessage(true)
    + g.panel.logs.options.withEnableLogDetails(true),

  // ============================================
  // TABLE PANELS
  // ============================================

  // Top routes by request count
  topRoutesTable(title, serviceName)::
    g.panel.table.new(title)
    + g.panel.table.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
    + g.panel.table.queryOptions.withTargets([
      g.query.prometheus.new(
        config.datasources.prometheus,
        'topk(10, sum by (http_route, http_request_method) (increase(http_server_request_total{service_name="%s"}[1h])))' % serviceName
      )
      + g.query.prometheus.withInstant(true)
      + g.query.prometheus.withFormat('table'),
    ])
    + g.panel.table.queryOptions.withTransformations([
      { id: 'organize', options: { excludeByName: { Time: true }, renameByName: { http_route: 'Route', http_request_method: 'Method', Value: 'Requests (1h)' } } },
    ]),

  // Top errors table
  topErrorsTable(title, serviceName)::
    g.panel.table.new(title)
    + g.panel.table.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
    + g.panel.table.queryOptions.withTargets([
      g.query.prometheus.new(
        config.datasources.prometheus,
        'topk(10, sum by (http_route, http_response_status_code) (increase(http_server_request_total{service_name="%s",http_response_status_code=~"[45].."}[1h])))' % serviceName
      )
      + g.query.prometheus.withInstant(true)
      + g.query.prometheus.withFormat('table'),
    ])
    + g.panel.table.queryOptions.withTransformations([
      { id: 'organize', options: { excludeByName: { Time: true }, renameByName: { http_route: 'Route', http_response_status_code: 'Status', Value: 'Count (1h)' } } },
    ]),
}

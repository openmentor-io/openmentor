// OpenMentor Infrastructure Deep Dive Dashboard
// Container and host-level metrics for infrastructure investigation

local g = import 'grafonnet-latest/main.libsonnet';
local config = import '../lib/config.libsonnet';

local tsDefaults = g.panel.timeSeries.options.legend.withDisplayMode('table')
                   + g.panel.timeSeries.options.legend.withPlacement('right')
                   + g.panel.timeSeries.options.legend.withCalcs(['mean', 'max', 'last']);

local containers = 'openmentor-frontend|openmentor-backend|grafana-alloy|traefik|cadvisor';

g.dashboard.new('OpenMentor Infrastructure Deep Dive')
+ g.dashboard.withDescription('Container-level infrastructure metrics for resource monitoring')
+ g.dashboard.withUid('openmentor-infra-deep-dive')
+ g.dashboard.withTags(config.tags + ['deep-dive', 'infrastructure'])
+ g.dashboard.withTimezone('browser')
+ g.dashboard.withEditable(true)
+ g.dashboard.time.withFrom(config.timeRange.from)
+ g.dashboard.time.withTo(config.timeRange.to)
+ g.dashboard.withRefresh(config.refresh)
+ g.dashboard.graphTooltip.withSharedCrosshair()

+ g.dashboard.withVariables([
  g.dashboard.variable.query.new('container')
  + g.dashboard.variable.query.withDatasource('prometheus', config.datasources.prometheus)
  + g.dashboard.variable.query.queryTypes.withLabelValues('name', 'container_cpu_usage_seconds_total')
  + g.dashboard.variable.query.withRefresh('time')
  + g.dashboard.variable.query.selectionOptions.withMulti(true)
  + g.dashboard.variable.query.selectionOptions.withIncludeAll(true)
  + g.dashboard.variable.query.withRegex('openmentor.*|grafana-alloy|traefik|cadvisor'),
])

+ g.dashboard.withLinks([
  g.dashboard.link.dashboards.new('Overview', ['openmentor'])
  + g.dashboard.link.dashboards.options.withKeepTime(true),
])

+ g.dashboard.withPanels([
  // ROW: Container Overview
  g.panel.row.new('Container Overview') + g.panel.row.gridPos.withY(0),

  g.panel.stat.new('Running Containers')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'count(container_last_seen{name=~"%s"})' % containers)
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('short')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.standardOptions.thresholds.withSteps([
    { color: config.colors.danger, value: null },
    { color: config.colors.warning, value: 3 },
    { color: config.colors.success, value: 4 },
  ])
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(0) + g.panel.stat.gridPos.withY(1),

  g.panel.stat.new('Total CPU Usage')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(container_cpu_usage_seconds_total{name=~"%s"}[5m])) * 100' % containers)
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('percent')
  + g.panel.stat.standardOptions.withDecimals(1)
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 70 },
    { color: config.colors.danger, value: 90 },
  ])
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(4) + g.panel.stat.gridPos.withY(1),

  g.panel.stat.new('Total Memory Usage')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(container_memory_usage_bytes{name=~"%s"})' % containers)
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('bytes')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(8) + g.panel.stat.gridPos.withY(1),

  g.panel.stat.new('Network RX Rate')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(container_network_receive_bytes_total{name=~"%s"}[5m]))' % containers)
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('Bps')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(12) + g.panel.stat.gridPos.withY(1),

  g.panel.stat.new('Network TX Rate')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum(rate(container_network_transmit_bytes_total{name=~"%s"}[5m]))' % containers)
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('Bps')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(16) + g.panel.stat.gridPos.withY(1),

  g.panel.stat.new('Oldest Container Uptime')
  + g.panel.stat.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.stat.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'max(time() - container_start_time_seconds{name=~"%s"})' % containers)
    + g.query.prometheus.withInstant(true),
  ])
  + g.panel.stat.standardOptions.withUnit('s')
  + g.panel.stat.options.withColorMode('value')
  + g.panel.stat.gridPos.withW(4) + g.panel.stat.gridPos.withH(4) + g.panel.stat.gridPos.withX(20) + g.panel.stat.gridPos.withY(1),

  // ROW: Container CPU
  g.panel.row.new('Container CPU') + g.panel.row.gridPos.withY(5),

  g.panel.timeSeries.new('CPU Usage by Container')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_cpu_usage_seconds_total{name=~"$container"}[5m])) * 100')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('percent')
  + g.panel.timeSeries.standardOptions.withMin(0)
  + tsDefaults
  + g.panel.timeSeries.standardOptions.thresholds.withSteps([
    { color: config.colors.success, value: null },
    { color: config.colors.warning, value: 70 },
    { color: config.colors.danger, value: 90 },
  ])
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(6),

  g.panel.timeSeries.new('CPU Usage (Stacked)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_cpu_usage_seconds_total{name=~"$container"}[5m])) * 100')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('percent')
  + g.panel.timeSeries.standardOptions.withMin(0)
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.fieldConfig.defaults.custom.withStacking({ mode: 'normal' })
  + g.panel.timeSeries.fieldConfig.defaults.custom.withFillOpacity(80)
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(6),

  // ROW: Container Memory
  g.panel.row.new('Container Memory') + g.panel.row.gridPos.withY(14),

  g.panel.timeSeries.new('Memory Usage by Container')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'container_memory_usage_bytes{name=~"$container"}')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('bytes')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(15),

  g.panel.timeSeries.new('Memory Usage (Stacked)')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'container_memory_usage_bytes{name=~"$container"}')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('bytes')
  + g.panel.timeSeries.options.legend.withDisplayMode('table')
  + g.panel.timeSeries.options.legend.withPlacement('bottom')
  + g.panel.timeSeries.fieldConfig.defaults.custom.withStacking({ mode: 'normal' })
  + g.panel.timeSeries.fieldConfig.defaults.custom.withFillOpacity(80)
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(15),

  // ROW: Container Network
  g.panel.row.new('Container Network') + g.panel.row.gridPos.withY(23),

  g.panel.timeSeries.new('Network Receive Rate')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_network_receive_bytes_total{name=~"$container"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('Bps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(24),

  g.panel.timeSeries.new('Network Transmit Rate')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_network_transmit_bytes_total{name=~"$container"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('Bps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(24),

  // ROW: Container Disk
  g.panel.row.new('Container Disk I/O') + g.panel.row.gridPos.withY(32),

  g.panel.timeSeries.new('Disk Read Rate')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_fs_reads_bytes_total{name=~"$container"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('Bps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(0) + g.panel.timeSeries.gridPos.withY(33),

  g.panel.timeSeries.new('Disk Write Rate')
  + g.panel.timeSeries.queryOptions.withDatasource('prometheus', config.datasources.prometheus)
  + g.panel.timeSeries.queryOptions.withTargets([
    g.query.prometheus.new(config.datasources.prometheus, 'sum by (name) (rate(container_fs_writes_bytes_total{name=~"$container"}[5m]))')
    + g.query.prometheus.withLegendFormat('{{name}}'),
  ])
  + g.panel.timeSeries.standardOptions.withUnit('Bps')
  + tsDefaults
  + g.panel.timeSeries.gridPos.withW(12) + g.panel.timeSeries.gridPos.withH(8) + g.panel.timeSeries.gridPos.withX(12) + g.panel.timeSeries.gridPos.withY(33),
])

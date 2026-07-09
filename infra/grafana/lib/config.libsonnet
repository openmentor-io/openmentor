// OpenMentor Grafana Configuration
// Central configuration for all dashboards and alerts

{
  // Data sources - match your Grafana Cloud setup
  datasources: {
    prometheus: 'grafanacloud-glamcoder-prom',
    loki: 'grafanacloud-glamcoder-logs',
    tempo: 'grafanacloud-glamcoder-traces',
    pyroscope: 'grafanacloud-glamcoder-profiles',
  },

  // Service names (used in service_name label for all metrics)
  services: {
    frontend: {
      name: 'openmentor-frontend',
      container: 'openmentor-frontend',
    },
    backend: {
      name: 'openmentor-api',
      container: 'openmentor-backend',
    },
    alloy: {
      name: 'openmentor-o11y',
      container: 'grafana-alloy',
    },
  },

  // Default time range
  timeRange: {
    from: 'now-12h',
    to: 'now',
  },

  // Refresh interval
  refresh: '30s',

  // Dashboard tags
  tags: ['openmentor', 'production'],

  // Panel defaults
  panels: {
    height: 8,
    width: 12,
  },

  // Common thresholds
  thresholds: {
    errorRate: {
      warning: 0.01,  // 1%
      critical: 0.05, // 5%
    },
    latencyP99: {
      warning: 1,     // 1 second
      critical: 3,    // 3 seconds
    },
    cpuPercent: {
      warning: 70,
      critical: 90,
    },
    memoryPercent: {
      warning: 80,
      critical: 95,
    },
  },

  // Alert evaluation
  alerts: {
    evaluationInterval: '1m',
    forDuration: '5m',
    notificationChannels: ['email', 'telegram'],
  },

  // Colors
  // Note: 'error' is a reserved keyword in Jsonnet, using 'danger' instead
  colors: {
    success: '#73BF69',
    warning: '#FF9830',
    danger: '#F2495C',
    info: '#5794F2',
    frontend: '#FF9830',
    backend: '#73BF69',
  },
}

package profiling

import (
	"fmt"
	"strings"
	"time"

	"github.com/grafana/pyroscope-go"
	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"go.uber.org/zap"
)

var defaultProfileTypes = []pyroscope.ProfileType{
	pyroscope.ProfileCPU,
	pyroscope.ProfileAllocSpace,
	pyroscope.ProfileAllocObjects,
	pyroscope.ProfileGoroutines,
	pyroscope.ProfileMutexCount,
	pyroscope.ProfileMutexDuration,
	pyroscope.ProfileBlockCount,
	pyroscope.ProfileBlockDuration,
}

var profileTypeMap = map[string][]pyroscope.ProfileType{
	"cpu":           {pyroscope.ProfileCPU},
	"alloc_space":   {pyroscope.ProfileAllocSpace},
	"alloc_objects": {pyroscope.ProfileAllocObjects},
	"goroutines":    {pyroscope.ProfileGoroutines},
	"mutex":         {pyroscope.ProfileMutexCount, pyroscope.ProfileMutexDuration},
	"block":         {pyroscope.ProfileBlockCount, pyroscope.ProfileBlockDuration},
}

// InitProfiler initializes continuous profiling for the backend.
func InitProfiler(cfg config.ProfilingConfig, serviceName, namespace, version, instanceID, environment string) (func(), error) {
	if !cfg.Enabled {
		logger.Info("Continuous profiling disabled")
		return func() {}, nil
	}

	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("profiling endpoint is required when profiling is enabled")
	}
	if cfg.UploadIntervalSeconds <= 0 {
		cfg.UploadIntervalSeconds = 15
	}

	profileTypes, err := parseProfileTypes(cfg.SampleTypes)
	if err != nil {
		return nil, err
	}

	applicationName := buildApplicationName(
		cfg.AppName,
		serviceName,
		namespace,
		environment,
		version,
		instanceID,
	)

	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: applicationName,
		ServerAddress:   cfg.Endpoint,
		UploadRate:      time.Duration(cfg.UploadIntervalSeconds) * time.Second,
		ProfileTypes:    profileTypes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start profiler: %w", err)
	}

	logger.Info("Continuous profiling initialized",
		zap.String("application_name", applicationName),
		zap.String("endpoint", cfg.Endpoint),
		zap.String("sample_types", cfg.SampleTypes),
		zap.Int("upload_interval_seconds", cfg.UploadIntervalSeconds),
	)

	return func() {
		if stopErr := profiler.Stop(); stopErr != nil {
			logger.Error("Failed to stop profiler", zap.Error(stopErr))
		}
	}, nil
}

func parseProfileTypes(value string) ([]pyroscope.ProfileType, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultProfileTypes, nil
	}

	types := make([]pyroscope.ProfileType, 0, len(defaultProfileTypes))
	seen := make(map[pyroscope.ProfileType]struct{}, len(defaultProfileTypes))

	for _, raw := range strings.Split(value, ",") {
		key := strings.ToLower(strings.TrimSpace(raw))
		mapped, ok := profileTypeMap[key]
		if !ok {
			return nil, fmt.Errorf("unsupported O11Y_PROFILING_SAMPLE_TYPES value: %q", key)
		}

		for _, t := range mapped {
			if _, exists := seen[t]; exists {
				continue
			}

			types = append(types, t)
			seen[t] = struct{}{}
		}
	}

	if len(types) == 0 {
		return defaultProfileTypes, nil
	}

	return types, nil
}

func buildApplicationName(baseAppName, serviceName, namespace, environment, version, instanceID string) string {
	baseAppName = strings.TrimSpace(baseAppName)
	if baseAppName == "" {
		baseAppName = "openmentor-api"
	}

	labels := []string{
		fmt.Sprintf("service_name=%s", serviceName),
		fmt.Sprintf("namespace=%s", namespace),
		fmt.Sprintf("environment=%s", environment),
		fmt.Sprintf("service_version=%s", version),
		fmt.Sprintf("instance=%s", instanceID),
	}

	return fmt.Sprintf("%s{%s}", baseAppName, strings.Join(labels, ","))
}

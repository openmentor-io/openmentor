package profiling

import (
	"testing"

	"github.com/grafana/pyroscope-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProfileTypes_Default(t *testing.T) {
	got, err := parseProfileTypes("")
	require.NoError(t, err)
	assert.Equal(t, defaultProfileTypes, got)
}

func TestParseProfileTypes_Custom(t *testing.T) {
	got, err := parseProfileTypes("cpu, alloc_space,mutex")
	require.NoError(t, err)

	assert.Equal(t, []pyroscope.ProfileType{
		pyroscope.ProfileCPU,
		pyroscope.ProfileAllocSpace,
		pyroscope.ProfileMutexCount,
		pyroscope.ProfileMutexDuration,
	}, got)
}

func TestParseProfileTypes_Invalid(t *testing.T) {
	_, err := parseProfileTypes("cpu,unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported O11Y_PROFILING_SAMPLE_TYPES")
}

func TestBuildApplicationName(t *testing.T) {
	got := buildApplicationName("openmentor-api", "openmentor-api", "openmentor-io", "production", "2.0.0", "inst-1")
	assert.Equal(t, "openmentor-api{service_name=openmentor-api,namespace=openmentor-io,environment=production,service_version=2.0.0,instance=inst-1}", got)
}

package metrics

import (
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Registry is the custom Prometheus registry that wraps metrics with service_name label
	Registry *prometheus.Registry

	// HTTP Metrics
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestTotal    *prometheus.CounterVec
	ActiveRequests      *prometheus.GaugeVec

	// Database Client Metrics (PostgreSQL)
	DBRequestDuration *prometheus.HistogramVec
	DBRequestTotal    *prometheus.CounterVec

	// Storage Client Metrics (S3-compatible object storage)
	S3StorageRequestDuration *prometheus.HistogramVec
	S3StorageRequestTotal    *prometheus.CounterVec

	// Business Metrics
	MentorProfileViews     *prometheus.CounterVec
	ContactFormSubmissions *prometheus.CounterVec
	ProfileUpdates         *prometheus.CounterVec
	ProfilePictureUploads  *prometheus.CounterVec
	MentorRegistrations    *prometheus.CounterVec
	PhotoClassifications   *prometheus.CounterVec

	// Mentor Auth Metrics
	MentorAuthLoginRequests     *prometheus.CounterVec
	MentorAuthLoginDuration     prometheus.Histogram
	MentorAuthVerifyRequests    *prometheus.CounterVec
	MentorAuthVerifyDuration    prometheus.Histogram
	MentorRequestsListTotal     *prometheus.CounterVec
	MentorRequestsListDuration  prometheus.Histogram
	MentorRequestsStatusUpdates *prometheus.CounterVec
	MentorRequestsDeclines      *prometheus.CounterVec

	// Review Metrics
	ReviewSubmissions *prometheus.CounterVec
	ReviewChecks      *prometheus.CounterVec
	ReviewDuration    prometheus.Histogram
	// ReviewLegacyLinkUses is the H4 cutover gauge: it counts hits on the
	// pre-H4 request-id review endpoints. Deleting those endpoints (the contract
	// step) is gated on this reaching, and staying at, zero.
	ReviewLegacyLinkUses *prometheus.CounterVec

	// Worker Metrics (background worker binary, cmd/worker)
	WorkerCronRunsTotal   *prometheus.CounterVec
	WorkerCronRunDuration *prometheus.HistogramVec
	WorkerEmailSendsTotal *prometheus.CounterVec

	// Profile retention purge (D70). The cron metrics above say whether the
	// sweep RAN; these say whether it worked. See their definitions in Init.
	ProfilePurgeProfilesTotal        *prometheus.CounterVec
	ProfilePurgeLastSuccessTimestamp prometheus.Gauge
	ProfilePurgeFirstStartTimestamp  prometheus.Gauge
	ProfilePurgeMaxAge               prometheus.Gauge

	// Infrastructure Metrics
	GoRoutines prometheus.Gauge
	HeapAlloc  prometheus.Gauge
)

// Init initializes the metrics registry with service_name label from config
// Uses WrapRegistererWith to automatically inject service_name into ALL metrics
// Must be called from main.go after config is loaded
func Init(serviceName string) {
	// Create custom registry
	Registry = prometheus.NewRegistry()

	// Wrap registry to automatically add service_name label to all metrics
	// This eliminates need for ConstLabels on individual metrics
	wrapped := prometheus.WrapRegistererWith(
		prometheus.Labels{"service_name": serviceName},
		Registry,
	)

	// Create promauto factory that uses the wrapped registry
	factory := promauto.With(wrapped)

	// HTTP Metrics
	HTTPRequestDuration = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_server_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"http_request_method", "http_route", "http_response_status_code"},
	)

	HTTPRequestTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_server_request_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"http_request_method", "http_route", "http_response_status_code"},
	)

	ActiveRequests = factory.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_server_active_requests",
			Help: "Number of active HTTP requests",
		},
		[]string{"http_request_method"},
	)

	// Database Client Metrics (PostgreSQL)
	DBRequestDuration = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_client_operation_duration_seconds",
			Help:    "Database client operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "status"},
	)

	DBRequestTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_client_operation_total",
			Help: "Total number of database client operations",
		},
		[]string{"operation", "status"},
	)

	// Storage Client Metrics (S3-compatible object storage)
	S3StorageRequestDuration = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "s3_storage_operation_duration_seconds",
			Help:    "S3-compatible object storage operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "status"},
	)

	S3StorageRequestTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "s3_storage_operation_total",
			Help: "Total number of S3-compatible object storage operations",
		},
		[]string{"operation", "status"},
	)

	// Business Metrics
	MentorProfileViews = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_profile_views_total",
			Help: "Total number of mentor profile views",
		},
		[]string{},
	)

	ContactFormSubmissions = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_contact_form_submissions_total",
			Help: "Total number of contact form submissions",
		},
		[]string{"status"},
	)

	ProfileUpdates = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_profile_updates_total",
			Help: "Total number of profile updates",
		},
		[]string{"status"},
	)

	ProfilePictureUploads = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_profile_picture_uploads_total",
			Help: "Total number of profile picture uploads",
		},
		[]string{"status"},
	)

	MentorRegistrations = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_registrations_total",
			Help: "Total mentor registration attempts",
		},
		[]string{"status"},
	)

	PhotoClassifications = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_photo_classifications_total",
			Help: "Total number of profile photo style classifications",
		},
		// busy = the decode was shed because every decode slot was taken
		// (imageclass.ErrDecoderBusy); the photo is still stored, with the
		// default style.
		[]string{"result"}, // result: hero | frame | error | busy
	)

	// Mentor Auth Metrics
	MentorAuthLoginRequests = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_auth_login_requests_total",
			Help: "Total mentor login requests",
		},
		[]string{"status"},
	)

	MentorAuthLoginDuration = factory.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "openmentor_mentor_auth_login_duration_seconds",
			Help:    "Mentor login request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	MentorAuthVerifyRequests = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_auth_verify_requests_total",
			Help: "Total mentor token verification requests",
		},
		[]string{"status"},
	)

	MentorAuthVerifyDuration = factory.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "openmentor_mentor_auth_verify_duration_seconds",
			Help:    "Mentor token verification duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	MentorRequestsListTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_requests_list_total",
			Help: "Total mentor requests list fetches",
		},
		[]string{"group"},
	)

	MentorRequestsListDuration = factory.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "openmentor_mentor_requests_list_duration_seconds",
			Help:    "Mentor requests list duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	MentorRequestsStatusUpdates = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_requests_status_updates_total",
			Help: "Total mentor request status updates",
		},
		[]string{"from_status", "to_status"},
	)

	MentorRequestsDeclines = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_mentor_requests_declines_total",
			Help: "Total mentor request declines",
		},
		[]string{"reason"},
	)

	// Review Metrics
	ReviewSubmissions = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_review_submissions_total",
			Help: "Total review submissions",
		},
		[]string{"status"},
	)

	ReviewChecks = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_review_checks_total",
			Help: "Total review eligibility checks",
		},
		[]string{"result"},
	)

	ReviewLegacyLinkUses = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_review_legacy_link_uses_total",
			Help: "Hits on the pre-H4 request-id review endpoints (endpoint: check|submit, outcome: accepted|refused)",
		},
		[]string{"endpoint", "outcome"},
	)

	ReviewDuration = factory.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "openmentor_review_submission_duration_seconds",
			Help:    "Review submission duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	// Worker Metrics (background worker binary, cmd/worker)
	WorkerCronRunsTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_worker_cron_runs_total",
			Help: "Total number of worker cron job runs",
		},
		// outcome: success | error | panic | skipped (non-production gate) |
		// skipped_overlap (tick dropped, previous run still in flight) |
		// skipped_lease (another run of this job holds the advisory lease, D82) |
		// lease_error (the lease could not be acquired, so the run failed closed)
		[]string{"job", "outcome"},
	)

	WorkerCronRunDuration = factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "openmentor_worker_cron_run_duration_seconds",
			Help:    "Worker cron job run duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600},
		},
		[]string{"job"},
	)

	WorkerEmailSendsTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_worker_email_sends_total",
			Help: "Total number of transactional email send attempts from worker jobs",
		},
		[]string{"template", "outcome"}, // outcome: success | error
	)

	// Profile retention purge (D70). openmentor_worker_cron_runs_total already
	// says whether the SWEEP ran, but it cannot say whether the sweep did
	// anything: PurgeDeletedProfiles deliberately returns nil when individual
	// profiles fail, so one bad row does not abort the backlog behind it — and
	// that made a run where EVERY profile failed indistinguishable from a
	// healthy one at the cron level. These four exist to close that gap, and
	// their shape mirrors the postgres-backup gauges because ProfilePurgeStale
	// mirrors DatabaseBackupStale.
	ProfilePurgeProfilesTotal = factory.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openmentor_profile_purge_profiles_total",
			Help: "Deleted profiles the retention sweep acted on, by outcome",
		},
		// outcome: purged (erased for good) | skipped (restored between the
		// listing and the write, so the guard refused it) | failed (errored;
		// retried on the next pass)
		[]string{"outcome"},
	)

	ProfilePurgeLastSuccessTimestamp = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "openmentor_profile_purge_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last retention sweep that completed with no failures",
		},
	)

	// ProfilePurgeFirstStartTimestamp gives a freshly started worker one grace
	// window before the staleness rule can fire, without ever claiming a sweep
	// that has not happened — exactly the role
	// openmentor_db_backup_first_start_timestamp_seconds plays for backups.
	ProfilePurgeFirstStartTimestamp = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "openmentor_profile_purge_first_start_timestamp_seconds",
			Help: "Unix timestamp at which this worker process started, for the purge staleness grace window",
		},
	)

	// ProfilePurgeMaxAge is PUBLISHED rather than hardcoded in the alert so
	// that retuning WORKER_PROFILE_PURGE_CRON moves the threshold with it. It
	// is derived from the schedule itself (two firing intervals), not from a
	// second env var that could disagree with it.
	ProfilePurgeMaxAge = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "openmentor_profile_purge_max_age_seconds",
			Help: "How stale the last successful retention sweep may be before it is considered broken",
		},
	)

	// Infrastructure Metrics
	GoRoutines = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "process_runtime_go_goroutines",
			Help: "Number of goroutines",
		},
	)

	HeapAlloc = factory.NewGauge(
		prometheus.GaugeOpts{
			Name: "process_runtime_go_mem_heap_alloc_bytes",
			Help: "Heap allocated bytes",
		},
	)
}

// RecordInfrastructureMetrics collects infrastructure metrics periodically
func RecordInfrastructureMetrics() {
	ticker := time.NewTicker(15 * time.Second)
	// TODO: Add stop channel/context to metrics goroutine to prevent leak on shutdown
	go func() {
		for range ticker.C {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			GoRoutines.Set(float64(runtime.NumGoroutine()))
			HeapAlloc.Set(float64(m.HeapAlloc))
		}
	}()
}

// MeasureDuration measures the duration of an operation
func MeasureDuration(start time.Time) float64 {
	return time.Since(start).Seconds()
}

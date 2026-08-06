package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/internal/handlers"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/reviewtoken"
)

// The observability middleware records Prometheus metrics, so the registry has to
// exist before a request runs through it.
func init() { metrics.Init("openmentor-review-sentinel-test") }

// sentinelReviewToken is a fabricated, well-formed H4 capability. Never a real
// minted token: no live capability may appear in the repository or in CI output.
const sentinelReviewToken = reviewtoken.Prefix + "SENTINELhttpLayerCapabilityAAAAAAAAAAAAAAAA"

// recordingReviewService accepts anything and records what it was given, so this
// test measures the transport, not the business rules.
type recordingReviewService struct {
	gotCheckToken  string
	gotSubmitToken string
	fail           error
}

func (s *recordingReviewService) CheckReviewByToken(_ context.Context, rawToken string) (*models.ReviewCheckResponse, error) {
	s.gotCheckToken = rawToken
	if s.fail != nil {
		return &models.ReviewCheckResponse{CanSubmit: false, Error: "This review link has already been used or has expired."}, s.fail
	}
	return &models.ReviewCheckResponse{CanSubmit: true, MentorName: "John Doe"}, nil
}

func (s *recordingReviewService) SubmitReviewWithToken(_ context.Context, rawToken string, _ *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error) {
	s.gotSubmitToken = rawToken
	if s.fail != nil {
		return &models.SubmitReviewResponse{Success: false, Error: "This review link has already been used or has expired."}, s.fail
	}
	return &models.SubmitReviewResponse{Success: true, ReviewID: "review-1"}, nil
}

func (s *recordingReviewService) CheckReview(_ context.Context, _ string) (*models.ReviewCheckResponse, error) {
	return &models.ReviewCheckResponse{CanSubmit: true, MentorName: "John Doe"}, nil
}

func (s *recordingReviewService) SubmitReview(_ context.Context, _ string, _ *models.SubmitReviewRequest) (*models.SubmitReviewResponse, error) {
	return &models.SubmitReviewResponse{Success: true, ReviewID: "review-1"}, nil
}

var _ services.ReviewServiceInterface = (*recordingReviewService)(nil)

// newReviewCapabilityRouter mirrors the middleware order and the route shapes
// cmd/api registers, so what this test observes is what production emits.
func newReviewCapabilityRouter(service services.ReviewServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(otelgin.Middleware("openmentor-api-test"))
	r.Use(middleware.ObservabilityMiddleware())

	handler := handlers.NewReviewHandler(service)
	group := r.Group("/api/v1")
	group.POST("/reviews/check", handler.CheckReviewByToken)
	group.POST("/reviews/submit", handler.SubmitReviewWithToken)
	group.GET("/reviews/:requestId/check", handler.CheckReview)
	group.POST("/reviews/:requestId", handler.SubmitReview)
	return r
}

func observeCapabilityLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Log
	logger.Log = zap.New(core)
	t.Cleanup(func() { logger.Log = previous })
	return logs
}

func recordCapabilitySpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return exporter
}

// TestReviewTokenNeverReachesUrlLogOrSpan is the H4 transport sentinel.
//
// It drives the real router with the real observability middleware and asserts
// the capability appears in NO url.path, NO url.query, NO span attribute, NO log
// field and NO response body — for both the happy path and the rejected path,
// since 4xx is the branch that logs route params and query params.
func TestReviewTokenNeverReachesUrlLogOrSpan(t *testing.T) {
	cases := map[string]error{
		"accepted": nil,
		"rejected": services.ErrReviewLinkInvalid,
	}

	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			logs := observeCapabilityLogs(t)
			spans := recordCapabilitySpans(t)
			service := &recordingReviewService{fail: failure}
			router := newReviewCapabilityRouter(service)

			checkBody, err := json.Marshal(models.CheckReviewRequest{Token: sentinelReviewToken})
			require.NoError(t, err)
			submitBody, err := json.Marshal(models.SubmitReviewRequest{
				Token:        sentinelReviewToken,
				MentorReview: "a genuinely useful hour",
				CaptchaToken: "0.captcha-sentinel",
			})
			require.NoError(t, err)

			responses := []string{
				do(t, router, http.MethodPost, "/api/v1/reviews/check", checkBody),
				do(t, router, http.MethodPost, "/api/v1/reviews/submit", submitBody),
			}

			// The handlers must actually have received it, or this proves nothing.
			assert.Equal(t, sentinelReviewToken, service.gotCheckToken)
			assert.Equal(t, sentinelReviewToken, service.gotSubmitToken)

			for _, body := range responses {
				assert.NotContains(t, body, sentinelReviewToken, "the response echoed the capability back")
			}

			for _, entry := range logs.All() {
				line := entry.Message + " " + fmt.Sprint(entry.ContextMap())
				assert.NotContains(t, line, sentinelReviewToken, "a log line carried the capability: %s", line)
			}

			require.NotEmpty(t, spans.GetSpans(), "no span was recorded: the test observed nothing")
			for _, span := range spans.GetSpans() {
				assert.NotContains(t, span.Name, sentinelReviewToken)
				for _, attr := range span.Attributes {
					assert.NotContains(t, attr.Value.String(), sentinelReviewToken,
						"span attribute %s carried the capability", attr.Key)
				}
				for _, event := range span.Events {
					for _, attr := range event.Attributes {
						assert.NotContains(t, attr.Value.String(), sentinelReviewToken,
							"span event attribute %s carried the capability", attr.Key)
					}
				}
			}
		})
	}
}

// TestReviewCapabilityRoutesHaveConstantUrls is the structural half of the same
// guarantee: the token-bearing routes have no parameter at all, so there is no
// way for a future change to put the capability back into the URL without
// changing the route template this test pins.
func TestReviewCapabilityRoutesHaveConstantUrls(t *testing.T) {
	router := newReviewCapabilityRouter(&recordingReviewService{})

	templates := map[string]string{}
	for _, route := range router.Routes() {
		templates[route.Method+" "+route.Path] = route.Path
	}

	for _, path := range []string{"POST /api/v1/reviews/check", "POST /api/v1/reviews/submit"} {
		template, ok := templates[path]
		require.True(t, ok, "%s is not registered", path)
		assert.NotContains(t, template, ":", "the capability path must carry no parameter: %s", template)
		assert.NotContains(t, template, "*", "the capability path must carry no wildcard: %s", template)
	}
}

func do(t *testing.T, router *gin.Engine, method, path string, body []byte) string {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// The request target itself must never have carried the capability.
	require.False(t, strings.Contains(req.URL.String(), sentinelReviewToken))
	return w.Body.String()
}

package httpclient

import (
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client defines an interface for making HTTP requests
// This allows for easy mocking and testing of HTTP calls
type Client interface {
	Post(url, contentType string, body io.Reader) (*http.Response, error)
	Get(url string) (*http.Response, error)
	Do(req *http.Request) (*http.Response, error)
}

// StandardHTTPClient wraps the standard http.Client
type StandardHTTPClient struct {
	client *http.Client
}

// NewStandardClient creates a new HTTP client with default settings.
// The transport is instrumented with OpenTelemetry: every outgoing request
// gets a client span and W3C traceparent/tracestate headers injected from
// the request context, so calls to the worker (pkg/trigger) and external
// APIs join the caller's trace. Requests without a traced context are
// simply passed through with no-op spans.
func NewStandardClient() Client {
	return &StandardHTTPClient{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Post makes a POST request
func (c *StandardHTTPClient) Post(url, contentType string, body io.Reader) (*http.Response, error) {
	return c.client.Post(url, contentType, body)
}

// Get makes a GET request
func (c *StandardHTTPClient) Get(url string) (*http.Response, error) {
	return c.client.Get(url)
}

// Do executes an HTTP request
func (c *StandardHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

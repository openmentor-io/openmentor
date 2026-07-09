package turnstile_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHTTPClient mocks the HTTP client
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Get(url string) (*http.Response, error) {
	args := m.Called(url)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*http.Response), args.Error(1)
}

func (m *MockHTTPClient) Post(url, contentType string, body io.Reader) (*http.Response, error) {
	args := m.Called(url, contentType, body)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*http.Response), args.Error(1)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*http.Response), args.Error(1)
}

// TestVerifier_Verify_Success tests successful verification
func TestVerifier_Verify_Success(t *testing.T) {
	mockClient := new(MockHTTPClient)
	verifier := turnstile.NewVerifier("test-secret-key", mockClient)

	// Mock successful response from Cloudflare
	responseBody := `{"success": true, "challenge_ts": "2024-01-01T00:00:00Z", "hostname": "example.com"}`
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
	}

	mockClient.On("Post", "https://challenges.cloudflare.com/turnstile/v0/siteverify", "application/x-www-form-urlencoded", mock.Anything).Return(mockResponse, nil)

	// Test verification
	err := verifier.Verify("valid-token")

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

// TestVerifier_Verify_Failed tests failed verification
func TestVerifier_Verify_Failed(t *testing.T) {
	mockClient := new(MockHTTPClient)
	verifier := turnstile.NewVerifier("test-secret-key", mockClient)

	// Mock failed response from Cloudflare
	responseBody := `{"success": false, "error-codes": ["invalid-input-response"]}`
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
	}

	mockClient.On("Post", "https://challenges.cloudflare.com/turnstile/v0/siteverify", "application/x-www-form-urlencoded", mock.Anything).Return(mockResponse, nil)

	// Test verification
	err := verifier.Verify("invalid-token")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "turnstile verification failed")
	mockClient.AssertExpectations(t)
}

// TestVerifier_Verify_NetworkError tests network error handling
func TestVerifier_Verify_NetworkError(t *testing.T) {
	mockClient := new(MockHTTPClient)
	verifier := turnstile.NewVerifier("test-secret-key", mockClient)

	// Mock network error
	mockClient.On("Post", "https://challenges.cloudflare.com/turnstile/v0/siteverify", "application/x-www-form-urlencoded", mock.Anything).Return(nil, assert.AnError)

	// Test verification
	err := verifier.Verify("token")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to verify turnstile token")
	mockClient.AssertExpectations(t)
}

// TestVerifier_Verify_InvalidJSON tests invalid JSON response
func TestVerifier_Verify_InvalidJSON(t *testing.T) {
	mockClient := new(MockHTTPClient)
	verifier := turnstile.NewVerifier("test-secret-key", mockClient)

	// Mock invalid JSON response
	responseBody := `{invalid-json`
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
	}

	mockClient.On("Post", "https://challenges.cloudflare.com/turnstile/v0/siteverify", "application/x-www-form-urlencoded", mock.Anything).Return(mockResponse, nil)

	// Test verification
	err := verifier.Verify("token")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode turnstile response")
	mockClient.AssertExpectations(t)
}

// TestVerifier_Verify_EmptyToken tests with empty token
func TestVerifier_Verify_EmptyToken(t *testing.T) {
	mockClient := new(MockHTTPClient)
	verifier := turnstile.NewVerifier("test-secret-key", mockClient)

	// Mock failed response for empty token
	responseBody := `{"success": false, "error-codes": ["missing-input-response"]}`
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(responseBody)),
	}

	mockClient.On("Post", "https://challenges.cloudflare.com/turnstile/v0/siteverify", "application/x-www-form-urlencoded", mock.Anything).Return(mockResponse, nil)

	// Test verification with empty token
	err := verifier.Verify("")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "turnstile verification failed")
	mockClient.AssertExpectations(t)
}

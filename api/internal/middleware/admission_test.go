package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// countingBody reports how much of the request body was actually read, which is
// the property the admission limiter exists for: a shed request must not have
// had its (up to 10 MiB) payload buffered.
type countingBody struct {
	inner io.Reader
	read  int
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	c.read += n
	return n, err
}

func (c *countingBody) Close() error { return nil }

func TestAdmissionLimiterShedsWithoutReadingTheBody(t *testing.T) {
	limiter := NewAdmissionLimiter(1, 20*time.Millisecond)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)

	var mu sync.Mutex
	handled := 0

	r := gin.New()
	r.POST("/upload", limiter.Middleware(), func(c *gin.Context) {
		mu.Lock()
		handled++
		mu.Unlock()
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			t.Errorf("reading body: %v", err)
		}
		entered <- struct{}{}
		// Bounded, so a limiter that stopped shedding fails the assertions
		// below instead of deadlocking the test.
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		c.Status(http.StatusOK)
	})

	// Occupy the only slot.
	holderDone := make(chan int)
	go func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("first"))
		r.ServeHTTP(w, req)
		holderDone <- w.Code
	}()
	<-entered

	// The second request finds no slot, waits, and is shed — with its payload
	// still on the wire.
	body := &countingBody{inner: strings.NewReader(strings.Repeat("x", 4096))}
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	w := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(w, req)
	waited := time.Since(start)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("shed request status = %d, want 429", w.Code)
	}
	if body.read != 0 {
		t.Errorf("shed request read %d body bytes, want 0 — waiting must be cheaper than being admitted", body.read)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("a shed request needs Retry-After: the client is meant to come back")
	}
	if waited < 20*time.Millisecond {
		t.Errorf("gave up after %v, want at least the configured wait", waited)
	}

	close(release)
	if code := <-holderDone; code != http.StatusOK {
		t.Errorf("holder status = %d, want 200", code)
	}

	mu.Lock()
	defer mu.Unlock()
	if handled != 1 {
		t.Errorf("handler ran %d times, want 1 (the shed request must never reach it)", handled)
	}
}

func TestAdmissionLimiterReleasesSlots(t *testing.T) {
	limiter := NewAdmissionLimiter(2, time.Second)

	r := gin.New()
	r.POST("/upload", limiter.Middleware(), func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			t.Errorf("reading body: %v", err)
		}
		c.Status(http.StatusOK)
	})

	// A burst well over the slot count, sequentially: a leaked slot would shed
	// every upload from then on, permanently.
	for i := range 10 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("payload")))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, w.Code)
		}
	}
	if held := limiter.InFlight(); held != 0 {
		t.Errorf("%d slots still held, want 0", held)
	}
}

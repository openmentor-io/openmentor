package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// AdmissionLimiter bounds how many requests are IN FLIGHT on the routes it
// guards. That is a different bound from RateLimiter, which caps the arrival
// RATE and says nothing about how many payloads are resident at once, and from
// the decode semaphore in pkg/imageclass, which only bounds the pixel buffers.
//
// It is what the big-body endpoints need: a request that has been admitted holds
// its whole JSON body plus the base64-decoded image while it talks to S3, so the
// resident cost is the product of the payload cap and the in-flight count.
// Measured on the picture endpoints, one in-flight upload retains ~3x its body
// (the body string, the JSON decoder's buffer, the decoded image), so the burst
// of 20 the rate limiters allow is ~840 MiB in a 512 MiB container. The slot
// count is MaxUploadsInFlight, which is sized against MaxImageBodyBytes.
//
// The slot is taken BEFORE the handler runs, which is the point: a shed request
// has never had its body read, so a waiter costs a goroutine and nothing else.
type AdmissionLimiter struct {
	slots chan struct{}
	wait  time.Duration
}

// errAdmissionBusy reports that every slot stayed taken for the whole wait.
var errAdmissionBusy = errors.New("no admission slot available")

// NewAdmissionLimiter allows maxInFlight concurrent requests through the
// returned middleware; anything beyond that waits up to wait for a slot and is
// then shed with 429.
func NewAdmissionLimiter(maxInFlight int, wait time.Duration) *AdmissionLimiter {
	if maxInFlight < 1 {
		maxInFlight = 1
	}
	return &AdmissionLimiter{
		slots: make(chan struct{}, maxInFlight),
		wait:  wait,
	}
}

// Middleware returns the Gin middleware that holds a slot for the duration of
// the request.
func (a *AdmissionLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := a.acquire(c.Request.Context()); err != nil {
			if errors.Is(err, errAdmissionBusy) {
				c.Header("Retry-After", "5")
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "The server is busy processing uploads. Please try again in a few seconds.",
				})
				return
			}
			// The client hung up while queued, so there is nobody to answer:
			// 499 is the conventional "client closed request".
			c.AbortWithStatus(499)
			return
		}
		defer a.release()

		c.Next()
	}
}

// InFlight reports how many slots are currently held (tests and diagnostics).
func (a *AdmissionLimiter) InFlight() int { return len(a.slots) }

func (a *AdmissionLimiter) acquire(ctx context.Context) error {
	// Take a free slot without consulting ctx: select picks at random among
	// ready cases, so an already-canceled context would otherwise shed half
	// the uncontended requests.
	select {
	case a.slots <- struct{}{}:
		return nil
	default:
	}

	timer := time.NewTimer(a.wait)
	defer timer.Stop()

	select {
	case a.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errAdmissionBusy
	}
}

func (a *AdmissionLimiter) release() { <-a.slots }

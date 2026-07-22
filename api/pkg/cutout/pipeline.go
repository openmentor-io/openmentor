package cutout

import (
	"context"
	"time"

	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// Metric label values for the cutout pipeline. Source separates live uploads
// from the worker backfill; Outcome is the terminal result of ProcessImage
// (plus NoPhoto, which only the backfill can produce before ProcessImage runs).
const (
	SourceUpload   = "upload"
	SourceBackfill = "backfill"

	OutcomeHero    = "hero"     // cutout accepted and hero asset uploaded
	OutcomeFrame   = "frame"    // decodable cutout rejected by the quality gate
	OutcomeNoPhoto = "no_photo" // no source photo to process
	OutcomeError   = "error"    // removal / gate / upload failure
)

// Uploader stores the accepted hero PNG (e.g. to <slug>/hero in object
// storage). ProcessImage calls it only when the quality gate passes.
type Uploader func(ctx context.Context, png []byte) error

// Result is the outcome of running the cutout pipeline on one image.
type Result struct {
	// Style is the photo_style to persist: StyleHero on success, StyleFrame
	// on rejection or any failure (a failed cutout must never break the photo).
	Style string
	// Outcome is the terminal classification for metrics/logging (Outcome*).
	Outcome string
	// Gate carries the mask measurements (populated once the gate ran).
	Gate GateResult
	// Err is non-nil when Outcome == OutcomeError.
	Err error
}

// ProcessImage runs the full hero-cutout pipeline on already-decoded image
// bytes: remove the background (timed), quality-gate the result, and on a pass
// upload it via put. It records all cutout metrics tagged with source, so both
// the upload and backfill callers stay consistent. It never panics and only
// returns an error inside Result.Err; the returned Style is always safe to
// persist. The caller is responsible for logging (it has the slug/context).
func (c *Client) ProcessImage(ctx context.Context, source string, imageBytes []byte, put Uploader) Result {
	start := time.Now()
	png, err := c.Remove(ctx, imageBytes)
	observeRemove(source, time.Since(start))
	if err != nil {
		recordOutcome(source, OutcomeError)
		return Result{Style: StyleFrame, Outcome: OutcomeError, Err: err}
	}

	gate, err := QualityGate(png)
	if err != nil {
		recordOutcome(source, OutcomeError)
		return Result{Style: StyleFrame, Outcome: OutcomeError, Err: err}
	}
	if !gate.OK {
		recordGateRejection(source, gate.Code)
		recordOutcome(source, OutcomeFrame)
		return Result{Style: StyleFrame, Outcome: OutcomeFrame, Gate: gate}
	}

	if err := put(ctx, png); err != nil {
		recordOutcome(source, OutcomeError)
		return Result{Style: StyleFrame, Outcome: OutcomeError, Gate: gate, Err: err}
	}

	recordOutcome(source, OutcomeHero)
	return Result{Style: StyleHero, Outcome: OutcomeHero, Gate: gate}
}

// RecordOutcome records a terminal outcome the pipeline itself cannot observe
// (currently only OutcomeNoPhoto, decided by the backfill before ProcessImage).
func RecordOutcome(source, outcome string) { recordOutcome(source, outcome) }

// The record* helpers are all nil-safe so the package works in tests and
// tooling that never call metrics.Init.
func recordOutcome(source, outcome string) {
	if metrics.PhotoCutouts != nil {
		metrics.PhotoCutouts.WithLabelValues(source, outcome).Inc()
	}
}

func observeRemove(source string, d time.Duration) {
	if metrics.PhotoCutoutRemoveSeconds != nil {
		metrics.PhotoCutoutRemoveSeconds.WithLabelValues(source).Observe(d.Seconds())
	}
}

func recordGateRejection(source, reason string) {
	if reason == "" {
		reason = "unknown"
	}
	if metrics.PhotoCutoutGateRejections != nil {
		metrics.PhotoCutoutGateRejections.WithLabelValues(source, reason).Inc()
	}
}

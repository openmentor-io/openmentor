package imageclass

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"time"
)

// The decode budget. image.Decode allocates one contiguous pixel buffer, so
// GOMEMLIMIT cannot soften it and the peak has to be bounded by construction:
// MaxPixels bounds ONE decode, maxConcurrentDecodes bounds how many run at
// once. Their product is the share of the API container (512 MiB, see
// infra/docker-compose.yml) that uploads may occupy, so raising either constant
// means re-deciding that share — TestDecodeBudgetFitsContainer enforces it.
const (
	// MaxPixels bounds the DECODED pixel count, because the compressed size
	// bounds nothing: a crafted PNG amplifies thousands of times. At
	// BytesPerPixel that is a 64 MiB buffer. It cannot go much lower — nothing
	// downscales client-side, so it has to clear an ordinary 12 MP phone photo
	// (12.2M px).
	MaxPixels = 16_000_000

	// BytesPerPixel is what one decoded pixel costs in the worst case: the
	// image.NRGBA an RGBA PNG decodes into.
	BytesPerPixel = 4

	// MaxAspectRatio rejects extreme strips. A 16000x1000 image fits the pixel
	// budget yet is no more a profile picture than a bomb is, and it still
	// forces a huge single-dimension allocation.
	MaxAspectRatio = 20

	// maxConcurrentDecodes caps simultaneous decodes, because MaxPixels alone
	// is not a bound: the upload endpoints share one rate limiter with a burst
	// of 20, and 20 decodes at the pixel bound is 1.3 GiB against a 512 MiB
	// container. Two holds the peak to 128 MiB and is still well above real
	// demand — profile pictures are uploaded a few times a day.
	maxConcurrentDecodes = 2

	// decodeQueueWait is how long an upload waits for a slot before giving up
	// on classification: long enough to absorb two real uploads overlapping,
	// short enough that waiters cannot pile up holding their payloads.
	decodeQueueWait = 250 * time.Millisecond
)

// ErrImageTooLarge reports geometry a full decode cannot afford.
var ErrImageTooLarge = errors.New("image is too large to process")

// ErrDecoderBusy reports that every decode slot stayed taken for
// decodeQueueWait. Classification is cosmetic, so callers treat this as "store
// the photo with the default style", not as a rejected upload.
var ErrDecoderBusy = errors.New("image decoder is busy")

// decodeSlots is the decode concurrency semaphore (see maxConcurrentDecodes).
var decodeSlots = make(chan struct{}, maxConcurrentDecodes)

// acquireDecodeSlot reserves one of the maxConcurrentDecodes decode slots. The
// slot has to be held for as long as the decoded image is reachable, not just
// across the image.Decode call: the pixel buffer is what is being budgeted.
func acquireDecodeSlot(ctx context.Context) error {
	// Take a free slot without consulting ctx: select picks at random among
	// ready cases, so an already-canceled context would otherwise cost half
	// the uncontended decodes their classification.
	select {
	case decodeSlots <- struct{}{}:
		return nil
	default:
	}

	timer := time.NewTimer(decodeQueueWait)
	defer timer.Stop()

	select {
	case decodeSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrDecoderBusy
	}
}

func releaseDecodeSlot() { <-decodeSlots }

// CheckBounds parses ONLY the image header (image.DecodeConfig: microseconds,
// no pixel buffer) and rejects geometry whose full decode would blow the
// container's memory budget. It MUST run before image.Decode: decoding is what
// allocates, so by then it is too late.
func CheckBounds(data []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to read image header: %w", err)
	}
	return checkConfigBounds(cfg)
}

func checkConfigBounds(cfg image.Config) error {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("image has no pixels (%dx%d)", cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		// The reason reaches the uploader verbatim, so say what to do about it.
		return fmt.Errorf("%w: %dx%d pixels — please crop or resize it to under %d megapixels",
			ErrImageTooLarge, cfg.Width, cfg.Height, MaxPixels/1_000_000)
	}

	long, short := cfg.Width, cfg.Height
	if short > long {
		long, short = short, long
	}
	if long > short*MaxAspectRatio {
		return fmt.Errorf("%w: %dx%d is too far from square (max %d:1)",
			ErrImageTooLarge, cfg.Width, cfg.Height, MaxAspectRatio)
	}
	return nil
}

package imageclass

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"time"
)

// The decode budget. image.Decode allocates one contiguous pixel buffer, so
// GOMEMLIMIT cannot soften it and the peak has to be bounded by construction:
// MaxDecodeBytes bounds ONE decode, maxConcurrentDecodes bounds how many run at
// once. Their product is the share of the API container (512 MiB, see
// infra/docker-compose.yml) that uploads may occupy, so raising either constant
// means re-deciding that share — TestDecodeBudgetFitsContainer enforces it.
const (
	// MaxPixels bounds the DECODED pixel count, because the compressed size
	// bounds nothing: a crafted PNG amplifies thousands of times. It cannot go
	// much lower — nothing downscales client-side, so it has to clear an
	// ordinary 12 MP phone photo (12.2M px).
	MaxPixels = 16_000_000

	// BytesPerPixel is what one decoded pixel of an ORDINARY 8-bit image costs
	// (image.NRGBA/RGBA, the widest of the 8-bit types). It is not a worst case:
	// 16-bit-per-channel images decode into image.RGBA64/NRGBA64 at twice that,
	// which is why the real guard is MaxDecodeBytes against the color model.
	BytesPerPixel = 4

	// MaxDecodeBytes bounds the pixel buffer of ONE decode. Pixels alone are the
	// wrong unit: at MaxPixels a 16-bit PNG is 128 MiB and an 8-bit one 64 MiB,
	// so a pixel-only bound silently doubles the budget for a format nothing
	// downscales client-side either.
	MaxDecodeBytes = MaxPixels * BytesPerPixel

	// MaxAspectRatio rejects extreme strips. A 16000x1000 image fits the pixel
	// budget yet is no more a profile picture than a bomb is, and it still
	// forces a huge single-dimension allocation.
	MaxAspectRatio = 20

	// maxConcurrentDecodes caps simultaneous decodes, because MaxDecodeBytes
	// alone is not a bound: the upload endpoints share one rate limiter with a
	// burst of 20, and 20 decodes at the byte bound is 1.3 GiB against a 512 MiB
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
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > MaxPixels {
		// The reason reaches the uploader verbatim, so say what to do about it.
		return fmt.Errorf("%w: %dx%d pixels — please crop or resize it to under %d megapixels",
			ErrImageTooLarge, cfg.Width, cfg.Height, MaxPixels/1_000_000)
	}
	// The header says how DEEP the pixels are as well as how many, and the
	// buffer is what has to fit: 16 bits per channel doubles the cost of the
	// same geometry.
	if bpp := decodedBytesPerPixel(cfg.ColorModel); pixels*bpp > MaxDecodeBytes {
		return fmt.Errorf("%w: %dx%d at %d bytes per pixel needs %d MB to decode (max %d MB) — 16 bits per channel costs twice what an ordinary 8-bit image costs, so re-saving it as 8-bit is usually enough",
			ErrImageTooLarge, cfg.Width, cfg.Height, bpp, pixels*bpp/1_000_000, int64(MaxDecodeBytes)/1_000_000)
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

// decodedBytesPerPixel is what one pixel costs in the image type a decoder
// produces for this color model — the number the byte budget has to be checked
// against, since image.DecodeConfig reveals the model for free.
//
// The 16-bit models are the reason this exists: image/png decodes a 16-bit PNG
// into image.RGBA64/NRGBA64 (pinned by TestCheckBoundsBudgetsByColorModel),
// which is 8 bytes per pixel, not the 4 an 8-bit RGBA image costs. Everything
// the registered decoders (jpeg, png,
// webp) can produce otherwise costs at most 4 — RGBA/NRGBA, CMYK and NYCbCrA at
// exactly 4, Gray/Gray16/Paletted/YCbCr below it — so 4 is the safe default for
// a model this switch does not name.
func decodedBytesPerPixel(model color.Model) int64 {
	switch model {
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	default:
		return BytesPerPixel
	}
}

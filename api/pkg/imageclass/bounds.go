package imageclass

import (
	"bytes"
	"errors"
	"fmt"
	"image"
)

const (
	// MaxPixels bounds the DECODED pixel count of an uploaded image. The
	// compressed size is no protection at all: a crafted 1-bit grayscale PNG
	// amplifies ~8,200x, so a 47 KiB file declaring 40000x40000 needs ~400 MiB
	// and a 190 KiB one ~1.5 GiB — against a 512 MiB container limit, with two
	// concurrent requests enough to OOM-kill the API. GOMEMLIMIT cannot help
	// either: the allocation is a single contiguous live []uint8.
	//
	// 40M px is comfortably above any real camera output (a 24 MP photo is
	// 24M px) while keeping one decode inside the container budget.
	MaxPixels = 40_000_000

	// MaxAspectRatio rejects extreme strips. A 40000x1000 image fits the pixel
	// budget yet is no more a profile picture than the bomb is, and it still
	// forces a huge single-dimension allocation.
	MaxAspectRatio = 20
)

// ErrImageTooLarge reports geometry a full decode cannot afford.
var ErrImageTooLarge = errors.New("image is too large to process")

// CheckBounds parses ONLY the image header (image.DecodeConfig: microseconds,
// no pixel buffer) and rejects geometry whose full decode would blow the
// container's memory budget. It MUST run before image.Decode — the whole
// vulnerability is that decoding is what allocates, and by then it is too late
// (measured: 5.6 µs to read the header versus 2.49 s and 397 MiB to decode the
// same 47 KiB file).
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
		return fmt.Errorf("%w: %dx%d pixels (max %d)", ErrImageTooLarge, cfg.Width, cfg.Height, MaxPixels)
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

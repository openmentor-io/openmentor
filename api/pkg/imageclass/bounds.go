package imageclass

import (
	"bytes"
	"errors"
	"fmt"
	"image"
)

const (
	// MaxPixels bounds the DECODED pixel count of an uploaded image, because the
	// compressed size bounds nothing: a crafted PNG amplifies thousands of
	// times, and the decode is one contiguous live []uint8, so GOMEMLIMIT cannot
	// soften it either.
	//
	// The bound is a memory budget, not a guess at what a camera produces: at
	// 16M px an RGBA decode measures ~74 MiB, so several concurrent uploads
	// still fit the API container's 512 MiB. It clears a 12 MP phone photo
	// (12.2M px); bigger originals have to be cropped or resized, which is what
	// a profile picture wants anyway.
	MaxPixels = 16_000_000

	// MaxAspectRatio rejects extreme strips. A 16000x1000 image fits the pixel
	// budget yet is no more a profile picture than a bomb is, and it still
	// forces a huge single-dimension allocation.
	MaxAspectRatio = 20
)

// ErrImageTooLarge reports geometry a full decode cannot afford.
var ErrImageTooLarge = errors.New("image is too large to process")

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

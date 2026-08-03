package imageclass

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"github.com/openmentor-io/openmentor/api/test/imagefixture"
)

func TestCheckBounds(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{
			// 100M px: a full decode needs ~95 MiB for a 12 KiB upload.
			name:    "gigapixel bomb",
			data:    imagefixture.BombPNG(t, 10000, 10000),
			wantErr: ErrImageTooLarge,
		},
		{
			// Inside the pixel budget, but a 50:1 strip is not a portrait.
			name:    "extreme aspect ratio",
			data:    imagefixture.BombPNG(t, 20000, 400),
			wantErr: ErrImageTooLarge,
		},
		{
			// 39.69M px of RGBA: the worst geometry a 40M px bound let through —
			// 150 KiB on the wire, 185 MiB of pixels, a third of the container
			// from a single request.
			name:    "largest payload a 40M px bound allowed",
			data:    imagefixture.RGBABombPNG(t, 6300, 6300),
			wantErr: ErrImageTooLarge,
		},
		{
			name: "ordinary profile photo",
			data: imagefixture.PhotoPNG(t, 400, 400),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckBounds(tt.data)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("CheckBounds() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CheckBounds() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckBoundsRejectsGarbage(t *testing.T) {
	if err := CheckBounds([]byte("not an image")); err == nil {
		t.Fatal("CheckBounds() error = nil, want a header parse failure")
	}
}

// TestClassifyBytesRejectsBombWithoutDecoding is the P3 regression: Classify
// used to call image.Decode as its very first action, so the bomb's pixels were
// allocated before anything could object. The allocation delta is the
// assertion — a full decode of 100M pixels cannot happen in a few kilobytes.
func TestClassifyBytesRejectsBombWithoutDecoding(t *testing.T) {
	bomb := imagefixture.BombPNG(t, 10000, 10000)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	style, err := ClassifyBytes(bomb)

	runtime.ReadMemStats(&after)

	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("ClassifyBytes() error = %v, want ErrImageTooLarge", err)
	}
	if style != "" {
		t.Errorf("ClassifyBytes() style = %q, want empty", style)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
		t.Errorf("rejecting the bomb allocated %d bytes: the pixels were decoded after all", allocated)
	}
}

// TestClassifyBytesWorstAcceptedGeometryFitsBudget measures what an attacker
// gets for free at the bound: the largest RGBA geometry that passes every
// validator. MaxPixels is only worth what this number says it is — several
// concurrent decodes have to fit the container's 512 MiB.
func TestClassifyBytesWorstAcceptedGeometryFitsBudget(t *testing.T) {
	// The square at the bound, whatever the bound currently is.
	side := int(math.Sqrt(MaxPixels))
	worst := imagefixture.RGBABombPNG(t, side, side)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	if _, err := ClassifyBytes(worst); err != nil {
		t.Fatalf("ClassifyBytes(%dx%d) error = %v, want the geometry at the bound to be accepted", side, side, err)
	}

	runtime.ReadMemStats(&after)

	// 16M px measures ~74 MiB, ~122 MiB under -race (which allocates its own
	// shadow state); the 40M px bound this replaced measured 185 MiB.
	const ceiling = 160 << 20
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > ceiling {
		t.Errorf("worst accepted decode (%dx%d) allocated %d bytes, ceiling %d: MaxPixels is too high for a 512 MiB container",
			side, side, allocated, ceiling)
	}
}

func TestClassifyBytesAcceptsFullSizePhoto(t *testing.T) {
	// 2000x2000 is a normal phone upload: it must still decode and classify.
	style, err := ClassifyBytes(imagefixture.PhotoPNG(t, 2000, 2000))
	if err != nil {
		t.Fatalf("ClassifyBytes() error = %v, want nil", err)
	}
	if style != StyleHero {
		t.Errorf("ClassifyBytes() style = %q, want %q for a near-white image", style, StyleHero)
	}
}

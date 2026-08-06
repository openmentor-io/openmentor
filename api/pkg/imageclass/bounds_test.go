package imageclass

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"

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
			// 39.69M px of RGBA: 150 KiB on the wire, 185 MiB decoded, and legal
			// under every check except the pixel bound.
			name:    "39.69M px RGBA square",
			data:    imagefixture.RGBABombPNG(t, 6300, 6300),
			wantErr: ErrImageTooLarge,
		},
		{
			// 16M px of 16-bit RGBA: inside the pixel bound, yet 128 MiB decoded
			// because image.NRGBA64 costs 8 bytes a pixel — the whole decode
			// share of the container in ONE upload.
			name:    "16M px 16-bit RGBA square",
			data:    imagefixture.RGBA64BombPNG(t, 4000, 4000),
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

// TestClassifyBytesRejectsBombWithoutDecoding pins the guard ORDER: the bomb has
// to be refused before image.Decode allocates, so the allocation delta is the
// assertion — a full decode of 100M pixels cannot happen in a few kilobytes.
func TestClassifyBytesRejectsBombWithoutDecoding(t *testing.T) {
	bomb := imagefixture.BombPNG(t, 10000, 10000)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	style, err := ClassifyBytes(context.Background(), bomb)

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

// TestDecodeBudgetFitsContainer is the arithmetic behind the decode budget: the
// biggest buffer one decode may allocate, times the decodes allowed to run at
// once, has to stay inside the share of the API container (512 MiB,
// infra/docker-compose.yml) that uploads may occupy — the rest is the Go
// runtime, the pgx pool and the request payloads. MaxDecodeBytes alone cannot
// carry this: the rate limiter in front of the upload endpoints bursts to 20.
func TestDecodeBudgetFitsContainer(t *testing.T) {
	const containerBytes = 512 << 20
	const decodeShare = containerBytes / 4

	peak := int64(DecodeBudgetBytes)
	if peak != int64(MaxDecodeBytes)*maxConcurrentDecodes {
		t.Fatalf("DecodeBudgetBytes = %d, want %d: the exported budget must stay the product it claims to be",
			peak, int64(MaxDecodeBytes)*maxConcurrentDecodes)
	}
	if peak > decodeShare {
		t.Errorf("decode budget is %d bytes (%d B x %d concurrent), over the %d-byte share of a %d-byte container",
			peak, int64(MaxDecodeBytes), maxConcurrentDecodes, decodeShare, containerBytes)
	}
}

// TestCheckBoundsBudgetsByColorModel is what makes the budget above honest: it
// is stated in BYTES, so the depth of a pixel counts. The same geometry at the
// pixel bound is 64 MiB as 8-bit RGBA and 128 MiB as 16-bit — the second is the
// whole decode share of the container in ONE upload, and a pixel-only bound
// admitted it.
func TestCheckBoundsBudgetsByColorModel(t *testing.T) {
	// The geometry is legal in every case: exactly at the pixel bound.
	const side = 4000

	tests := []struct {
		name  string
		model color.Model
		want  bool // rejected?
	}{
		{"8-bit RGBA (4 B/px)", color.NRGBAModel, false},
		{"16-bit RGBA (8 B/px)", color.NRGBA64Model, true},
		{"16-bit RGB (8 B/px)", color.RGBA64Model, true},
		{"16-bit gray (2 B/px)", color.Gray16Model, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkConfigBounds(image.Config{Width: side, Height: side, ColorModel: tt.model})
			if got := errors.Is(err, ErrImageTooLarge); got != tt.want {
				t.Errorf("checkConfigBounds(%dx%d) error = %v, want rejected = %v", side, side, err, tt.want)
			}
		})
	}

	// And the mapping is not wishful thinking: a real 16-bit PNG header reports a
	// 64-bit model, so the geometry above is refused end to end.
	if err := CheckBounds(imagefixture.RGBA64BombPNG(t, side, side)); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("CheckBounds(%dx%d 16-bit PNG) error = %v, want ErrImageTooLarge", side, side, err)
	}
}

// TestClassifyBytesWorstAcceptedGeometryFitsBudget measures what an attacker
// gets for free at the bound: the largest geometry of each pixel format that
// passes every validator. MaxDecodeBytes is what TestDecodeBudgetFitsContainer
// multiplies by, so it has to be measured rather than assumed — per format,
// since that is where the pixel-only bound was wrong.
func TestClassifyBytesWorstAcceptedGeometryFitsBudget(t *testing.T) {
	tests := []struct {
		name          string
		png           func(tb testing.TB, width, height int) []byte
		bytesPerPixel int64
	}{
		{"8-bit RGBA", imagefixture.RGBABombPNG, 4},
		{"16-bit RGBA", imagefixture.RGBA64BombPNG, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The square at the byte bound, whatever the bound currently is.
			side := int(math.Sqrt(float64(int64(MaxDecodeBytes) / tt.bytesPerPixel)))
			worst := tt.png(t, side, side)

			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)

			if _, err := ClassifyBytes(context.Background(), worst); err != nil {
				t.Fatalf("ClassifyBytes(%dx%d) error = %v, want the geometry at the bound to be accepted", side, side, err)
			}

			runtime.ReadMemStats(&after)

			// One decode at the bound measures ~74 MiB, ~122 MiB under -race
			// (which allocates its own shadow state), around the 64 MiB the
			// pixel buffer costs.
			const ceiling = 160 << 20
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > ceiling {
				t.Errorf("worst accepted decode (%dx%d) allocated %d bytes, ceiling %d: MaxDecodeBytes is too high for a 512 MiB container",
					side, side, allocated, ceiling)
			}
		})
	}
}

// TestClassifyBytesShedsDecodeWhenSlotsAreTaken: with every slot held, an upload
// waits decodeQueueWait and then gives up on classification instead of adding
// another pixel buffer to the heap.
func TestClassifyBytesShedsDecodeWhenSlotsAreTaken(t *testing.T) {
	fillDecodeSlots(t)

	start := time.Now()
	style, err := ClassifyBytes(context.Background(), imagefixture.PhotoPNG(t, 400, 400))
	waited := time.Since(start)

	if !errors.Is(err, ErrDecoderBusy) {
		t.Fatalf("ClassifyBytes() error = %v, want ErrDecoderBusy", err)
	}
	if style != "" {
		t.Errorf("ClassifyBytes() style = %q, want empty (the caller substitutes the default style)", style)
	}
	if waited < decodeQueueWait {
		t.Errorf("gave up after %v, want at least the %v queue wait", waited, decodeQueueWait)
	}
}

// TestClassifyBytesCanceledWaitReturnsContextError: a client that disconnects
// while queued must not keep a goroutine waiting for a slot.
func TestClassifyBytesCanceledWaitReturnsContextError(t *testing.T) {
	fillDecodeSlots(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ClassifyBytes(ctx, imagefixture.PhotoPNG(t, 400, 400)); !errors.Is(err, context.Canceled) {
		t.Fatalf("ClassifyBytes() error = %v, want context.Canceled", err)
	}
}

// TestClassifyBytesReleasesDecodeSlots: a burst well over the slot count has to
// leave every slot free again. A leaked slot would shed classification for every
// later upload, permanently.
func TestClassifyBytesReleasesDecodeSlots(t *testing.T) {
	photo := imagefixture.PhotoPNG(t, 400, 400)
	const callers = 4 * maxConcurrentDecodes

	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = ClassifyBytes(context.Background(), photo)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		// Shedding is a legitimate outcome under contention; nothing else is.
		if err != nil && !errors.Is(err, ErrDecoderBusy) {
			t.Errorf("caller %d: ClassifyBytes() error = %v", i, err)
		}
	}
	if held := len(decodeSlots); held != 0 {
		t.Errorf("%d decode slots still held after the burst, want 0", held)
	}
}

func TestClassifyBytesAcceptsFullSizePhoto(t *testing.T) {
	// 2000x2000 is a normal phone upload: it must still decode and classify.
	style, err := ClassifyBytes(context.Background(), imagefixture.PhotoPNG(t, 2000, 2000))
	if err != nil {
		t.Fatalf("ClassifyBytes() error = %v, want nil", err)
	}
	if style != StyleHero {
		t.Errorf("ClassifyBytes() style = %q, want %q for a near-white image", style, StyleHero)
	}
}

// fillDecodeSlots takes every decode slot for the duration of the test.
func fillDecodeSlots(tb testing.TB) {
	tb.Helper()
	for range maxConcurrentDecodes {
		decodeSlots <- struct{}{}
	}
	tb.Cleanup(func() {
		for range maxConcurrentDecodes {
			<-decodeSlots
		}
	})
}

// TestClassifyBytesFreeSlotBeatsCanceledContext: a canceled request context
// must not cost the classification when a slot is sitting free.
func TestClassifyBytesFreeSlotBeatsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	photo := imagefixture.PhotoPNG(t, 400, 400)
	for i := range 20 {
		style, err := ClassifyBytes(ctx, photo)
		if err != nil {
			t.Fatalf("attempt %d: ClassifyBytes() error = %v, want nil with every slot free", i, err)
		}
		if style != StyleHero {
			t.Fatalf("attempt %d: style = %q, want %q", i, style, StyleHero)
		}
	}
}

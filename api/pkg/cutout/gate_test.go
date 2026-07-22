package cutout

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// encode renders an RGBA image to PNG bytes for QualityGate.
func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func blank(w, h int) *image.NRGBA {
	return image.NewNRGBA(image.Rect(0, 0, w, h))
}

func fillRect(img *image.NRGBA, x0, y0, x1, y1 int) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 150, B: 120, A: 255})
		}
	}
}

func TestQualityGate_AcceptsCentralSubject(t *testing.T) {
	// A portrait-like blob: one solid region covering ~40% of the frame.
	img := blank(200, 200)
	fillRect(img, 50, 40, 150, 200)

	res, err := QualityGate(encode(t, img))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected pass, got rejection: %s (coverage=%.2f dominance=%.2f)", res.Reason, res.Coverage, res.Dominance)
	}
}

func TestQualityGate_RejectsEmptyMask(t *testing.T) {
	res, err := QualityGate(encode(t, blank(200, 200)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected rejection for an all-transparent cutout")
	}
}

func TestQualityGate_RejectsFullMask(t *testing.T) {
	// Background not removed: everything opaque.
	img := blank(200, 200)
	fillRect(img, 0, 0, 200, 200)

	res, err := QualityGate(encode(t, img))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected rejection for a fully-opaque cutout")
	}
}

func TestQualityGate_RejectsFragmentedMask(t *testing.T) {
	// Shredded mask: many small disconnected blobs, none dominant. Blobs are
	// 8px with 8px gaps so they stay disconnected on the 128px gate grid.
	img := blank(256, 256)
	for y := 0; y < 256; y += 16 {
		for x := (y / 16 % 2) * 16; x < 256; x += 32 {
			fillRect(img, x, y, x+8, y+8)
		}
	}

	res, err := QualityGate(encode(t, img))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatalf("expected rejection for a fragmented mask (coverage=%.2f dominance=%.2f)", res.Coverage, res.Dominance)
	}
}

func TestQualityGate_ReasonCodes(t *testing.T) {
	// Empty mask -> too_small.
	if res, _ := QualityGate(encode(t, blank(200, 200))); res.Code != ReasonTooSmall {
		t.Fatalf("empty mask: want code %q, got %q", ReasonTooSmall, res.Code)
	}
	// Full mask -> near_full.
	full := blank(200, 200)
	fillRect(full, 0, 0, 200, 200)
	if res, _ := QualityGate(encode(t, full)); res.Code != ReasonNearFull {
		t.Fatalf("full mask: want code %q, got %q", ReasonNearFull, res.Code)
	}
	// Fragmented mask -> fragmented.
	frag := blank(256, 256)
	for y := 0; y < 256; y += 16 {
		for x := (y / 16 % 2) * 16; x < 256; x += 32 {
			fillRect(frag, x, y, x+8, y+8)
		}
	}
	if res, _ := QualityGate(encode(t, frag)); res.Code != ReasonFragmented {
		t.Fatalf("fragmented mask: want code %q, got %q", ReasonFragmented, res.Code)
	}
	// Accepted mask -> no code.
	ok := blank(200, 200)
	fillRect(ok, 50, 40, 150, 200)
	if res, _ := QualityGate(encode(t, ok)); res.Code != "" {
		t.Fatalf("accepted mask: want empty code, got %q", res.Code)
	}
}

func TestQualityGate_RejectsGarbageBytes(t *testing.T) {
	if _, err := QualityGate([]byte("not a png")); err == nil {
		t.Fatal("expected decode error for garbage bytes")
	}
}

func TestClient_DisabledWhenNoURL(t *testing.T) {
	if New(Config{}).Enabled() {
		t.Fatal("client with empty ServiceURL must be disabled")
	}
	if !New(Config{ServiceURL: "http://rembg:7000"}).Enabled() {
		t.Fatal("client with ServiceURL must be enabled")
	}
}

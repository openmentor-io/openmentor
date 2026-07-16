package imageclass

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// solidImage returns a w x h image filled with a single color.
func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// noisyImage returns a w x h image of deterministic random noise.
func noisyImage(w, h int) *image.RGBA {
	rng := rand.New(rand.NewSource(42))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(rng.Intn(256)),
				G: uint8(rng.Intn(256)),
				B: uint8(rng.Intn(256)),
				A: 255,
			})
		}
	}
	return img
}

// portraitOnLightBackground simulates a studio portrait: near-white
// background with a dark subject in the center (the border stays light).
func portraitOnLightBackground(w, h int) *image.RGBA {
	img := solidImage(w, h, color.RGBA{R: 245, G: 243, B: 240, A: 255})
	for y := h / 4; y < h-h/8; y++ {
		for x := w / 3; x < w-w/3; x++ {
			img.Set(x, y, color.RGBA{R: 60, G: 45, B: 40, A: 255})
		}
	}
	return img
}

func TestClassifyImage(t *testing.T) {
	tests := []struct {
		name string
		img  image.Image
		want string
	}{
		{"solid near-white background", solidImage(200, 200, color.RGBA{R: 245, G: 245, B: 245, A: 255}), StyleHero},
		{"portrait on light background", portraitOnLightBackground(200, 200), StyleHero},
		{"solid dark background", solidImage(200, 200, color.RGBA{R: 40, G: 40, B: 40, A: 255}), StyleFrame},
		{"solid mid-grey background (uniform but not bright)", solidImage(200, 200, color.RGBA{R: 150, G: 150, B: 150, A: 255}), StyleFrame},
		{"busy noise (bright-ish but not uniform)", noisyImage(200, 200), StyleFrame},
		{"tiny image, light", solidImage(3, 3, color.White), StyleHero},
		{"tiny image, dark", solidImage(3, 3, color.Black), StyleFrame},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyImage(tt.img); got != tt.want {
				t.Errorf("ClassifyImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClassifyIgnoresCenter pins the border sampling: a light border around
// a busy center is still 'hero' (only the outer frame is sampled).
func TestClassifyIgnoresCenter(t *testing.T) {
	img := noisyImage(200, 200)
	// Paint a 6%+ light border over the noise.
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			if x < 14 || x >= 186 || y < 14 || y >= 186 {
				img.Set(x, y, color.RGBA{R: 248, G: 248, B: 248, A: 255})
			}
		}
	}
	if got := ClassifyImage(img); got != StyleHero {
		t.Errorf("ClassifyImage() = %q, want %q (light border, busy center)", got, StyleHero)
	}
}

func TestClassifyDecodesPNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(64, 64, color.White)); err != nil {
		t.Fatalf("png encode: %v", err)
	}

	got, err := ClassifyBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("ClassifyBytes() error: %v", err)
	}
	if got != StyleHero {
		t.Errorf("ClassifyBytes(png) = %q, want %q", got, StyleHero)
	}
}

func TestClassifyDecodesJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, noisyImage(64, 64), nil); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}

	got, err := ClassifyBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("ClassifyBytes() error: %v", err)
	}
	if got != StyleFrame {
		t.Errorf("ClassifyBytes(jpeg noise) = %q, want %q", got, StyleFrame)
	}
}

func TestClassifyBase64(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(32, 32, color.White)); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	tests := []struct {
		name  string
		input string
	}{
		{"raw base64", encoded},
		{"data URI", "data:image/png;base64," + encoded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClassifyBase64(tt.input)
			if err != nil {
				t.Fatalf("ClassifyBase64() error: %v", err)
			}
			if got != StyleHero {
				t.Errorf("ClassifyBase64() = %q, want %q", got, StyleHero)
			}
		})
	}
}

func TestClassificationMetrics(t *testing.T) {
	metrics.Init("imageclass-test")

	var heroBuf bytes.Buffer
	if err := png.Encode(&heroBuf, solidImage(32, 32, color.White)); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	var frameBuf bytes.Buffer
	if err := png.Encode(&frameBuf, solidImage(32, 32, color.Black)); err != nil {
		t.Fatalf("png encode: %v", err)
	}

	// hero via ClassifyBytes; hero again via ClassifyBase64 (counts once per
	// classification, not once per layer).
	if _, err := ClassifyBytes(heroBuf.Bytes()); err != nil {
		t.Fatalf("ClassifyBytes() error: %v", err)
	}
	if _, err := ClassifyBase64(base64.StdEncoding.EncodeToString(heroBuf.Bytes())); err != nil {
		t.Fatalf("ClassifyBase64() error: %v", err)
	}
	// frame
	if _, err := ClassifyBytes(frameBuf.Bytes()); err != nil {
		t.Fatalf("ClassifyBytes() error: %v", err)
	}
	// errors: undecodable bytes, invalid base64
	if _, err := ClassifyBytes([]byte("not an image")); err == nil {
		t.Fatal("ClassifyBytes() expected error, got nil")
	}
	if _, err := ClassifyBase64("!!!not-base64!!!"); err == nil {
		t.Fatal("ClassifyBase64() expected error, got nil")
	}

	if got := testutil.ToFloat64(metrics.PhotoClassifications.WithLabelValues(StyleHero)); got != 2 {
		t.Errorf("hero classifications = %v, want 2", got)
	}
	if got := testutil.ToFloat64(metrics.PhotoClassifications.WithLabelValues(StyleFrame)); got != 1 {
		t.Errorf("frame classifications = %v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.PhotoClassifications.WithLabelValues("error")); got != 2 {
		t.Errorf("error classifications = %v, want 2", got)
	}
}

func TestClassifyBase64Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid base64", "!!!not-base64!!!"},
		{"invalid data URI", "data:image/png;base64"},
		{"valid base64, not an image", base64.StdEncoding.EncodeToString([]byte("hello"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ClassifyBase64(tt.input); err == nil {
				t.Errorf("ClassifyBase64(%q) expected error, got nil", tt.name)
			}
		})
	}
}

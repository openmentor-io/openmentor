// Package imageclass classifies mentor profile pictures into a display
// "photo style" for the frontend: photos on a light, uniform background
// (studio portraits, generated avatars) render as 'hero' (full-bleed),
// everything else renders inside a 'frame'.
//
// The classifier samples the outer border of the image (the ~6% frame of
// pixels along each edge) and computes the mean and standard deviation of
// the pixel luminance there. A bright (mean > ~0.78) and uniform
// (std-dev < ~0.12) border means the subject sits on a light plain
// background -> 'hero'; anything darker or busier -> 'frame'.
package imageclass

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"math"
	"strings"

	// Register the decoders for the profile-picture content types the API
	// accepts (image/jpeg, image/png, image/webp).
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Photo styles stored in mentors.photo_style.
const (
	StyleHero  = "hero"
	StyleFrame = "frame"
)

const (
	// borderFraction is the width of the sampled border, as a fraction of
	// each image dimension (the outer ~6% frame).
	borderFraction = 0.06
	// heroMeanLuminance is the minimum mean border luminance (0..1) for a
	// 'hero' classification.
	heroMeanLuminance = 0.78
	// heroMaxStdDev is the maximum border luminance standard deviation
	// (0..1) for a 'hero' classification.
	heroMaxStdDev = 0.12
)

// Classify decodes an image (jpeg, png or webp) from r and classifies it.
func Classify(r io.Reader) (string, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}
	return ClassifyImage(img), nil
}

// ClassifyBytes classifies raw (already decoded from base64) image bytes.
func ClassifyBytes(data []byte) (string, error) {
	return Classify(bytes.NewReader(data))
}

// ClassifyBase64 classifies a base64-encoded image, accepting both raw
// base64 and data URI format (data:image/png;base64,...) — the same input
// shape the profile-picture upload endpoints receive.
func ClassifyBase64(imageData string) (string, error) {
	if strings.HasPrefix(imageData, "data:") {
		parts := strings.SplitN(imageData, ",", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid data URI format")
		}
		imageData = parts[1]
	}
	raw, err := base64.StdEncoding.DecodeString(imageData)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 image: %w", err)
	}
	return ClassifyBytes(raw)
}

// ClassifyImage classifies an already-decoded image.
func ClassifyImage(img image.Image) string {
	mean, stdDev, ok := borderLuminance(img)
	if !ok {
		return StyleFrame
	}
	if mean > heroMeanLuminance && stdDev < heroMaxStdDev {
		return StyleHero
	}
	return StyleFrame
}

// borderLuminance computes the mean and standard deviation of the
// luminance (Rec. 709, normalized to 0..1) of the pixels in the outer
// border frame of the image. ok is false when the image has no pixels.
func borderLuminance(img image.Image) (mean, stdDev float64, ok bool) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}

	borderW := borderThickness(width)
	borderH := borderThickness(height)

	var sum, sumSq float64
	var count int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		inYBorder := y < bounds.Min.Y+borderH || y >= bounds.Max.Y-borderH
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !inYBorder && x >= bounds.Min.X+borderW && x < bounds.Max.X-borderW {
				// Inside the frame: skip straight to the right border.
				x = bounds.Max.X - borderW - 1
				continue
			}
			l := luminance(img.At(x, y).RGBA())
			sum += l
			sumSq += l * l
			count++
		}
	}
	if count == 0 {
		return 0, 0, false
	}

	mean = sum / float64(count)
	variance := sumSq/float64(count) - mean*mean
	if variance < 0 {
		variance = 0 // guard against floating point rounding
	}
	return mean, math.Sqrt(variance), true
}

// borderThickness returns the sampled border thickness for one dimension
// (at least 1 pixel, capped at half the dimension).
func borderThickness(size int) int {
	t := int(math.Round(float64(size) * borderFraction))
	if t < 1 {
		t = 1
	}
	if t > size/2 {
		t = (size + 1) / 2
	}
	return t
}

// luminance converts color.Color.RGBA() 16-bit premultiplied components to
// a Rec. 709 relative luminance in 0..1.
func luminance(r, g, b, _ uint32) float64 {
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 65535.0
}

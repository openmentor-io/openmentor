// Package imagefixture builds profile-picture payloads for tests: an ordinary
// photo, and the decompression bomb the photo endpoints must refuse. It is
// shared because the bomb has to be asserted on at three layers (imageclass,
// s3storage and the services) and generating it in-process — rather than
// checking a multi-megabyte fixture into the repo — is the whole point.
package imagefixture

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// BombPNG returns a valid 1-bit grayscale PNG of the declared size whose
// scanlines are all zero. It deflates to a few kilobytes while a full decode
// has to allocate width*height pixels — 10000x10000 is ~12 KiB on the wire and
// ~95 MiB decoded, an ~8,200x amplification.
func BombPNG(tb testing.TB, width, height int) []byte {
	tb.Helper()
	// bit depth 1, grayscale: one bit per pixel on the wire.
	return bombPNG(tb, width, height, 1, 0, 1+(width+7)/8)
}

// RGBABombPNG is BombPNG declared as 8-bit RGBA — the most expensive shape an
// upload can ask for, since every pixel costs 4 bytes once decoded (4x the
// grayscale bomb at the same geometry). This is what the pixel bound has to be
// sized against.
func RGBABombPNG(tb testing.TB, width, height int) []byte {
	tb.Helper()
	return bombPNG(tb, width, height, 8, 6, 1+width*4)
}

func bombPNG(tb testing.TB, width, height, bitDepth, colourType, rowBytes int) []byte {
	tb.Helper()

	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")

	header := make([]byte, 0, 13)
	header = binary.BigEndian.AppendUint32(header, uint32(width))  //nolint:gosec // test fixture dimensions
	header = binary.BigEndian.AppendUint32(header, uint32(height)) //nolint:gosec // test fixture dimensions
	// deflate, adaptive filtering, no interlace.
	header = append(header, byte(bitDepth), byte(colourType), 0, 0, 0)
	writeChunk(tb, &out, "IHDR", header)

	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	row := make([]byte, rowBytes) // filter byte + the scanline itself
	for y := 0; y < height; y++ {
		if _, err := zw.Write(row); err != nil {
			tb.Fatalf("deflate row %d: %v", y, err)
		}
	}
	if err := zw.Close(); err != nil {
		tb.Fatalf("close deflate stream: %v", err)
	}
	writeChunk(tb, &out, "IDAT", idat.Bytes())
	writeChunk(tb, &out, "IEND", nil)

	return out.Bytes()
}

// PhotoPNG returns an ordinary near-white PNG of the given size — a legitimate
// upload, which must keep working.
func PhotoPNG(tb testing.TB, width, height int) []byte {
	tb.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	light := color.RGBA{R: 250, G: 250, B: 250, A: 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, light)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func writeChunk(tb testing.TB, out *bytes.Buffer, chunkType string, data []byte) {
	tb.Helper()
	if err := binary.Write(out, binary.BigEndian, uint32(len(data))); err != nil {
		tb.Fatalf("write chunk length: %v", err)
	}
	crc := crc32.NewIEEE()
	for _, part := range [][]byte{[]byte(chunkType), data} {
		out.Write(part)
		crc.Write(part)
	}
	if err := binary.Write(out, binary.BigEndian, crc.Sum32()); err != nil {
		tb.Fatalf("write chunk crc: %v", err)
	}
}

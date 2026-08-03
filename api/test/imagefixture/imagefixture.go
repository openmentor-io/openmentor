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
// has to allocate width*height pixels: 10000x10000 measures ~12 KiB on the wire
// and ~95 MiB decoded, the same ~8,200x amplification the audit measured on a
// 40000x40000 file (397 MiB against a 512 MiB container).
func BombPNG(tb testing.TB, width, height int) []byte {
	tb.Helper()

	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")

	header := make([]byte, 0, 13)
	header = binary.BigEndian.AppendUint32(header, uint32(width))  //nolint:gosec // test fixture dimensions
	header = binary.BigEndian.AppendUint32(header, uint32(height)) //nolint:gosec // test fixture dimensions
	// bit depth 1, grayscale, deflate, adaptive filtering, no interlace.
	header = append(header, 1, 0, 0, 0, 0)
	writeChunk(tb, &out, "IHDR", header)

	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	row := make([]byte, 1+(width+7)/8) // filter byte + one bit per pixel
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

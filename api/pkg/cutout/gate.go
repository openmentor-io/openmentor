package cutout

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

// Gate thresholds. A good portrait cutout has a subject that occupies a
// meaningful part of the frame and forms one dominant connected region;
// masks outside these bounds are model failures (empty frame, background
// kept, shredded speckle) and must fall back to the 'frame' treatment.
const (
	// minCoverage/maxCoverage bound the fraction of opaque pixels.
	minCoverage = 0.08
	maxCoverage = 0.92
	// minDominance is the minimum share of opaque pixels that must belong to
	// the single largest connected region.
	minDominance = 0.60
	// alphaOpaque is the alpha threshold (0..255) above which a pixel counts
	// as subject.
	alphaOpaque = 128
	// gateGridSize is the max dimension of the downsampled grid used for the
	// coverage/connectivity analysis (keeps the gate O(gridSize²)).
	gateGridSize = 128
)

// Bounded gate rejection reason codes (used as a metric label — keep the
// cardinality fixed). Reason carries the human-readable message.
const (
	ReasonTooSmall   = "too_small"  // near-empty mask, subject too small
	ReasonNearFull   = "near_full"  // background not removed, near-full mask
	ReasonFragmented = "fragmented" // no single dominant connected region
)

// GateResult carries the gate verdict and its measurements (for logging).
type GateResult struct {
	OK        bool
	Coverage  float64 // opaque fraction of the frame
	Dominance float64 // largest connected region / all opaque
	Code      string  // bounded rejection reason code (Reason* / "" when OK)
	Reason    string  // human-readable rejection reason ("" when OK)
}

// QualityGate decodes a cutout PNG and decides whether it is good enough to
// ship as a hero card. It never returns an error for a "bad but decodable"
// cutout — that is a rejection, not an error.
func QualityGate(pngBytes []byte) (GateResult, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return GateResult{}, fmt.Errorf("failed to decode cutout png: %w", err)
	}
	return gateImage(img), nil
}

func gateImage(img image.Image) GateResult {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return GateResult{Reason: "empty image"}
	}

	// Downsample onto a boolean grid: true = opaque (subject).
	gw, gh := gridDims(w, h)
	grid := make([]bool, gw*gh)
	opaque := 0
	for gy := 0; gy < gh; gy++ {
		for gx := 0; gx < gw; gx++ {
			// Sample the pixel at the center of this grid cell.
			px := bounds.Min.X + (gx*w+w/2)/gw
			py := bounds.Min.Y + (gy*h+h/2)/gh
			_, _, _, a := img.At(px, py).RGBA()
			if a>>8 >= alphaOpaque {
				grid[gy*gw+gx] = true
				opaque++
			}
		}
	}

	total := gw * gh
	coverage := float64(opaque) / float64(total)
	if coverage < minCoverage {
		return GateResult{Coverage: coverage, Code: ReasonTooSmall, Reason: "subject too small (near-empty mask)"}
	}
	if coverage > maxCoverage {
		return GateResult{Coverage: coverage, Code: ReasonNearFull, Reason: "background not removed (near-full mask)"}
	}

	dominance := largestRegionShare(grid, gw, gh, opaque)
	if dominance < minDominance {
		return GateResult{Coverage: coverage, Dominance: dominance, Code: ReasonFragmented, Reason: "mask is fragmented"}
	}

	return GateResult{OK: true, Coverage: coverage, Dominance: dominance}
}

// gridDims scales (w, h) so the longer side is gateGridSize, preserving
// aspect ratio (minimum 1).
func gridDims(w, h int) (int, int) {
	if w >= h {
		gw := gateGridSize
		gh := h * gateGridSize / w
		if gh < 1 {
			gh = 1
		}
		return gw, gh
	}
	gh := gateGridSize
	gw := w * gateGridSize / h
	if gw < 1 {
		gw = 1
	}
	return gw, gh
}

// largestRegionShare returns the size of the largest 4-connected true-region
// divided by the total number of true cells.
func largestRegionShare(grid []bool, gw, gh, opaque int) float64 {
	if opaque == 0 {
		return 0
	}
	visited := make([]bool, len(grid))
	queue := make([]int, 0, opaque)
	largest := 0

	for start := range grid {
		if !grid[start] || visited[start] {
			continue
		}
		// BFS flood fill from this cell.
		size := 0
		visited[start] = true
		queue = append(queue[:0], start)
		for len(queue) > 0 {
			cur := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			size++
			x, y := cur%gw, cur/gw
			if x > 0 {
				tryVisit(grid, visited, &queue, cur-1)
			}
			if x < gw-1 {
				tryVisit(grid, visited, &queue, cur+1)
			}
			if y > 0 {
				tryVisit(grid, visited, &queue, cur-gw)
			}
			if y < gh-1 {
				tryVisit(grid, visited, &queue, cur+gw)
			}
		}
		if size > largest {
			largest = size
		}
	}

	return float64(largest) / float64(opaque)
}

func tryVisit(grid, visited []bool, queue *[]int, idx int) {
	if grid[idx] && !visited[idx] {
		visited[idx] = true
		*queue = append(*queue, idx)
	}
}

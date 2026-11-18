package main

import (
	"image"
	"math/bits"
)

// dHash computes a 64-bit difference hash: the image is reduced to a 9x8
// grayscale grid and each bit records whether a cell is brighter than its
// right neighbour. Re-encoded, resized or lightly edited copies of an image
// produce hashes that differ in only a few bits.
func dHash(img image.Image) uint64 {
	const cols, rows = 9, 8
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return 0
	}

	lum := luminanceFunc(img)
	// Sample at most ~256 points per axis; the grid is tiny, so this loses
	// nothing and keeps large photos fast.
	stepX, stepY := max(1, w/256), max(1, h/256)

	var sum, cnt [rows][cols]float64
	for y := 0; y < h; y += stepY {
		r := y * rows / h
		for x := 0; x < w; x += stepX {
			c := x * cols / w
			sum[r][c] += lum(b.Min.X+x, b.Min.Y+y)
			cnt[r][c]++
		}
	}

	var hash uint64
	for r := 0; r < rows; r++ {
		for c := 0; c < cols-1; c++ {
			left := sum[r][c] / max(cnt[r][c], 1)
			right := sum[r][c+1] / max(cnt[r][c+1], 1)
			hash <<= 1
			if left > right {
				hash |= 1
			}
		}
	}
	return hash
}

// luminanceFunc returns a fast per-pixel luminance reader (0-255).
func luminanceFunc(img image.Image) func(x, y int) float64 {
	switch m := img.(type) {
	case *image.YCbCr: // JPEG
		return func(x, y int) float64 { return float64(m.Y[m.YOffset(x, y)]) }
	case *image.Gray:
		return func(x, y int) float64 { return float64(m.Pix[m.PixOffset(x, y)]) }
	default:
		return func(x, y int) float64 {
			r, g, b, _ := m.At(x, y).RGBA()
			return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257
		}
	}
}

func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

//go:build cgo

package graphics

import (
	"math"
	"strconv"
	"strings"
)

type point struct {
	x, y float64
}

type rect struct {
	x, y, width, height float64
}

func (r rect) maxX() float64 {
	return r.x + r.width
}

func (r rect) maxY() float64 {
	return r.y + r.height
}

func (r rect) center() point {
	return point{r.x + r.width/2, r.y + r.height/2}
}

func (r rect) json() []float64 {
	return []float64{rounded(r.x), rounded(r.y), rounded(r.width), rounded(r.height)}
}

func (r rect) String() string {
	parts := make([]string, 0, 4)
	for _, value := range r.json() {
		parts = append(parts, strconv.FormatFloat(value, 'g', -1, 64))
	}
	return strings.Join(parts, ", ")
}

func (p point) json() []float64 {
	return []float64{rounded(p.x), rounded(p.y)}
}

type affine struct {
	a, b, c, d, tx, ty float64
}

func scaling(sx, sy float64) affine {
	return affine{a: sx, d: sy}
}

func (t affine) apply(p point) point {
	return point{t.a*p.x + t.c*p.y + t.tx, t.b*p.x + t.d*p.y + t.ty}
}

func (t affine) applyRect(r rect) rect {
	corners := [4]point{t.apply(point{r.x, r.y}), t.apply(point{r.maxX(), r.y}), t.apply(point{r.x, r.maxY()}), t.apply(point{r.maxX(), r.maxY()})}
	left, top, right, bottom := corners[0].x, corners[0].y, corners[0].x, corners[0].y
	for _, corner := range corners[1:] {
		left, right = min(left, corner.x), max(right, corner.x)
		top, bottom = min(top, corner.y), max(bottom, corner.y)
	}
	return rect{left, top, right - left, bottom - top}
}

func rounded(value float64) float64 {
	return roundedTo(value, 2)
}

func roundedTo(value float64, places float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	factor := math.Pow(10, places)
	return math.Round(value*factor) / factor
}

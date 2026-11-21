//go:build cgo && (darwin || android)

package graphics

import (
	"bytes"
	"cmp"
	"context"
	"slices"
)

const largestComponentCount = 10000

type component struct {
	minX, minY, maxX, maxY int
	count                  int
	sumX, sumY             float64
}

func (c component) rect() rect {
	return rect{float64(c.minX), float64(c.minY), float64(c.maxX - c.minX + 1), float64(c.maxY - c.minY + 1)}
}

func (c component) centroid() point {
	return point{c.sumX/float64(c.count) + 0.5, c.sumY/float64(c.count) + 0.5}
}

type span struct {
	y, start, end int
}

func (c *component) merge(s span) {
	count := s.end - s.start + 1
	sumX := float64(count) * float64(s.start+s.end) / 2
	sumY := float64(count * s.y)
	if c.count == 0 {
		*c = component{minX: s.start, minY: s.y, maxX: s.end, maxY: s.y, count: count, sumX: sumX, sumY: sumY}
		return
	}
	c.minX, c.maxX = min(c.minX, s.start), max(c.maxX, s.end)
	c.minY, c.maxY = min(c.minY, s.y), max(c.maxY, s.y)
	c.count += count
	c.sumX += sumX
	c.sumY += sumY
}

func root(parents []int, label int) int {
	current := label
	for parents[current] != current {
		parents[current] = parents[parents[current]]
		current = parents[current]
	}
	return current
}

func join(parents []int, a, b int) {
	first, second := root(parents, a), root(parents, b)
	if first != second {
		parents[max(first, second)] = min(first, second)
	}
}

func components(ctx context.Context, binary *raster, minimumPixels int) ([]component, error) {
	var runs []span
	var parents []int
	previousStart := 0
	for y := range binary.height {
		if y%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		row := binary.row(y)
		currentStart := len(runs)
		for x := 0; x < binary.width; {
			found := bytes.IndexByte(row[x:], 255)
			if found < 0 {
				break
			}
			start := x + found
			end := binary.width - 1
			if rest := bytes.IndexByte(row[start:], 0); rest >= 0 {
				end = start + rest - 1
			}
			parents = append(parents, len(runs))
			runs = append(runs, span{y: y, start: start, end: end})
			x = end + 1
		}
		above := previousStart
		for current := currentStart; current < len(runs); current++ {
			for above < currentStart && runs[above].end < runs[current].start-1 {
				above++
			}
			for index := above; index < currentStart && runs[index].start <= runs[current].end+1; index++ {
				join(parents, current, index)
			}
		}
		previousStart = currentStart
	}
	merged := make([]component, len(runs))
	for index, found := range runs {
		merged[root(parents, index)].merge(found)
	}
	kept := slices.DeleteFunc(merged, func(c component) bool {
		return c.count == 0 || c.count < minimumPixels
	})
	slices.SortStableFunc(kept, func(a, b component) int {
		return cmp.Or(cmp.Compare(a.minY, b.minY), cmp.Compare(a.minX, b.minX))
	})
	return kept[:min(len(kept), largestComponentCount)], nil
}

func mergedRegions(ctx context.Context, mask *raster, radius int) ([]rect, error) {
	merged := mask
	if radius > 0 {
		padded := newRaster(mask.width+radius*2, mask.height+radius*2, 1)
		mask.copyInto(padded, radius, radius)
		merged = padded.morphed(radius, true)
	}
	found, err := components(ctx, merged, 1)
	if err != nil {
		return nil, err
	}
	rects := make([]rect, len(found))
	for index, item := range found {
		bounds := item.rect()
		rects[index] = rect{bounds.x, bounds.y, bounds.width - float64(radius*2), bounds.height - float64(radius*2)}
	}
	return rects, nil
}

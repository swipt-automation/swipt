//go:build cgo && (darwin || android)

package graphics

import (
	"cmp"
	"context"
	"math"
	"slices"
)

const (
	coarsestSide = 8
	deepestLevel = 5
	refineRadius = 2
)

type plane struct {
	values []float32
	width  int
	height int
}

func newPlane(width, height int) *plane {
	return &plane{values: make([]float32, max(width*height, 1)), width: width, height: height}
}

func channelPlane(r *raster, channel int) *plane {
	var weights [4]int
	weights[channel] = 1
	return planeOf(mixed(r, weights, 1))
}

type kernel struct {
	centered  *plane
	weights   *plane
	weightSum float32
	variance  float32
}

func newKernel(values, weights *plane) (*kernel, error) {
	weightSum := sum(weights.values)
	if weightSum <= 0.5 {
		return nil, failure(nameRange, "the template is transparent")
	}
	mean := dot(values.values, weights.values) / weightSum
	centered := centeredProduct(values, weights, mean)
	variance := dot(centered.values, values.values) - mean*sum(centered.values)
	return &kernel{centered: centered, weights: weights, weightSum: weightSum, variance: variance}, nil
}

func flat(weightSum float32) float32 {
	return 1.6e-5 * weightSum
}

func correlation(sums [3]float32, k *kernel) float32 {
	variance := sums[2] - sums[1]*sums[1]/k.weightSum
	if variance <= flat(k.weightSum) || k.variance <= 0 {
		return 0
	}
	return sums[0] / float32(math.Sqrt(float64(k.variance*variance)))
}

type matchLevel struct {
	image   *plane
	squares *plane
	kernel  *kernel
}

type colorKernel struct {
	centered  [3]*plane
	weights   *plane
	weightSum float32
	variance  float32
}

func newColorKernel(template *raster, weights *plane) (*colorKernel, error) {
	out := &colorKernel{weights: weights}
	for channel := range 3 {
		k, err := newKernel(channelPlane(template, channel), weights)
		if err != nil {
			return nil, err
		}
		out.centered[channel] = k.centered
		out.variance += k.variance
		out.weightSum = k.weightSum
	}
	return out, nil
}

func (k *colorKernel) score(target *raster, x, y int) float32 {
	var correlated, spread float32
	for _, sums := range colorWindowSums(target, x, y, k) {
		correlated += sums[0]
		spread += max(sums[2]-sums[1]*sums[1]/k.weightSum, 0)
	}
	if spread <= flat(k.weightSum)*3 || k.variance <= 0 {
		return 0
	}
	return correlated / float32(math.Sqrt(float64(k.variance*spread)))
}

func templateWeights(template *raster) *plane {
	if template.gray() {
		out := newPlane(template.width, template.height)
		for index := range out.values {
			out.values[index] = 1
		}
		return out
	}
	return channelPlane(template, alpha)
}

type match struct {
	frame rect
	score float32
	scale float64
}

type candidate struct {
	score float32
	x, y  int
}

func buildLevels(ctx context.Context, base *plane, template *raster, gray *plane, full *kernel) ([]*matchLevel, error) {
	depth := 0
	for depth < deepestLevel && min(template.width, template.height)>>(depth+1) >= coarsestSide {
		depth++
	}
	levels := []*matchLevel{{image: base, squares: base.squared(), kernel: full}}
	for level := 1; level <= depth; level++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		image := base.resampled(max(base.width>>level, 1), max(base.height>>level, 1))
		width, height := max(template.width>>level, 1), max(template.height>>level, 1)
		if width > image.width || height > image.height {
			break
		}
		k, err := newKernel(gray.resampled(width, height), full.weights.resampled(width, height))
		if err != nil {
			return nil, err
		}
		levels = append(levels, &matchLevel{image: image, squares: image.squared(), kernel: k})
	}
	return levels, nil
}

func byScore[T any](score func(T) float32) func(a, b T) int {
	return func(a, b T) int {
		return cmp.Compare(score(b), score(a))
	}
}

func coarseCandidates(coarse *matchLevel, cutoff float32, wanted int) []candidate {
	k := coarse.kernel
	correlations := correlated(coarse.image, k.centered)
	sums := correlated(coarse.image, k.weights)
	squares := correlated(coarse.squares, k.weights)
	var candidates []candidate
	for y := range correlations.height {
		for x := range correlations.width {
			index := y*correlations.width + x
			variance := squares.values[index] - sums.values[index]*sums.values[index]/k.weightSum
			if variance <= flat(k.weightSum) {
				continue
			}
			if value := correlations.values[index] / float32(math.Sqrt(float64(k.variance*variance))); value >= cutoff {
				candidates = append(candidates, candidate{score: value, x: x, y: y})
			}
		}
	}
	slices.SortStableFunc(candidates, byScore(func(c candidate) float32 { return c.score }))
	var kept []candidate
	for _, found := range candidates {
		if len(kept) >= wanted {
			break
		}
		if !slices.ContainsFunc(kept, func(other candidate) bool {
			return abs(other.x-found.x)*2 < k.weights.width && abs(other.y-found.y)*2 < k.weights.height
		}) {
			kept = append(kept, found)
		}
	}
	return kept
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func refinePosition(levels []*matchLevel, x, y int) (int, int, float32) {
	best := float32(0)
	origins := make([]point, 0, (refineRadius*2+1)*(refineRadius*2+1))
	for level := len(levels) - 2; level >= 0; level-- {
		current := levels[level]
		limitX := current.image.width - current.kernel.weights.width
		limitY := current.image.height - current.kernel.weights.height
		centerX, centerY := x*2, y*2
		origins = origins[:0]
		for dy := -refineRadius; dy <= refineRadius; dy++ {
			for dx := -refineRadius; dx <= refineRadius; dx++ {
				origins = append(origins, point{float64(min(max(centerX+dx, 0), limitX)), float64(min(max(centerY+dy, 0), limitY))})
			}
		}
		best = -2
		for index, sums := range windowSums(current, origins) {
			if value := correlation(sums, current.kernel); value > best {
				best, x, y = value, int(origins[index].x), int(origins[index].y)
			}
		}
	}
	return x, y, best
}

func search(ctx context.Context, base *plane, target, template *raster, threshold float32, limit int, factor float64) ([]match, error) {
	gray := planeOf(template.grayscale())
	weights := templateWeights(template)
	full, err := newKernel(gray, weights)
	if err != nil {
		return nil, err
	}
	if full.variance/full.weightSum <= 1e-6 {
		return nil, failure(nameRange, "the template has no contrast to match")
	}
	levels, err := buildLevels(ctx, base, template, gray, full)
	if err != nil {
		return nil, err
	}
	coarse := levels[len(levels)-1]
	if coarse.image.height-coarse.kernel.weights.height+1 <= 0 || coarse.image.width-coarse.kernel.weights.width+1 <= 0 {
		return nil, nil
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	margin := float32(0.1)
	if len(levels) > 1 {
		margin = 0.25
	}
	kept := coarseCandidates(coarse, max(threshold-margin, 0), min(max(limit*3, 20), 300))
	var colors *colorKernel
	if !target.gray() && !template.gray() {
		if colors, err = newColorKernel(template, weights); err != nil {
			return nil, err
		}
	}
	var matches []match
	for _, found := range kept {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		x, y, best := refinePosition(levels, found.x, found.y)
		if len(levels) == 1 {
			best = correlation(windowSums(levels[0], []point{{float64(x), float64(y)}})[0], levels[0].kernel)
		}
		if colors != nil {
			best = colors.score(target, x, y)
		}
		if best >= threshold {
			matches = append(matches, match{frame: rect{float64(x), float64(y), float64(template.width), float64(template.height)}, score: best, scale: factor})
		}
	}
	return matches, nil
}

func intersectionOverUnion(a, b rect) float64 {
	overlap, overlaps := a.intersection(b)
	if !overlaps {
		return 0
	}
	shared := overlap.width * overlap.height
	return shared / (a.width*a.height + b.width*b.height - shared)
}

func findTemplate(ctx context.Context, target, template *raster, factors []float64, threshold float64, limit int, overlap float64) ([]match, error) {
	base := planeOf(target.grayscale())
	var found []match
	for _, factor := range factors {
		width := int(math.Round(float64(template.width) * factor))
		height := int(math.Round(float64(template.height) * factor))
		if width < 3 || height < 3 || width > target.width || height > target.height {
			continue
		}
		matches, err := search(ctx, base, target, resampled(template, width, height), float32(threshold), limit, factor)
		if err != nil {
			return nil, err
		}
		found = append(found, matches...)
	}
	slices.SortStableFunc(found, byScore(func(m match) float32 { return m.score }))
	var kept []match
	for _, candidate := range found {
		if len(kept) >= limit {
			break
		}
		if !slices.ContainsFunc(kept, func(other match) bool {
			return intersectionOverUnion(other.frame, candidate.frame) > overlap
		}) {
			kept = append(kept, candidate)
		}
	}
	return kept, nil
}

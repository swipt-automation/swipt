//go:build cgo && (darwin || android)

package graphics

const (
	defaultMatchThreshold      = 0.8
	defaultMatchLimit          = 100
	defaultMatchOverlap        = 0.3
	defaultColorTolerance      = 16
	defaultDifferenceThreshold = 24
)

func (a *callArguments) region() *rect {
	if len(a.Region) != 4 {
		return nil
	}
	return &rect{a.Region[0], a.Region[1], a.Region[2], a.Region[3]}
}

func (c *imageCall) analyze() (any, error) {
	switch c.method {
	case "pixel":
		return c.pixel()
	case "stats":
		node, err := c.image(0)
		if err != nil {
			return nil, err
		}
		shown, _, err := node.rasterIn(c.ctx, c.arguments.region())
		if err != nil {
			return nil, err
		}
		return shown.statistics(), nil
	case "find":
		return c.find()
	case "colors":
		return c.colors()
	case "components":
		return c.components()
	case "diff":
		return c.difference()
	}
	return nil, failure(nameType, "graphics: there is no call ", c.method)
}

func (c *imageCall) raster(index int) (*imageNode, *raster, error) {
	node, err := c.image(index)
	if err != nil {
		return nil, nil, err
	}
	shown, err := node.raster(c.ctx)
	return node, shown, err
}

func (c *imageCall) pixel() (any, error) {
	node, err := c.image(0)
	if err != nil {
		return nil, err
	}
	if len(c.arguments.Point) != 2 {
		return nil, failure(nameType, "pixel needs a point")
	}
	x, y := node.pixelPoint(point{c.arguments.Point[0], c.arguments.Point[1]})
	if x < 0 || y < 0 || x >= node.width || y >= node.height {
		return nil, failure(nameRange, "the point lies outside the image")
	}
	shown, err := node.raster(c.ctx)
	if err != nil {
		return nil, err
	}
	return shown.at(x, y), nil
}

type matchResult struct {
	Frame  []float64 `json:"frame"`
	Center []float64 `json:"center"`
	Score  float64   `json:"score"`
	Scale  float64   `json:"scale"`
}

func (c *imageCall) find() (any, error) {
	node, err := c.image(0)
	if err != nil {
		return nil, err
	}
	template, pattern, err := c.raster(1)
	if err != nil {
		return nil, err
	}
	target, origin, err := node.rasterIn(c.ctx, c.arguments.region())
	if err != nil {
		return nil, err
	}
	base := node.scale() / template.scale()
	factors := []float64{base}
	if len(c.arguments.Scales) > 0 {
		factors = make([]float64, len(c.arguments.Scales))
		for index, scale := range c.arguments.Scales {
			factors[index] = scale * base
		}
	}
	threshold, limit, overlap := float64(defaultMatchThreshold), defaultMatchLimit, float64(defaultMatchOverlap)
	if c.arguments.Threshold != nil {
		threshold = *c.arguments.Threshold
	}
	if c.arguments.Limit != nil {
		limit = *c.arguments.Limit
	}
	if c.arguments.Overlap != nil {
		overlap = *c.arguments.Overlap
	}
	matches, err := findTemplate(c.ctx, target, pattern, factors, threshold, limit, overlap)
	if err != nil {
		return nil, err
	}
	results := make([]matchResult, len(matches))
	for index, found := range matches {
		frame := node.spaceRect(rect{found.frame.x + origin.x, found.frame.y + origin.y, found.frame.width, found.frame.height})
		results[index] = matchResult{Frame: frame.json(), Center: frame.center().json(), Score: roundedTo(float64(found.score), 3), Scale: rounded(found.scale / base)}
	}
	return results, nil
}

func (c *imageCall) colors() (any, error) {
	node, shown, err := c.raster(0)
	if err != nil {
		return nil, err
	}
	results := make([]bool, len(c.arguments.Checks))
	for index, check := range c.arguments.Checks {
		if len(check.Point) != 2 || len(check.Color) != 3 {
			continue
		}
		x, y := node.pixelPoint(point{check.Point[0], check.Point[1]})
		if x < 0 || y < 0 || x >= shown.width || y >= shown.height {
			continue
		}
		allowed := defaultColorTolerance
		if check.Tolerance != nil {
			allowed = *check.Tolerance
		}
		pixel := shown.at(x, y)
		results[index] = abs(pixel[0]-check.Color[0]) <= allowed && abs(pixel[1]-check.Color[1]) <= allowed && abs(pixel[2]-check.Color[2]) <= allowed
	}
	return results, nil
}

type componentResult struct {
	Frame    []float64 `json:"frame"`
	Pixels   int       `json:"pixels"`
	Centroid []float64 `json:"centroid"`
}

func (c *imageCall) components() (any, error) {
	node, shown, err := c.raster(0)
	if err != nil {
		return nil, err
	}
	minimum := 1
	if c.arguments.MinimumPixels != nil {
		minimum = *c.arguments.MinimumPixels
	}
	found, err := components(c.ctx, shown.binarized(), minimum)
	if err != nil {
		return nil, err
	}
	results := make([]componentResult, len(found))
	for index, item := range found {
		results[index] = componentResult{Frame: node.spaceRect(item.rect()).json(), Pixels: item.count, Centroid: node.spacePoint(item.centroid()).json()}
	}
	return results, nil
}

type differenceResult struct {
	Ratio  float64     `json:"ratio"`
	Frames [][]float64 `json:"frames"`
}

func (c *imageCall) difference() (any, error) {
	first, a, err := c.raster(0)
	if err != nil {
		return nil, err
	}
	_, other, err := c.raster(1)
	if err != nil {
		return nil, err
	}
	threshold := defaultDifferenceThreshold
	if c.arguments.Threshold != nil {
		threshold = int(*c.arguments.Threshold)
	}
	mask, changed := differenceMask(a, resampled(other, a.width, a.height), threshold)
	regions, err := mergedRegions(c.ctx, mask, max(2, a.width/200))
	if err != nil {
		return nil, err
	}
	frames := make([][]float64, len(regions))
	for index, region := range regions {
		frames[index] = first.spaceRect(region).json()
	}
	return differenceResult{Ratio: roundedTo(float64(changed)/float64(max(a.width*a.height, 1)), 4), Frames: frames}, nil
}

//go:build cgo

package graphics

type quad [4]point

type textRecognition interface {
	box(line, candidate, start, end int) (quad, bool, error)
	release()
}

type shape struct {
	Quad   [][]float64 `json:"quad"`
	Frame  []float64   `json:"frame"`
	Center []float64   `json:"center"`
}

type placement struct {
	transform     affine
	origin        point
	width, height float64
}

func (p placement) shape(corners quad) shape {
	var out shape
	left, top, right, bottom := 0.0, 0.0, 0.0, 0.0
	for index, corner := range corners {
		placed := p.transform.apply(point{p.origin.x + corner.x*p.width, p.origin.y + (1-corner.y)*p.height})
		if index == 0 {
			left, top, right, bottom = placed.x, placed.y, placed.x, placed.y
		}
		left, right = min(left, placed.x), max(right, placed.x)
		top, bottom = min(top, placed.y), max(bottom, placed.y)
		out.Quad = append(out.Quad, placed.json())
	}
	frame := rect{left, top, right - left, bottom - top}
	out.Frame, out.Center = frame.json(), frame.center().json()
	return out
}

type recognizedText struct {
	native    textRecognition
	placement placement
}

func (t *recognizedText) release() {
	t.native.release()
}

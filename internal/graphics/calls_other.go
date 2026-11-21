//go:build cgo && !darwin && !android

package graphics

var analysisNames = map[string]string{
	"pixel":      "pixel",
	"stats":      "stats",
	"find":       "find",
	"colors":     "matchColors",
	"components": "components",
	"diff":       "diff",
}

func (c *imageCall) analyze() (any, error) {
	if name, known := analysisNames[c.method]; known {
		return nil, unsupported(name)
	}
	return nil, failure(nameType, "graphics: there is no call ", c.method)
}

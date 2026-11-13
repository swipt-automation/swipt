package accessibility

import (
	"fmt"
	"math"
	"strconv"

	"github.com/swipt-automation/swipt/internal/coredevice/dtx"
)

type LayoutFrame struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type LayoutPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func decodeLayoutRawElement(value any) []byte {
	token, ok := value.(map[string]any)
	if !ok || token["TokenType"] != "AXElementTokenSimulator" {
		return nil
	}
	data, ok := token["ElementData"].([]byte)
	if !ok || len(data) == 0 || len(data) > 4096 {
		return nil
	}
	return data
}

func decodeLayoutFrame(value any) *LayoutFrame {
	fields, ok := value.(map[string]any)
	if !ok || fields["$class"] != "NSValue" {
		return nil
	}
	s, ok := fields["NS.rectval"].(string)
	if !ok || len(s) > 256 {
		return nil
	}
	var frame LayoutFrame
	if n, err := fmt.Sscanf(s, "{{%f, %f}, {%f, %f}}", &frame.X, &frame.Y, &frame.Width, &frame.Height); err != nil || n != 4 {
		return nil
	}
	if !finiteLayoutNumber(frame.X) || !finiteLayoutNumber(frame.Y) || !finiteLayoutNumber(frame.Width) || !finiteLayoutNumber(frame.Height) || frame.Width < 0 || frame.Height < 0 {
		return nil
	}
	return &frame
}

func finiteLayoutNumber(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func (p LayoutPoint) archiveObject() dtx.Object {
	return dtx.Object{ClassName: "NSValue", Fields: map[string]any{
		"NS.special":  int64(1),
		"NS.pointval": "{" + strconv.FormatFloat(p.X, 'g', -1, 64) + ", " + strconv.FormatFloat(p.Y, 'g', -1, 64) + "}",
	}, PrimitiveFields: []string{"NS.special"}}
}

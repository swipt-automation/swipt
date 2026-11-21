//go:build darwin && cgo

package graphics

/*
#include <stdlib.h>
#include "graphics_darwin.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/log"
)

func warmVision(logger log.ContextLogger) {
	C.swipt_vision_warm(C.uintptr_t(cgo.NewHandle(logger)))
}

//export swiptGraphicsWarmed
func swiptGraphicsWarmed(logger C.uintptr_t, message *C.char) {
	handle := cgo.Handle(logger)
	if message != nil {
		handle.Value().(log.ContextLogger).Error(C.GoString(message))
	}
	handle.Delete()
}

const glyphPixels = 24

type textOptions struct {
	languages      []string
	detectLanguage *bool
	fast           *bool
	words          []string
	correction     *bool
	minimumHeight  float64
}

type textCandidate struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

type textLine struct {
	quad       quad
	candidates []textCandidate
}

type barcode struct {
	quad      quad
	payload   *string
	symbology string
}

type rectangleJSON struct {
	Quad [4][2]float64 `json:"quad"`
}

func (r rectangleJSON) corners() quad {
	var out quad
	for index, corner := range r.Quad {
		out[index] = point{corner[0], corner[1]}
	}
	return out
}

func cStrings(values []string) (**C.char, C.size_t, func()) {
	if values == nil {
		return nil, 0, func() {}
	}
	array := (*[1 << 20]*C.char)(C.malloc(C.size_t(max(len(values), 1)) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))[:len(values):len(values)]
	for index, value := range values {
		array[index] = C.CString(value)
	}
	return (**C.char)(unsafe.Pointer(unsafe.SliceData(array))), C.size_t(len(values)), func() {
		for _, value := range array {
			C.free(unsafe.Pointer(value))
		}
		C.free(unsafe.Pointer(unsafe.SliceData(array)))
	}
}

func tristate(value *bool) C.int32_t {
	switch {
	case value == nil:
		return -1
	case *value:
		return 1
	}
	return 0
}

type nativeText struct {
	pointer unsafe.Pointer
}

func (t nativeText) box(line, candidate, start, end int) (quad, bool, error) {
	var corners [8]C.double
	var found C.bool
	if message := C.swipt_text_box(t.pointer, C.int64_t(line), C.int64_t(candidate), C.int64_t(start), C.int64_t(end), &corners[0], &found); message != nil {
		return quad{}, false, failed(nameGeneric, message)
	}
	var out quad
	for index := range out {
		out[index] = point{float64(corners[index*2]), float64(corners[index*2+1])}
	}
	return out, bool(found), nil
}

func (t nativeText) release() {
	C.swipt_release(t.pointer)
}

func recognizeText(ctx context.Context, r *raster, options textOptions) (nativeText, []textLine, error) {
	languages, languageCount, freeLanguages := cStrings(options.languages)
	defer freeLanguages()
	words, wordCount, freeWords := cStrings(options.words)
	defer freeWords()
	token, done := watchCancel(ctx)
	defer done()
	var text unsafe.Pointer
	var encoded *C.char
	message := C.swipt_text_recognize(bytesPointer(r.pixels), C.int64_t(r.width), C.int64_t(r.height), C.int64_t(r.stride), C.int32_t(r.channels), C.swipt_text_options{
		languages:       languages,
		language_count:  languageCount,
		words:           words,
		word_count:      wordCount,
		detect_language: tristate(options.detectLanguage),
		fast:            tristate(options.fast),
		correction:      tristate(options.correction),
		minimum_height:  C.double(options.minimumHeight),
	}, token, &text, &encoded)
	if message != nil {
		return nativeText{}, nil, interrupted(ctx, failed(nameGeneric, message))
	}
	native := nativeText{text}
	defer C.free(unsafe.Pointer(encoded))
	var lines []struct {
		rectangleJSON
		Candidates []textCandidate `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(C.GoString(encoded)), &lines); err != nil {
		native.release()
		return nativeText{}, nil, err
	}
	out := make([]textLine, len(lines))
	for index, line := range lines {
		out[index] = textLine{quad: line.corners(), candidates: line.Candidates}
	}
	return native, out, nil
}

func detectBarcodes(ctx context.Context, r *raster) ([]barcode, error) {
	token, done := watchCancel(ctx)
	defer done()
	var encoded *C.char
	if message := C.swipt_barcodes_detect(bytesPointer(r.pixels), C.int64_t(r.width), C.int64_t(r.height), C.int64_t(r.stride), C.int32_t(r.channels), token, &encoded); message != nil {
		return nil, interrupted(ctx, failed(nameGeneric, message))
	}
	defer C.free(unsafe.Pointer(encoded))
	var codes []struct {
		rectangleJSON
		Payload   *string `json:"payload"`
		Symbology string  `json:"symbology"`
	}
	if err := json.Unmarshal([]byte(C.GoString(encoded)), &codes); err != nil {
		return nil, err
	}
	out := make([]barcode, len(codes))
	for index, code := range codes {
		out[index] = barcode{quad: code.corners(), payload: code.Payload, symbology: code.Symbology}
	}
	return out, nil
}

type nativePrint struct {
	pointer unsafe.Pointer
}

func (p nativePrint) distance(other nativePrint) (float64, error) {
	var distance C.double
	if message := C.swipt_feature_distance(p.pointer, other.pointer, &distance); message != nil {
		return 0, failed(nameGeneric, message)
	}
	return float64(distance), nil
}

func (p nativePrint) release() {
	C.swipt_release(p.pointer)
}

func printOf(ctx context.Context, r *raster) (nativePrint, error) {
	token, done := watchCancel(ctx)
	defer done()
	var print unsafe.Pointer
	if message := C.swipt_feature_print(bytesPointer(r.pixels), C.int64_t(r.width), C.int64_t(r.height), C.int64_t(r.stride), C.int32_t(r.channels), token, &print); message != nil {
		return nativePrint{}, interrupted(ctx, failed(nameGeneric, message))
	}
	return nativePrint{print}, nil
}

func (c *imageCall) vision() (any, error) {
	switch c.method {
	case "text":
		return c.text()
	case "barcodes":
		return c.barcodes()
	case "distance":
		return c.distance()
	}
	return c.nearest()
}

func (c *imageCall) featurePrint(index int) (nativePrint, error) {
	_, shown, err := c.raster(index)
	if err != nil {
		return nativePrint{}, err
	}
	return printOf(c.ctx, shown)
}

func (c *imageCall) distance() (any, error) {
	reference, err := c.featurePrint(0)
	if err != nil {
		return nil, err
	}
	defer reference.release()
	other, err := c.featurePrint(1)
	if err != nil {
		return nil, err
	}
	defer other.release()
	distance, err := reference.distance(other)
	if err != nil {
		return nil, err
	}
	return roundedTo(distance, 3), nil
}

func (c *imageCall) nearest() (any, error) {
	if len(c.objects) < 2 {
		return nil, failure(nameType, "nearest needs candidates")
	}
	reference, err := c.featurePrint(0)
	if err != nil {
		return nil, err
	}
	defer reference.release()
	best, bestDistance := -1, 0.0
	for index := 1; index < len(c.objects); index++ {
		candidate, err := c.featurePrint(index)
		if err != nil {
			return nil, err
		}
		distance, err := reference.distance(candidate)
		candidate.release()
		if err != nil {
			return nil, err
		}
		if best < 0 || distance < bestDistance {
			best, bestDistance = index-1, distance
		}
	}
	return map[string]float64{"index": float64(best), "distance": roundedTo(bestDistance, 3)}, nil
}

type textLineResult struct {
	shape
	Text       string          `json:"text"`
	Confidence float64         `json:"confidence"`
	Candidates []textCandidate `json:"candidates"`
}

func (c *imageCall) text() (any, error) {
	node, err := c.image(0)
	if err != nil {
		return nil, err
	}
	shown, origin, err := node.rasterIn(c.ctx, c.arguments.region())
	if err != nil {
		return nil, err
	}
	options := textOptions{languages: c.arguments.Languages, detectLanguage: c.arguments.DetectLanguage, words: c.arguments.Words, correction: c.arguments.Correction}
	if c.arguments.Level != "" {
		fast := c.arguments.Level == "fast"
		options.fast = &fast
	}
	sampled := shown
	if height := c.arguments.MinimumHeight; height != nil {
		options.minimumHeight = min(max(*height*node.scale()/float64(shown.height), 0), 1)
		if density := glyphPixels / *height; density < node.scale() {
			sampled = resampled(shown, max(int(float64(shown.width)*density/node.scale()), 1), max(int(float64(shown.height)*density/node.scale()), 1))
		}
	}
	native, lines, err := recognizeText(c.ctx, sampled, options)
	if err != nil {
		return nil, err
	}
	text := &recognizedText{native: native, placement: placement{transform: node.transform, origin: origin, width: float64(shown.width), height: float64(shown.height)}}
	results := make([]textLineResult, len(lines))
	for index, line := range lines {
		for rank := range line.candidates {
			line.candidates[rank].Confidence = rounded(line.candidates[rank].Confidence)
		}
		results[index] = textLineResult{shape: text.placement.shape(line.quad), Text: line.candidates[0].Text, Confidence: line.candidates[0].Confidence, Candidates: line.candidates}
	}
	return map[string]any{"lines": results, "text": c.registrar.register(text)}, nil
}

type barcodeResult struct {
	shape
	Payload   *string `json:"payload"`
	Symbology string  `json:"symbology"`
}

func (c *imageCall) barcodes() (any, error) {
	node, shown, err := c.raster(0)
	if err != nil {
		return nil, err
	}
	found, err := detectBarcodes(c.ctx, shown)
	if err != nil {
		return nil, err
	}
	place := placement{transform: node.transform, width: float64(shown.width), height: float64(shown.height)}
	results := make([]barcodeResult, len(found))
	for index, code := range found {
		results[index] = barcodeResult{shape: place.shape(code.quad), Payload: code.payload, Symbology: code.symbology}
	}
	return results, nil
}

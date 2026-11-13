package media

import (
	"bytes"
	"math/bits"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	hevcNALVPS         = 32
	hevcNALSPS         = 33
	hevcNALPPS         = 34
	hevcNALAggregation = 48
	hevcNALFragment    = 49
)

func hevcNALType(nal []byte) int {
	return int(nal[0]>>1) & 0x3f
}

type HEVCSequence struct {
	SubLayers         int
	TemporalNesting   bool
	ChromaFormat      int
	CodedWidth        int
	CodedHeight       int
	LumaDepthMinus8   int
	ChromaDepthMinus8 int

	nal            []byte
	rbsp           []byte
	subWidth       int
	subHeight      int
	window         [4]int
	windowPosition int
	restPosition   int
}

func ParseHEVCSequence(nal []byte) (*HEVCSequence, error) {
	rbsp := unescapeRBSP(nal)
	if len(rbsp) < 15 {
		return nil, E.New("media: the sequence parameter set is short")
	}
	r := &bitReader{data: rbsp, position: 16}
	s := &HEVCSequence{nal: nal, rbsp: rbsp, subWidth: 1, subHeight: 1}
	r.bits(4)
	maxSubLayersMinus1 := int(r.bits(3))
	s.SubLayers = maxSubLayersMinus1 + 1
	s.TemporalNesting = r.flag()
	skipHEVCProfileTierLevel(r, maxSubLayersMinus1)
	r.unsignedGolomb()
	s.ChromaFormat = int(r.unsignedGolomb())
	separatePlanes := s.ChromaFormat == 3 && r.flag()
	s.CodedWidth = int(r.unsignedGolomb())
	s.CodedHeight = int(r.unsignedGolomb())
	s.windowPosition = r.position
	if r.flag() {
		for index := range s.window {
			s.window[index] = int(r.unsignedGolomb())
		}
	}
	s.restPosition = r.position
	s.LumaDepthMinus8 = int(r.unsignedGolomb())
	s.ChromaDepthMinus8 = int(r.unsignedGolomb())
	if r.overrun || s.ChromaFormat > 3 || s.LumaDepthMinus8 > 7 || s.ChromaDepthMinus8 > 7 {
		return nil, E.New("media: read the sequence parameter set")
	}
	if !separatePlanes && (s.ChromaFormat == 1 || s.ChromaFormat == 2) {
		s.subWidth = 2
	}
	if !separatePlanes && s.ChromaFormat == 1 {
		s.subHeight = 2
	}
	if width, height := s.Size(); width <= 0 || height <= 0 {
		return nil, E.New("media: the sequence parameter set has no usable size")
	}
	return s, nil
}

func skipHEVCProfileTierLevel(r *bitReader, maxSubLayersMinus1 int) {
	r.bits(8)
	r.bits(32)
	r.bits(48)
	r.bits(8)
	profilePresent := make([]bool, maxSubLayersMinus1)
	levelPresent := make([]bool, maxSubLayersMinus1)
	for i := range maxSubLayersMinus1 {
		profilePresent[i] = r.flag()
		levelPresent[i] = r.flag()
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			r.bits(2)
		}
	}
	for i := range maxSubLayersMinus1 {
		if profilePresent[i] {
			r.bits(88)
		}
		if levelPresent[i] {
			r.bits(8)
		}
	}
}

func (s *HEVCSequence) NAL() []byte {
	return s.nal
}

func (s *HEVCSequence) ProfileTierLevel() []byte {
	return s.rbsp[3:15]
}

func (s *HEVCSequence) Size() (width, height int) {
	return s.CodedWidth - s.subWidth*(s.window[0]+s.window[1]), s.CodedHeight - s.subHeight*(s.window[2]+s.window[3])
}

func (s *HEVCSequence) CroppedTo(width, height int) *HEVCSequence {
	current, currentHeight := s.Size()
	extraWidth := max(current-width, 0) / s.subWidth
	extraHeight := max(currentHeight-height, 0) / s.subHeight
	last := bytes.LastIndexFunc(s.rbsp, func(value rune) bool { return value != 0 })
	if width <= 0 || height <= 0 || extraWidth+extraHeight == 0 || last*8 < s.restPosition {
		return s
	}
	cropped := *s
	cropped.window[1] += extraWidth
	cropped.window[3] += extraHeight
	r := &bitReader{data: s.rbsp}
	var w bitWriter
	w.copy(r, s.windowPosition)
	w.bits(1, 1)
	for _, value := range cropped.window {
		w.unsignedGolomb(uint(value))
	}
	cropped.restPosition = w.position
	r.position = s.restPosition
	w.copy(r, last*8+8-bits.TrailingZeros8(s.rbsp[last]))
	cropped.rbsp = w.data
	cropped.nal = escapeRBSP(w.data)
	return &cropped
}

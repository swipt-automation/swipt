//go:build cgo

package graphics

import (
	"encoding/binary"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/rfb/media"
)

type hevcFormat struct {
	vps           []byte
	source        []byte
	sps           []byte
	pps           []byte
	width         int
	height        int
	configuration []byte
}

func newHEVCFormat(vps, sps, pps []byte, screen device.ScreenInfo) (*hevcFormat, error) {
	sequence, err := media.ParseHEVCSequence(sps)
	if err != nil {
		return nil, err
	}
	if screen.Width > 0 && screen.Height > 0 {
		sequence = sequence.CroppedTo(screen.Width, screen.Height)
	}
	format := &hevcFormat{vps: vps, source: sps, sps: sequence.NAL(), pps: pps}
	format.width, format.height = sequence.Size()
	if format.width > 0xffff || format.height > 0xffff {
		return nil, E.New("graphics: the picture is ", format.width, "x", format.height)
	}
	configuration := append([]byte{1}, sequence.ProfileTierLevel()...)
	nesting := byte(0)
	if sequence.TemporalNesting {
		nesting = 1
	}
	configuration = append(configuration, 0xf0, 0x00, 0xfc, 0xfc|byte(sequence.ChromaFormat), 0xf8|byte(sequence.LumaDepthMinus8), 0xf8|byte(sequence.ChromaDepthMinus8), 0, 0,
		byte(min(sequence.SubLayers, 7))<<3|nesting<<2|3, 3)
	for _, nal := range [][]byte{format.vps, format.sps, format.pps} {
		configuration = append(configuration, 0x80|nal[0]>>1&0x3f)
		configuration = binary.BigEndian.AppendUint16(configuration, 1)
		configuration = binary.BigEndian.AppendUint16(configuration, uint16(len(nal)))
		configuration = append(configuration, nal...)
	}
	format.configuration = configuration
	return format, nil
}

package media

import (
	"encoding/binary"

	E "github.com/sagernet/sing/common/exceptions"
)

type rtpPacket struct {
	marker           bool
	payloadType      uint8
	sequence         uint16
	timestamp        uint32
	ssrc             uint32
	extensionProfile uint16
	payloadOffset    int
	payloadEnd       int
}

func isRTCP(datagram []byte) bool {
	return len(datagram) >= 2 && datagram[1] >= 192 && datagram[1] <= 223
}

func parseRTPHeader(data []byte) (rtpPacket, error) {
	if len(data) < 12 {
		return rtpPacket{}, E.New("media: RTP packet of ", len(data), " bytes is shorter than 12")
	}
	if data[0]>>6 != 2 {
		return rtpPacket{}, E.New("media: RTP version ", data[0]>>6, ", expected 2")
	}
	if isRTCP(data) {
		return rtpPacket{}, E.New("media: RTCP packet type ", data[1], " where RTP was expected")
	}
	packet := rtpPacket{
		marker:      data[1]&0x80 != 0,
		payloadType: data[1] & 0x7f,
		sequence:    binary.BigEndian.Uint16(data[2:]),
		timestamp:   binary.BigEndian.Uint32(data[4:]),
		ssrc:        binary.BigEndian.Uint32(data[8:]),
	}
	offset := 12 + 4*int(data[0]&0x0f)
	if data[0]&0x10 != 0 {
		if len(data) < offset+4 {
			return rtpPacket{}, E.New("media: RTP header extension at offset ", offset, " exceeds the ", len(data), "-byte packet")
		}
		packet.extensionProfile = binary.BigEndian.Uint16(data[offset:])
		offset += 4 + 4*int(binary.BigEndian.Uint16(data[offset+2:]))
	}
	end := len(data)
	if data[0]&0x20 != 0 {
		end -= int(data[end-1])
	}
	if offset > end {
		return rtpPacket{}, E.New("media: RTP header of ", offset, " bytes and its padding exceed the ", len(data), "-byte packet")
	}
	packet.payloadOffset = offset
	packet.payloadEnd = end
	return packet, nil
}

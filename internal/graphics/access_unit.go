//go:build cgo

package graphics

import (
	"encoding/binary"

	"github.com/swipt-automation/swipt/internal/rfb/media"
)

const (
	packetHeaderSize = 13
	nalVPS           = 32
	nalSPS           = 33
	nalPPS           = 34
	nalAggregated    = 48
	nalFragmented    = 49
)

type accessUnit struct {
	nals         [][]byte
	rtpTimestamp uint32
	arrival      float64
	profile      uint16
	damaged      bool
}

func nalType(nal []byte) int {
	return int(nal[0]>>1) & 0x3f
}

func (u *accessUnit) keyframe() bool {
	for _, nal := range u.nals {
		if kind := nalType(nal); kind >= 16 && kind <= 23 {
			return true
		}
	}
	return false
}

func (u *accessUnit) nal(kind int) []byte {
	for _, nal := range u.nals {
		if nalType(nal) == kind {
			return nal
		}
	}
	return nil
}

func (u *accessUnit) sample() []byte {
	var sample []byte
	for _, nal := range u.nals {
		if kind := nalType(nal); kind >= nalVPS && kind <= nalPPS {
			continue
		}
		sample = binary.BigEndian.AppendUint32(sample, uint32(len(nal)))
		sample = append(sample, nal...)
	}
	return sample
}

func (u *accessUnit) turns() int {
	return media.ExtensionTurns(u.profile)
}

type depacketizer struct {
	current      *accessUnit
	fragment     []byte
	hasSequence  bool
	lastSequence uint16
}

func (d *depacketizer) depacketize(batch []byte) []*accessUnit {
	var units []*accessUnit
	for offset := 0; offset+packetHeaderSize <= len(batch); {
		arrival := binary.BigEndian.Uint64(batch[offset:])
		rtcp := batch[offset+8] != 0
		length := int(binary.BigEndian.Uint32(batch[offset+9:]))
		offset += packetHeaderSize
		if offset+length > len(batch) {
			break
		}
		if !rtcp {
			units = d.push(batch[offset:offset+length], float64(arrival)/1e6, units)
		}
		offset += length
	}
	return units
}

func (d *depacketizer) addAggregated(payload []byte, unit *accessUnit) {
	for at := 2; at+2 <= len(payload); {
		size := int(binary.BigEndian.Uint16(payload[at:]))
		at += 2
		if size <= 0 || at+size > len(payload) {
			unit.damaged = true
			return
		}
		unit.nals = append(unit.nals, append([]byte(nil), payload[at:at+size]...))
		at += size
	}
}

func (d *depacketizer) addFragment(payload []byte, unit *accessUnit) {
	if len(payload) <= 3 {
		unit.damaged = true
		return
	}
	header := payload[2]
	switch {
	case header&0x80 != 0:
		if d.fragment != nil {
			unit.damaged = true
		}
		d.fragment = append([]byte{payload[0]&0x81 | (header&0x3f)<<1, payload[1]}, payload[3:]...)
	case d.fragment != nil:
		d.fragment = append(d.fragment, payload[3:]...)
	default:
		unit.damaged = true
	}
	if header&0x40 != 0 && d.fragment != nil {
		unit.nals = append(unit.nals, d.fragment)
		d.fragment = nil
	}
}

func (d *depacketizer) push(packet []byte, arrival float64, done []*accessUnit) []*accessUnit {
	if len(packet) < 12 || packet[0]>>6 != 2 {
		return done
	}
	marker := packet[1]&0x80 != 0
	sequence := binary.BigEndian.Uint16(packet[2:])
	timestamp := binary.BigEndian.Uint32(packet[4:])
	offset := 12 + 4*int(packet[0]&0x0f)
	var profile uint16
	if packet[0]&0x10 != 0 {
		if len(packet) < offset+4 {
			return done
		}
		profile = binary.BigEndian.Uint16(packet[offset:])
		offset += 4 + 4*int(binary.BigEndian.Uint16(packet[offset+2:]))
	}
	end := len(packet)
	if packet[0]&0x20 != 0 {
		end -= int(packet[end-1])
	}
	if offset+2 > end {
		return done
	}
	gap := d.hasSequence && sequence != d.lastSequence+1
	d.hasSequence = true
	d.lastSequence = sequence
	if d.current != nil && d.current.rtpTimestamp != timestamp {
		if d.fragment != nil {
			d.current.damaged = true
			d.fragment = nil
		}
		done = append(done, d.current)
		d.current = nil
	}
	unit := d.current
	if unit == nil {
		unit = &accessUnit{rtpTimestamp: timestamp, arrival: arrival, profile: profile}
	}
	if gap {
		unit.damaged = true
		d.fragment = nil
	}
	payload := packet[offset:end]
	switch nalType(payload) {
	case nalAggregated:
		d.addAggregated(payload, unit)
	case nalFragmented:
		d.addFragment(payload, unit)
	default:
		unit.nals = append(unit.nals, append([]byte(nil), payload...))
	}
	if marker {
		done = append(done, unit)
		d.current = nil
		d.fragment = nil
	} else {
		d.current = unit
	}
	return done
}

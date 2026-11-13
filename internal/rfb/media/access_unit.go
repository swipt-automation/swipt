package media

import (
	"encoding/binary"
	"time"
)

const VideoClockRate = 24000

type AccessUnit struct {
	NALs    [][]byte
	Time    int64
	Arrival time.Time
	Turns   int
	Damaged bool
}

func (u *AccessUnit) Keyframe() bool {
	for _, nal := range u.NALs {
		if kind := hevcNALType(nal); kind >= 16 && kind <= 23 {
			return true
		}
	}
	return false
}

func (u *AccessUnit) nal(kind int) []byte {
	for _, nal := range u.NALs {
		if hevcNALType(nal) == kind {
			return nal
		}
	}
	return nil
}

func (u *AccessUnit) ParameterSets() (vps, sps, pps []byte) {
	return u.nal(hevcNALVPS), u.nal(hevcNALSPS), u.nal(hevcNALPPS)
}

func (u *AccessUnit) Sample() []byte {
	var sample []byte
	for _, nal := range u.NALs {
		if kind := hevcNALType(nal); kind >= hevcNALVPS && kind <= hevcNALPPS {
			continue
		}
		sample = binary.BigEndian.AppendUint32(sample, uint32(len(nal)))
		sample = append(sample, nal...)
	}
	return sample
}

type Depacketizer struct {
	current   *AccessUnit
	fragment  []byte
	started   bool
	sequence  uint16
	timestamp uint32
	wraps     int64
}

func (d *Depacketizer) Push(packet []byte, arrival time.Time, done []*AccessUnit) []*AccessUnit {
	header, err := parseRTPHeader(packet)
	if err != nil || header.payloadOffset+2 > header.payloadEnd {
		return done
	}
	gap := d.started && header.sequence != d.sequence+1
	if d.started && header.timestamp < d.timestamp && d.timestamp-header.timestamp > 1<<31 {
		d.wraps++
	}
	d.started = true
	d.sequence = header.sequence
	d.timestamp = header.timestamp
	time := d.wraps<<32 + int64(header.timestamp)
	if d.current != nil && d.current.Time != time {
		if d.fragment != nil {
			d.current.Damaged = true
			d.fragment = nil
		}
		done = append(done, d.current)
		d.current = nil
	}
	unit := d.current
	if unit == nil {
		unit = &AccessUnit{Time: time, Arrival: arrival, Turns: extensionTurns(header.extensionProfile)}
	}
	if gap {
		unit.Damaged = true
		d.fragment = nil
	}
	payload := packet[header.payloadOffset:header.payloadEnd]
	switch hevcNALType(payload) {
	case hevcNALAggregation:
		d.addAggregated(payload, unit)
	case hevcNALFragment:
		d.addFragment(payload, unit)
	default:
		unit.NALs = append(unit.NALs, append([]byte(nil), payload...))
	}
	if header.marker {
		done = append(done, unit)
		d.current = nil
		d.fragment = nil
	} else {
		d.current = unit
	}
	return done
}

func (d *Depacketizer) addAggregated(payload []byte, unit *AccessUnit) {
	for at := 2; at+2 <= len(payload); {
		size := int(binary.BigEndian.Uint16(payload[at:]))
		at += 2
		if size <= 0 || at+size > len(payload) {
			unit.Damaged = true
			return
		}
		unit.NALs = append(unit.NALs, append([]byte(nil), payload[at:at+size]...))
		at += size
	}
}

func (d *Depacketizer) addFragment(payload []byte, unit *AccessUnit) {
	if len(payload) <= 3 {
		unit.Damaged = true
		return
	}
	header := payload[2]
	switch {
	case header&0x80 != 0:
		if d.fragment != nil {
			unit.Damaged = true
		}
		d.fragment = append([]byte{payload[0]&0x81 | (header&0x3f)<<1, payload[1]}, payload[3:]...)
	case d.fragment != nil:
		d.fragment = append(d.fragment, payload[3:]...)
	default:
		unit.Damaged = true
	}
	if header&0x40 != 0 && d.fragment != nil {
		unit.NALs = append(unit.NALs, d.fragment)
		d.fragment = nil
	}
}

func PacketSequence(packet []byte) *HEVCSequence {
	header, err := parseRTPHeader(packet)
	if err != nil || header.payloadOffset+3 > header.payloadEnd {
		return nil
	}
	payload := packet[header.payloadOffset:header.payloadEnd]
	var nal []byte
	switch hevcNALType(payload) {
	case hevcNALSPS:
		nal = payload
	case hevcNALAggregation:
		for at := 2; at+2 <= len(payload); {
			size := int(binary.BigEndian.Uint16(payload[at:]))
			if size < 3 || at+2+size > len(payload) {
				return nil
			}
			if unit := payload[at+2 : at+2+size]; hevcNALType(unit) == hevcNALSPS {
				nal = unit
				break
			}
			at += 2 + size
		}
	}
	if nal == nil {
		return nil
	}
	sequence, err := ParseHEVCSequence(nal)
	if err != nil {
		return nil
	}
	return sequence
}

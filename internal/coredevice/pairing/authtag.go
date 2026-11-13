package pairing

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"math/bits"
	"slices"
	"strings"
)

// idevice computes the Bonjour authTag as SipHash-2-4 of the identifier keyed
// by the 16-byte IRK, and takes bytes 5 through 0 of the little-endian result
// (idevice 3854a5d, idevice/src/remote_pairing/peer_device.rs:33-45, 129-140).
func AuthTag(irk []byte, identifier string) []byte {
	sum := sipHash24(binary.LittleEndian.Uint64(irk[0:8]), binary.LittleEndian.Uint64(irk[8:16]), []byte(identifier))
	tag := binary.LittleEndian.AppendUint64(nil, sum)[:6]
	slices.Reverse(tag)
	return tag
}

func (r *Record) MatchesService(identifier string, authTag string) bool {
	if len(r.AltIRK) != 16 || identifier == "" {
		return false
	}
	tag, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authTag))
	return err == nil && len(tag) == 6 && bytes.Equal(tag, AuthTag(r.AltIRK, identifier))
}

func sipHash24(k0, k1 uint64, message []byte) uint64 {
	v0 := k0 ^ 0x736f6d6570736575
	v1 := k1 ^ 0x646f72616e646f6d
	v2 := k0 ^ 0x6c7967656e657261
	v3 := k1 ^ 0x7465646279746573
	round := func() {
		v0 += v1
		v1 = bits.RotateLeft64(v1, 13)
		v1 ^= v0
		v0 = bits.RotateLeft64(v0, 32)
		v2 += v3
		v3 = bits.RotateLeft64(v3, 16)
		v3 ^= v2
		v0 += v3
		v3 = bits.RotateLeft64(v3, 21)
		v3 ^= v0
		v2 += v1
		v1 = bits.RotateLeft64(v1, 17)
		v1 ^= v2
		v2 = bits.RotateLeft64(v2, 32)
	}
	length := len(message)
	for len(message) >= 8 {
		m := binary.LittleEndian.Uint64(message)
		v3 ^= m
		round()
		round()
		v0 ^= m
		message = message[8:]
	}
	last := uint64(length) << 56
	for i, b := range message {
		last |= uint64(b) << (8 * i)
	}
	v3 ^= last
	round()
	round()
	v0 ^= last
	v2 ^= 0xff
	round()
	round()
	round()
	round()
	return v0 ^ v1 ^ v2 ^ v3
}

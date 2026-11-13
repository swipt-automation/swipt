package media

// SSSession handles a remote video attributes change, including its
// orientation, only when the address isAppleIDAddress and the connection
// options have connectingToIOSdevice; otherwise it returns at once
// (ScreenSharing 776.27, macOS 27.0.1 26A434, UUID
// EC0B2C20-F816-3FCE-822D-F9403C09EB50,
// -[SSSession remoteVideoClient:remoteVideoAttributesDidChange:] 0x1cc0de894..0x1cc0de8b0;
// ScreenSharing 786.3, macOS 27.2 26B5101f, UUID
// 21DF7A41-0D38-3DF3-B87A-8A6249686735,
// -[SSSession remoteVideoClient:remoteVideoAttributesDidChange:] 0x22e521914..0x22e521930).
const (
	appleExtensionProfile = 0x9000
	appleExtensionMask    = 0xf000
	orientationShift      = 8
	orientationMask       = 0x3 << orientationShift
)

var orientationTurns = [4]int{
	0, // portrait
	2, // portrait upside down
	3, // landscape left
	1, // landscape right
}

var turnsOrientation = [4]uint16{0, 3, 1, 2}

const macTurnsOffset = 1

func PacketOrientation(packet []byte) (uint16, bool) {
	if len(packet) < 12 || packet[0]&0x10 == 0 {
		return 0, false
	}
	offset := 12 + 4*int(packet[0]&0x0f)
	if len(packet) < offset+2 {
		return 0, false
	}
	profile := uint16(packet[offset])<<8 | uint16(packet[offset+1])
	if profile&appleExtensionMask != appleExtensionProfile {
		return 0, false
	}
	return (profile & orientationMask) >> orientationShift, true
}

// ExtensionTurns returns the clockwise quarter turns of the screen that the
// profile of an RTP header extension from the device carries.
func ExtensionTurns(profile uint16) int {
	if profile&appleExtensionMask != appleExtensionProfile {
		return 0
	}
	return orientationTurns[(profile&orientationMask)>>orientationShift]
}

func macExtensionProfile(profile uint16) uint16 {
	turns := orientationTurns[(profile&orientationMask)>>orientationShift]
	return profile&^orientationMask | turnsOrientation[(turns+macTurnsOffset)&3]<<orientationShift
}

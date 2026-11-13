package rfb

type Status uint16

// Apple's viewer handles status command 2 as "server pasteboard updated"
// (ScreenSharing 776.27, macOS 27.0.1 26A434, UUID EC0B2C20-F816-3FCE-822D-F9403C09EB50,
// _HandleServerMessage 0x1cc062684..0x1cc0626b0, table 0x1cc06455c, 0x1cc063510..0x1cc063540).
const (
	StatusPasteboardChanged Status = 2
)

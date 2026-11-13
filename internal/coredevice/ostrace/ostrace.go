package ostrace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/rsd"

	"howett.net/plist"
)

// pymobiledevice3 starts the activity stream with StartActivity on this shim;
// the reply is a little-endian length of the length, the little-endian
// length, and a plist, and each entry follows as 0x02, a little-endian
// length, and a record of a 129-byte header, the NUL-terminated filename, the
// image name and the message, both sized in the header with their NUL
// (pymobiledevice3 eed1fde, pymobiledevice3/services/os_trace.py:131-203,
// 224, 337-380).
const (
	serviceName  = "com.apple.os_trace_relay.shim.remote"
	streamFlags  = 0x4
	headerLength = 129
	maxRecord    = 1 << 20
)

// Continuous is the mach continuous time the entry was logged at; the time at
// 55 and 63 is when diagnosticd framed the record (24C5037i diagnosticd
// 0x100005454, 0x100005540, 0x100005568), and Continuous matched the activity
// trace tap's time for the same entry (iOS 27.3 24C5037i).
type Entry struct {
	PID        uint64
	Continuous uint64
	Message    string
}

type Stream struct {
	conn   *rsd.PlistConn
	reader *bufio.Reader
}

func Dial(ctx context.Context, device *coredevice.Device, pid uint64) (*Stream, error) {
	conn, err := device.DialShim(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "ostrace: connect")
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	stream := &Stream{conn: conn, reader: bufio.NewReader(conn)}
	err = conn.Send(map[string]any{"Request": "StartActivity", "MessageFilter": int64(0xFFFF), "Pid": int64(pid), "StreamFlags": int64(streamFlags)})
	if err == nil {
		err = stream.started()
	}
	if err != nil {
		conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, E.Cause(err, "ostrace: start the stream of process ", pid)
	}
	return stream, nil
}

func (s *Stream) started() error {
	var size uint32
	if err := binary.Read(s.reader, binary.LittleEndian, &size); err != nil {
		return err
	}
	if size == 0 || size > 8 {
		return E.New("unexpected length size ", size)
	}
	encoded := make([]byte, 8)
	if _, err := io.ReadFull(s.reader, encoded[:size]); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint64(encoded)
	if length > maxRecord {
		return E.New("reply of ", length, " bytes is too large")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(s.reader, data); err != nil {
		return err
	}
	var reply map[string]any
	if _, err := plist.Unmarshal(data, &reply); err != nil {
		return E.Cause(err, "decode the reply")
	}
	if status, _ := reply["Status"].(string); status != "RequestSuccessful" {
		return E.New("refused: ", fmt.Sprint(reply))
	}
	return nil
}

func (s *Stream) Next() (Entry, error) {
	marker, err := s.reader.ReadByte()
	if err != nil {
		return Entry{}, err
	}
	if marker != 0x02 {
		return Entry{}, E.New("ostrace: unexpected marker ", marker)
	}
	var length uint32
	if err = binary.Read(s.reader, binary.LittleEndian, &length); err != nil {
		return Entry{}, err
	}
	if length > maxRecord {
		return Entry{}, E.New("ostrace: record of ", length, " bytes is too large")
	}
	record := make([]byte, length)
	if _, err = io.ReadFull(s.reader, record); err != nil {
		return Entry{}, err
	}
	return parse(record)
}

func parse(record []byte) (Entry, error) {
	if len(record) < headerLength {
		return Entry{}, E.New("ostrace: record of ", len(record), " bytes is shorter than its header")
	}
	entry := Entry{
		PID:        uint64(binary.LittleEndian.Uint32(record[9:])),
		Continuous: binary.LittleEndian.Uint64(record[75:]),
	}
	imageLength := int(binary.LittleEndian.Uint16(record[107:]))
	messageLength := int(binary.LittleEndian.Uint16(record[109:]))
	filenameEnd := bytes.IndexByte(record[headerLength:], 0)
	if filenameEnd < 0 {
		return Entry{}, E.New("ostrace: record has no filename")
	}
	offset := headerLength + filenameEnd + 1 + imageLength
	if messageLength == 0 || offset+messageLength > len(record) {
		return Entry{}, E.New("ostrace: record has no message")
	}
	entry.Message = string(record[offset : offset+messageLength-1])
	return entry, nil
}

func (s *Stream) Close() error {
	return s.conn.Close()
}

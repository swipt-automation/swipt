package debugserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
)

// pymobiledevice3 reaches debugserver through this service without an RSD
// checkin (pymobiledevice3 11.50.3, pymobiledevice3/cli/developer/debugserver.py:31,
// pymobiledevice3/remote/remote_service_discovery.py:370-381).
const serviceName = "com.apple.internal.dt.remote.debugproxy"

const exchangeTimeout = 3 * time.Second

type Conn struct {
	conn   net.Conn
	reader *bufio.Reader
}

func Dial(ctx context.Context, device *coredevice.Device) (*Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, exchangeTimeout)
	defer cancel()
	conn, err := device.DialService(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "debugserver: connect")
	}
	c := &Conn{conn: conn, reader: bufio.NewReader(conn)}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	err = c.Commands("QStartNoAckMode")
	if err == nil {
		_, err = conn.Write([]byte{'+'})
	}
	if err == nil {
		err = c.Commands("QSetDetachOnError:1", "QThreadSuffixSupported")
	}
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
		return nil, E.Errors(ctx.Err(), err)
	}
	return c, nil
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

func frame(packet string) []byte {
	var checksum byte
	for i := 0; i < len(packet); i++ {
		checksum += packet[i]
	}
	return fmt.Appendf(nil, "$%s#%02x", packet, checksum)
}

func (c *Conn) receive() (string, error) {
	for {
		if _, err := c.reader.ReadBytes('$'); err != nil {
			return "", err
		}
		raw, err := c.reader.ReadBytes('#')
		if err != nil {
			return "", err
		}
		if _, err = c.reader.Discard(2); err != nil {
			return "", err
		}
		packet := decode(raw[:len(raw)-1])
		if strings.HasPrefix(packet, "O") && packet != "OK" {
			continue
		}
		return packet, nil
	}
}

func decode(raw []byte) string {
	var out bytes.Buffer
	for i := 0; i < len(raw); i++ {
		switch {
		case raw[i] == '}' && i+1 < len(raw):
			i++
			out.WriteByte(raw[i] ^ 0x20)
		case raw[i] == '*' && i+1 < len(raw) && out.Len() > 0:
			i++
			out.Write(bytes.Repeat(out.Bytes()[out.Len()-1:], int(raw[i])-29))
		default:
			out.WriteByte(raw[i])
		}
	}
	return out.String()
}

func (c *Conn) Query(packet string) (string, error) {
	_ = c.conn.SetDeadline(time.Now().Add(exchangeTimeout))
	if _, err := c.conn.Write(frame(packet)); err != nil {
		return "", err
	}
	return c.receive()
}

type ReplyError struct {
	Packet string
	Reply  string
}

func (e *ReplyError) Error() string {
	return "debugserver: " + e.Packet + ": " + e.Reply
}

func (c *Conn) Commands(packets ...string) error {
	var batch []byte
	for _, packet := range packets {
		batch = append(batch, frame(packet)...)
	}
	_ = c.conn.SetDeadline(time.Now().Add(exchangeTimeout))
	written := make(chan error, 1)
	go func() {
		_, err := c.conn.Write(batch)
		written <- err
	}()
	var failure error
	for _, packet := range packets {
		reply, err := c.receive()
		if err != nil {
			return E.Errors(<-written, err)
		}
		if reply != "OK" && failure == nil {
			failure = &ReplyError{Packet: packet, Reply: reply}
		}
	}
	return E.Errors(<-written, failure)
}

type Stop struct {
	Signal int
	Thread string
	Values map[string]string
}

type ExitedError struct {
	Reply string
}

func (e *ExitedError) Error() string {
	return "debugserver: the process exited: " + e.Reply
}

func (c *Conn) Resume(packet string) (Stop, error) {
	reply, err := c.Query(packet)
	if err != nil {
		return Stop{}, err
	}
	return parseStop(packet, reply)
}

func (c *Conn) Continue(packet string) (Stop, error) {
	_ = c.conn.SetDeadline(time.Now().Add(exchangeTimeout))
	if _, err := c.conn.Write(frame(packet)); err != nil {
		return Stop{}, err
	}
	_ = c.conn.SetDeadline(time.Time{})
	reply, err := c.receive()
	if err != nil {
		return Stop{}, err
	}
	return parseStop(packet, reply)
}

func parseStop(packet, reply string) (Stop, error) {
	if reply == "" {
		return Stop{}, E.New("debugserver: ", packet, ": empty reply")
	}
	switch reply[0] {
	case 'W', 'X':
		return Stop{}, &ExitedError{Reply: reply}
	case 'T', 'S':
	default:
		return Stop{}, E.New("debugserver: ", packet, ": ", reply)
	}
	if len(reply) < 3 {
		return Stop{}, E.New("debugserver: ", packet, ": ", reply)
	}
	signal, err := strconv.ParseUint(reply[1:3], 16, 8)
	if err != nil {
		return Stop{}, E.Cause(err, "debugserver: ", packet, ": ", reply)
	}
	stop := Stop{Signal: int(signal), Values: make(map[string]string)}
	for field := range strings.SplitSeq(reply[3:], ";") {
		key, value, found := strings.Cut(field, ":")
		if found {
			stop.Values[key] = value
		}
	}
	stop.Thread = stop.Values["thread"]
	return stop, nil
}

func (s Stop) Register(number int) (uint64, error) {
	text := s.Values[fmt.Sprintf("%02x", number)]
	data, err := hex.DecodeString(text)
	if err != nil || len(data) != 8 {
		return 0, E.New("debugserver: register ", number, " is ", strconv.Quote(text), " in the stop reply")
	}
	return binary.LittleEndian.Uint64(data), nil
}

func WriteRegister(thread string, number int, value uint64) string {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], value)
	return fmt.Sprintf("P%x=%s;thread:%s;", number, hex.EncodeToString(data[:]), thread)
}

func (c *Conn) ReadMemory(address uint64, length int) ([]byte, error) {
	packet := fmt.Sprintf("m%x,%x", address, length)
	reply, err := c.Query(packet)
	if err != nil {
		return nil, err
	}
	data, err := hex.DecodeString(reply)
	if err != nil || len(data) != length {
		return nil, E.New("debugserver: ", packet, ": ", reply)
	}
	return data, nil
}

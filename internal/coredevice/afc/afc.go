package afc

import (
	"encoding/binary"
	"io"
	"net"

	E "github.com/sagernet/sing/common/exceptions"
)

// The packet format, operations and file modes follow go-ios 6910f3b,
// ios/afc/afc.go:4-22 and ios/afc/client.go:494-499.
const (
	magic        = "CFA6LPAA"
	headerLength = 40
	chunkLength  = 1 << 20

	operationStatus        = 0x01
	operationMakeDirectory = 0x09
	operationOpen          = 0x0D
	operationWrite         = 0x10
	operationClose         = 0x14

	modeWrite = 3
)

type Client struct {
	conn   net.Conn
	packet uint64
}

func New(conn net.Conn) *Client {
	return &Client{conn: conn}
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) request(operation uint64, arguments, payload []byte) ([]byte, error) {
	header := make([]byte, headerLength)
	copy(header, magic)
	binary.LittleEndian.PutUint64(header[8:], uint64(headerLength+len(arguments)+len(payload)))
	binary.LittleEndian.PutUint64(header[16:], uint64(headerLength+len(arguments)))
	binary.LittleEndian.PutUint64(header[24:], c.packet)
	binary.LittleEndian.PutUint64(header[32:], operation)
	c.packet++
	for _, part := range [][]byte{header, arguments, payload} {
		_, err := c.conn.Write(part)
		if err != nil {
			return nil, E.Cause(err, "afc: write")
		}
	}
	_, err := io.ReadFull(c.conn, header)
	if err != nil {
		return nil, E.Cause(err, "afc: read")
	}
	length := binary.LittleEndian.Uint64(header[8:])
	if length < headerLength || length > headerLength+chunkLength {
		return nil, E.New("afc: invalid packet length ", length)
	}
	body := make([]byte, length-headerLength)
	_, err = io.ReadFull(c.conn, body)
	if err != nil {
		return nil, E.Cause(err, "afc: read")
	}
	if binary.LittleEndian.Uint64(header[32:]) == operationStatus {
		if len(body) < 8 {
			return nil, E.New("afc: short status")
		}
		status := binary.LittleEndian.Uint64(body)
		if status != 0 {
			return nil, E.New("afc: status ", status)
		}
		return nil, nil
	}
	return body, nil
}

func (c *Client) MakeDirectory(path string) error {
	_, err := c.request(operationMakeDirectory, append([]byte(path), 0), nil)
	return err
}

func (c *Client) Upload(path string, source io.Reader) error {
	arguments := make([]byte, 8, 8+len(path)+1)
	binary.LittleEndian.PutUint64(arguments, modeWrite)
	handle, err := c.request(operationOpen, append(append(arguments, path...), 0), nil)
	if err != nil {
		return err
	}
	if len(handle) < 8 {
		return E.New("afc: open ", path, ": short handle")
	}
	handle = handle[:8]
	buffer := make([]byte, chunkLength)
	for {
		length, readErr := source.Read(buffer)
		if length > 0 {
			_, err = c.request(operationWrite, handle, buffer[:length])
			if err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	_, err = c.request(operationClose, handle, nil)
	return err
}

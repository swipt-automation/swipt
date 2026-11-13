package afc

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
)

const Service = "com.apple.afc"

// The packet format, operations, file modes and status codes follow go-ios
// 6910f3b, ios/afc/afc.go:4-22 and ios/afc/client.go:494-499, and go-ios
// 63c0d57, ios/afc/errors.go:17 and ios/afc/client.go:440-461, and go-ios
// ca2c56d, ios/afc/afc.go:13.
const (
	magic        = "CFA6LPAA"
	headerLength = 40
	chunkLength  = 1 << 20

	operationStatus        = 0x01
	operationRemovePath    = 0x08
	operationMakeDirectory = 0x09
	operationFileInfo      = 0x0A
	operationOpen          = 0x0D
	operationRead          = 0x0F
	operationWrite         = 0x10
	operationClose         = 0x14

	modeRead  = 1
	modeWrite = 3

	statusObjectNotFound = 8
)

type StatusError uint64

func (e StatusError) Error() string {
	return "afc: status " + strconv.FormatUint(uint64(e), 10)
}

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
			return nil, StatusError(status)
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
	return c.withFile(path, modeWrite, func(handle []byte) error {
		buffer := make([]byte, chunkLength)
		for {
			length, readErr := source.Read(buffer)
			if length > 0 {
				_, err := c.request(operationWrite, handle, buffer[:length])
				if err != nil {
					return err
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	})
}

func (c *Client) Exists(path string) (bool, error) {
	_, err := c.request(operationFileInfo, append([]byte(path), 0), nil)
	if status, failed := errors.AsType[StatusError](err); failed && status == statusObjectNotFound {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) Remove(path string) error {
	_, err := c.request(operationRemovePath, append([]byte(path), 0), nil)
	if status, failed := errors.AsType[StatusError](err); failed && status == statusObjectNotFound {
		return nil
	}
	return err
}

func (c *Client) ReadFile(path string) ([]byte, error) {
	read := make([]byte, 16)
	binary.LittleEndian.PutUint64(read[8:], chunkLength)
	var content []byte
	err := c.withFile(path, modeRead, func(handle []byte) error {
		copy(read, handle)
		for {
			chunk, err := c.request(operationRead, read, nil)
			if err != nil || len(chunk) == 0 {
				return err
			}
			content = append(content, chunk...)
		}
	})
	if err != nil {
		return nil, err
	}
	return content, nil
}

func (c *Client) withFile(path string, mode uint64, use func(handle []byte) error) error {
	arguments := make([]byte, 8, 8+len(path)+1)
	binary.LittleEndian.PutUint64(arguments, mode)
	handle, err := c.request(operationOpen, append(append(arguments, path...), 0), nil)
	if err != nil {
		return err
	}
	if len(handle) < 8 {
		return E.New("afc: open ", path, ": short handle")
	}
	handle = handle[:8]
	err = use(handle)
	_, closeErr := c.request(operationClose, handle, nil)
	return E.Errors(err, closeErr)
}

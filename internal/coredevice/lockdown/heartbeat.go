package lockdown

import (
	"context"
	"fmt"
	"net"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/swipt-automation/swipt/internal/coredevice/rsd"
)

// Wireless services are closed unless the pairing's heartbeat answers Marco
// with Polo (iOS 18.6.2 22G100). The device sends the first Marco at once, and
// each Marco carried an Interval of 10 (iOS 27.3 24C5037i); idevice waits one
// interval for each (idevice 8970209,
// idevice/src/services/heartbeat.rs:62-95).
const heartbeatGrace = 5 * time.Second

type Heartbeat struct {
	conn     net.Conn
	plist    *rsd.PlistConn
	interval time.Duration
	done     chan struct{}
	err      error
}

func StartHeartbeat(ctx context.Context, record *PairRecord, dial Dialer, logger logger.ContextLogger) (*Heartbeat, error) {
	conn, err := DialService(ctx, record, dial, "com.apple.mobile.heartbeat", logger)
	if err != nil {
		return nil, err
	}
	heartbeat := &Heartbeat{conn: conn, plist: rsd.NewPlistConn(conn), interval: 10 * time.Second, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	err = heartbeat.reply()
	if !stop() || ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	logger.Debug("lockdown heartbeat started, interval ", heartbeat.interval)
	go func() {
		defer conn.Close()
		for {
			if err := heartbeat.reply(); err != nil {
				heartbeat.err = err
				close(heartbeat.done)
				return
			}
		}
	}()
	return heartbeat, nil
}

func (h *Heartbeat) reply() error {
	for {
		err := h.conn.SetReadDeadline(time.Now().Add(h.interval + heartbeatGrace))
		if err != nil {
			return E.Cause(err, "read lockdown heartbeat")
		}
		message, err := h.plist.Receive()
		if err != nil {
			return E.Cause(err, "read lockdown heartbeat")
		}
		switch message["Command"] {
		case "Marco":
			if interval, ok := message["Interval"].(uint64); ok && interval > 0 {
				h.interval = time.Duration(interval) * time.Second
			}
			err = h.plist.Send(map[string]any{"Command": "Polo"})
			if err != nil {
				return E.Cause(err, "answer lockdown heartbeat")
			}
			return nil
		case "SleepyTime":
			continue
		default:
			return E.New("lockdown heartbeat: unexpected message ", fmt.Sprint(message))
		}
	}
}

func (h *Heartbeat) Wait() error {
	<-h.done
	return h.err
}

func (h *Heartbeat) Close() error {
	return h.conn.Close()
}

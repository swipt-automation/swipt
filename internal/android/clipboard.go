//go:build android

package android

import (
	"bytes"
	"context"
	"encoding/json"

	E "github.com/sagernet/sing/common/exceptions"
)

type observerFunc struct {
	next   func(string)
	failed func(string)
}

func (o *observerFunc) Next(value string) {
	if o.next != nil {
		o.next(value)
	}
}

func (o *observerFunc) Failed(message string) {
	if o.failed != nil {
		o.failed(message)
	}
}

func (d *Device) Clipboard(ctx context.Context) (json.RawMessage, error) {
	if !d.root {
		value, err := d.Layer.Clipboard()
		if err != nil {
			return nil, Failure(err)
		}
		return json.RawMessage(value), nil
	}
	proc, err := d.helper.started()
	if err != nil {
		return nil, err
	}
	return proc.call(ctx, shellRequest{Method: "clipboard.get"})
}

func (d *Device) SetClipboard(ctx context.Context, text *string) error {
	if !d.root {
		var value string
		if text != nil {
			value = *text
		}
		return Failure(d.Layer.SetClipboard(text != nil, value))
	}
	proc, err := d.helper.started()
	if err != nil {
		return err
	}
	_, err = proc.call(ctx, shellRequest{Method: "clipboard.set", Text: text})
	return err
}

func (d *Device) WatchClipboard(ctx context.Context, publish func(json.RawMessage), ended func(error)) error {
	if !d.root {
		watch, err := d.Layer.WatchState("clipboard", &observerFunc{
			next:   func(value string) { publish(json.RawMessage(value)) },
			failed: func(message string) { ended(Failure(E.New(message))) },
		})
		if err != nil {
			return Failure(err)
		}
		go func() {
			<-ctx.Done()
			watch.Stop()
		}()
		return nil
	}
	proc, err := d.helper.started()
	if err != nil {
		return err
	}
	changed := make(chan struct{}, 1)
	changed <- struct{}{}
	select {
	case proc.clipboardAccess <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if len(proc.clipboardWatchers) == 0 {
		_, err = proc.call(ctx, shellRequest{Method: "clipboard.watch"})
	}
	if err == nil {
		proc.access.Lock()
		proc.clipboardWatchers[changed] = struct{}{}
		proc.access.Unlock()
	}
	<-proc.clipboardAccess
	if err != nil {
		return err
	}
	go func() {
		defer proc.unwatchClipboard(changed)
		var last json.RawMessage
		for {
			select {
			case <-changed:
			case <-proc.exited:
				ended(proc.failure)
				return
			case <-ctx.Done():
				return
			}
			value, err := proc.call(ctx, shellRequest{Method: "clipboard.get"})
			if err != nil {
				if ctx.Err() == nil {
					ended(err)
				}
				return
			}
			if !bytes.Equal(value, last) {
				last = value
				publish(value)
			}
		}
	}()
	return nil
}

func (d *Device) Advertise(
	name string,
	serviceType string,
	port int,
	hostname string,
	attributes []string,
	onHostname func(string),
	onError func(error),
) (func(), error) {
	if !d.root {
		encoded, err := json.Marshal(attributes)
		if err != nil {
			return nil, err
		}
		watch, err := d.Layer.Advertise(name, serviceType, int32(port), hostname, string(encoded), &observerFunc{
			next:   onHostname,
			failed: func(message string) { onError(Failure(E.New(message))) },
		})
		if err != nil {
			return nil, Failure(err)
		}
		return watch.Stop, nil
	}
	proc, err := d.helper.started()
	if err != nil {
		return nil, err
	}
	proc.access.Lock()
	proc.advertiseSession++
	session := proc.advertiseSession
	proc.onAdvertiseHostname = onHostname
	proc.onAdvertiseError = onError
	proc.access.Unlock()
	_, err = proc.call(context.Background(), shellRequest{
		Method:     "advertise.start",
		Session:    session,
		Name:       name,
		Type:       serviceType,
		Port:       port,
		Hostname:   hostname,
		Attributes: attributes,
	})
	if err != nil {
		proc.access.Lock()
		if proc.advertiseSession == session {
			proc.onAdvertiseHostname, proc.onAdvertiseError = nil, nil
		}
		proc.access.Unlock()
		return nil, err
	}
	return func() {
		proc.access.Lock()
		if proc.advertiseSession != session {
			proc.access.Unlock()
			return
		}
		proc.onAdvertiseHostname, proc.onAdvertiseError = nil, nil
		proc.access.Unlock()
		_, _ = proc.call(context.Background(), shellRequest{Method: "advertise.stop", Session: session})
	}, nil
}

func (p *shellHelperProcess) unwatchClipboard(changed chan struct{}) {
	p.clipboardAccess <- struct{}{}
	defer func() { <-p.clipboardAccess }()
	p.access.Lock()
	delete(p.clipboardWatchers, changed)
	last := len(p.clipboardWatchers) == 0
	p.access.Unlock()
	if last {
		_, _ = p.call(context.Background(), shellRequest{Method: "clipboard.unwatch"})
	}
}

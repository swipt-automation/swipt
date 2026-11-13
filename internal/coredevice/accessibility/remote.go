package accessibility

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/dtx"
)

// axauditd gives its reads of applications a messaging timeout of 10000 s
// (24B5110a axauditd 0x10000cbb4..0x10000cbc0, 0x1000115b8).
var ErrStalled = E.New("accessibility: the device stopped answering")

type StallError struct {
	PID    uint64
	Reason string
}

func (e *StallError) Error() string {
	return ErrStalled.Error() + ": " + e.Reason
}

func (e *StallError) Unwrap() error {
	return ErrStalled
}

// axauditd accepts connections on its main queue (24B5110a axauditd
// 0x10000cc78..0x10000cc90), which also serves the requests of every
// connection and its own reads of the tree (24B5110a AccessibilityAudit
// 0x253f9c670..0x253f9c67c; 24B5110a AccessibilityPlatformTranslation
// 0x253fe12b8..0x253fe12c4).
const handshakeWait = 7 * time.Second

// AXPTranslatorRequest request types, which the response carries back as
// associatedRequestType (24A446 AccessibilityPlatformTranslation
// -[AXPTranslator processTranslatorRequest:] 0x24fe57a4c, 0x24fe57bec).
const (
	requestApplication = 1
	requestAttribute   = 2
	requestFrontmost   = 4
	requestAttributes  = 5
	requestHitTest     = 6
	requestAction      = 7
	requestTree        = 11
)

// AXPAttribute numbers (24A446 AccessibilityPlatformTranslation
// __AXPAttributeToString 0x24fe585dc). In a requestAttribute request,
// attributeNative reads the native UIAccessibility attribute given as
// "parameter" (-[AXPTranslator_iOS processAttributeRequest:] 0x24fe4e4c8).
const (
	attributeBundleIdentifier = 6
	attributeCustomActions    = 13
	attributeFocusedElement   = 19
	attributeFrame            = 21
	attributeIdentifier       = 25
	attributeIsEnabled        = 27
	attributeIsValid          = 31
	attributeLabel            = 33
	attributeRawElementData   = 60
	attributeNative           = 63
	attributeTraits           = 77
)

// UIAccessibility native attributes (24A446 UIAccessibility
// -[NSObject(AXPrivCategory) _iosAccessibilityAttributeValue:] 0x1bb739f00).
const (
	nativeClassName = 5003
)

// AX trait bits (24A446 AXRuntime _kAXSelectedTrait 0x18f8773f0,
// _kAXNotEnabledTrait 0x18f877418, _kAXTextEntryTrait 0x18f877460,
// _kAXSecureTextFieldTrait 0x18f877498).
const (
	traitSelected        = 0x8
	traitNotEnabled      = 0x100
	traitTextEntry       = 0x40000
	traitSecureTextField = 0x01000000
)

type RemoteClient struct {
	conn                 *dtx.Conn
	ctx                  context.Context
	owner                context.Context
	cancel               context.CancelCauseFunc
	stopContext          func() bool
	closeOnce            sync.Once
	closeErr             error
	enabled              bool
	mu                   sync.Mutex
	stateMu              sync.Mutex
	pending              []*pendingRead
	drained              chan struct{}
	screenViewing        []translationObject
	screenViewingPending chan struct{}
	display              func() int
	pushed               func(Push)
}

func DialRemote(ctx, owner context.Context, device *coredevice.Device, display func() int, pushed func(Push)) (_ *RemoteClient, err error) {
	// axauditd starts DTX on a remoteAXService connection without reading an
	// RSDCheckin (24B5110a axauditd 0x10000d214..0x10000d310).
	conn, err := device.DialService(ctx, "com.apple.accessibility.axAuditDaemon.remoteAXService")
	if err != nil {
		return nil, E.Cause(err, "accessibility: connect remoteAXService")
	}
	dtxConn, err := dtx.NewConn(owner, conn, func(event dtx.Event) (any, bool) {
		// Accessibility Inspector answers 7 (Xcode 27.1 27A9275
		// AccessibilityAuditDeviceManager -[XDMDevice hostApiVersion] 0x2954).
		if event.Selector == "hostAPIVersion" {
			return int64(7), true
		}
		if event.Selector == "_notifyOfPublishedCapabilities:" {
			return nil, true
		}
		return nil, false
	})
	if err != nil {
		return nil, E.Cause(err, "accessibility: initialize DTX")
	}
	operation, cancel := context.WithCancelCause(ctx)
	c := &RemoteClient{
		conn: dtxConn, ctx: operation, owner: owner, cancel: cancel,
		screenViewingPending: make(chan struct{}, 1),
		display:              display,
		pushed:               pushed,
	}
	c.mu.Lock()
	go c.readLoop()
	c.stopContext = context.AfterFunc(operation, func() { c.Close() })
	defer func() {
		c.mu.Unlock()
		if err != nil {
			err = E.Append(err, c.Close(), func(err error) error {
				return E.Cause(err, "accessibility: stop remote AX")
			})
		}
	}()
	handshake, cancelHandshake := context.WithTimeout(operation, handshakeWait)
	_, err = dtxConn.Invoke(handshake, 0, "deviceAPIVersion")
	cancelHandshake()
	if errors.Is(err, context.DeadlineExceeded) && operation.Err() == nil {
		return nil, &StallError{Reason: "no answer to deviceAPIVersion within " + handshakeWait.String()}
	}
	if cause := context.Cause(operation); cause != nil {
		return nil, cause
	}
	if err != nil {
		return nil, E.Cause(err, "accessibility: invoke deviceAPIVersion")
	}
	// AXAuditDevicesAppRemoteServer drops processDataFromHost: data until
	// clientNeedsAccessibility: true, which also starts pushing the cached tree
	// (24A446 AccessibilityAudit 0x24fe0b45c, 0x24fe0b5e4..0x24fe0b61c).
	if err = dtxConn.Notify(owner, 0, "clientNeedsAccessibility:", true); err != nil {
		return nil, E.Cause(err, "accessibility: notify clientNeedsAccessibility:")
	}
	c.enabled = true
	if err = context.Cause(operation); err != nil {
		return nil, err
	}
	return c, nil
}

const closeWait = 200 * time.Millisecond

func (c *RemoteClient) Close() error {
	c.closeOnce.Do(func() {
		c.cancel(net.ErrClosed)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.stopContext()
		// clientNeedsAccessibility: false clears the server's cache manager and
		// cancels the connection (24B5110a AccessibilityAudit
		// 0x253f9c578..0x253f9c5a8, 0x253f9be80..0x253f9be90), while a lost
		// socket only clears the connection (24B5110a AccessibilityAudit
		// 0x253f9bc3c..0x253f9bce4) and leaves the server, its cache manager and
		// the manager's _transportChannel retaining each other (24B5110a
		// AccessibilityPlatformTranslation 0x253fe4994..0x253fe49a8).
		if c.enabled {
			ctx, cancel := context.WithTimeout(c.owner, closeWait)
			if err := c.conn.Notify(ctx, 0, "clientNeedsAccessibility:", false); err != nil {
				c.closeErr = E.Cause(err, "accessibility: notify clientNeedsAccessibility: false")
			} else {
				select {
				case <-c.conn.Done():
				case <-ctx.Done():
					c.closeErr = E.New("accessibility: the device kept the connection open after clientNeedsAccessibility: false")
				}
			}
			cancel()
		}
		c.conn.Close()
	})
	return c.closeErr
}

func (c *RemoteClient) CloseCause(cause error) error {
	c.cancel(cause)
	return c.Close()
}

type translationObject struct {
	pid         uint64
	objectID    uint64
	application bool
	fields      map[string]any
}

func parseTranslation(value any) (translationObject, bool) {
	fields, ok := value.(map[string]any)
	if !ok || fields["$class"] != "AXPTranslationObject" {
		return translationObject{}, false
	}
	pid, pidOK := unsigned(fields["pid"])
	id, idOK := unsigned(fields["objectID"])
	application, appOK := fields["isApplicationElement"].(bool)
	return translationObject{pid: pid, objectID: id, application: application, fields: fields}, pidOK && idOK && appOK && pid != 0 && id != 0
}

func (t translationObject) same(other translationObject) bool {
	return t.pid == other.pid && t.objectID == other.objectID && t.application == other.application
}

// AXPTranslationObject spells the key didPopuldateAppInfo (24A446
// AccessibilityPlatformTranslation -[AXPTranslationObject encodeWithCoder:]
// 0x24fe58b20).
func (t translationObject) archiveObject() dtx.Object {
	fields := make(map[string]any)
	for _, key := range []string{"pid", "objectID", "isApplicationElement", "didPopuldateAppInfo", "bridgeDelegateToken", "rawElementData"} {
		if value, ok := t.fields[key]; ok {
			fields[key] = value
		}
	}
	return dtx.Object{ClassName: "AXPTranslationObject", Fields: fields, PrimitiveFields: []string{"pid", "isApplicationElement", "didPopuldateAppInfo"}}
}

type remoteResponse struct {
	value any
	code  uint64
}

type remoteRequest struct {
	kind      uint64
	object    *translationObject
	attribute uint64
	action    uint64
	params    any
}

func (c *RemoteClient) query(ctx context.Context, kind uint64, obj *translationObject, attribute, action uint64, params any) (remoteResponse, error) {
	responses, err := c.exchange(ctx, []remoteRequest{{kind: kind, object: obj, attribute: attribute, action: action, params: params}})
	if err != nil {
		return remoteResponse{}, err
	}
	return responses[0], nil
}

// AXP responses carry the request type but no identifier (24A446
// AccessibilityPlatformTranslation -[AXPTranslatorResponse encodeWithCoder:]
// 0x24fe59764), and axauditd serves requests one after another on its main
// queue (24B5110a AccessibilityAudit 0x253f9c670..0x253f9c67c).
func (c *RemoteClient) exchange(ctx context.Context, requests []remoteRequest) ([]remoteResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := context.Cause(c.ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := context.AfterFunc(c.ctx, func() { cancel(context.Cause(c.ctx)) })
	defer stop()
	wait := exchangeWait(requests)
	if err := c.drain(ctx, wait); err != nil {
		return nil, err
	}
	reads := make([]*pendingRead, 0, len(requests))
	for _, request := range requests {
		var translation any
		if request.object != nil {
			translation = request.object.archiveObject()
		} else if request.kind == requestFrontmost || request.kind == requestHitTest {
			translation = dtx.Object{ClassName: "AXPTranslationObject", Fields: map[string]any{
				"pid": int64(0), "objectID": int64(0), "isApplicationElement": false, "didPopuldateAppInfo": false,
			}, PrimitiveFields: []string{"pid", "isApplicationElement", "didPopuldateAppInfo"}}
		}
		data, err := dtx.MarshalArchive(dtx.Object{ClassName: "AXPTranslatorRequest", Fields: map[string]any{
			"requestType": request.kind, "attributeType": request.attribute, "actionType": request.action, "clientType": int64(2),
			"translation": translation, "parameters": request.params,
		}, PrimitiveFields: []string{"requestType", "attributeType", "actionType", "clientType"}})
		if err != nil {
			return nil, err
		}
		read := &pendingRead{kind: request.kind, attribute: request.attribute, sent: time.Now(), response: make(chan remoteResponse, 1)}
		if request.object != nil {
			read.pid = request.object.pid
		}
		if params, isMap := request.params.(map[string]any); isMap && request.attribute == attributeNative {
			read.native, _ = unsigned(params["parameter"])
		}
		reads = append(reads, read)
		c.stateMu.Lock()
		c.pending = append(c.pending, read)
		c.stateMu.Unlock()
		if err = c.conn.Notify(ctx, 0, "processDataFromHost:", data); err != nil {
			if ctx.Err() != nil {
				err = context.Cause(ctx)
			}
			c.cancel(err)
			return nil, err
		}
	}
	stalled := time.NewTimer(wait)
	defer stalled.Stop()
	responses := make([]remoteResponse, len(reads))
	for index, read := range reads {
		select {
		case responses[index] = <-read.response:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-stalled.C:
			c.stateMu.Lock()
			queued := len(c.pending)
			c.stateMu.Unlock()
			return nil, &StallError{PID: read.pid, Reason: F.ToString("no answer to ", len(reads)-index, " of ", len(reads), " requests within ", wait,
				"; the first unanswered is ", read.describe(), "; ", queued, " requests of this connection are unanswered")}
		}
	}
	return responses, nil
}

func (c *RemoteClient) drain(ctx context.Context, wait time.Duration) error {
	c.stateMu.Lock()
	if len(c.pending) == 0 {
		c.stateMu.Unlock()
		return nil
	}
	head := c.pending[0]
	queued := len(c.pending)
	if c.drained == nil {
		c.drained = make(chan struct{})
	}
	drained := c.drained
	c.stateMu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return &StallError{PID: head.pid, Reason: F.ToString("the ", queued, " requests an earlier read left unanswered got no answer within ", wait,
			" more; the first of them is ", head.describe())}
	}
}

func exchangeWait(requests []remoteRequest) time.Duration {
	wait := 3*time.Second + time.Duration(len(requests))*6*time.Millisecond
	if slices.ContainsFunc(requests, remoteRequest.heavy) {
		wait += 4 * time.Second
	}
	return wait
}

func (r remoteRequest) heavy() bool {
	switch r.kind {
	case requestAction, requestSet:
		return true
	case requestAttribute:
		return r.attribute == attributeUserTestingSnapshot || r.attribute == attributeSearch
	}
	return false
}

type pendingRead struct {
	kind      uint64
	attribute uint64
	native    uint64
	pid       uint64
	sent      time.Time
	response  chan remoteResponse
}

func (r *pendingRead) describe() string {
	text := F.ToString("request type ", r.kind)
	if r.kind == requestAttribute {
		text += F.ToString(" for attribute ", r.attribute)
	}
	if r.native != 0 {
		text += F.ToString(" of native attribute ", r.native)
	}
	if r.pid != 0 {
		text += F.ToString(" to process ", r.pid)
	}
	return text + ", sent " + time.Since(r.sent).Round(time.Millisecond).String() + " ago"
}

func (c *RemoteClient) readLoop() {
	for {
		event, err := c.conn.Receive(context.Background())
		if err == nil {
			err = c.receive(event)
		}
		if err != nil {
			c.cancel(err)
			return
		}
	}
}

func (c *RemoteClient) receive(event dtx.Event) error {
	if event.Selector != "processDataFromRemoteDevice:" {
		return nil
	}
	for _, argument := range event.Arguments {
		data, ok := argument.([]byte)
		if !ok {
			return E.New("accessibility: remote AX response is not archived data")
		}
		decoded, err := dtx.UnmarshalArchive(data)
		if err != nil {
			return err
		}
		response, ok := decoded.(map[string]any)
		if !ok || response["$class"] != "AXPTranslatorResponse" {
			return E.New("accessibility: invalid remote AX response")
		}
		kind, kindOK := unsigned(response["associatedRequestType"])
		attribute, attrOK := unsigned(response["attribute"])
		code, codeOK := unsigned(response["error"])
		notification, notificationOK := unsigned(response["notification"])
		if !kindOK || !attrOK || !codeOK || !notificationOK {
			return E.New("accessibility: invalid remote AX response metadata")
		}
		switch {
		case notification != 0:
			if c.pushed != nil {
				c.pushed(pushedNotification(notification, response))
			}
		case kind == requestTree:
			c.receiveScreenViewingNotifications(response["resultData"])
			if c.pushed != nil {
				c.pushed(pushedTree(response["resultData"]))
			}
		default:
			c.stateMu.Lock()
			if len(c.pending) == 0 {
				c.stateMu.Unlock()
				return E.New("accessibility: a response of request type ", kind, " came with no request pending")
			}
			head := c.pending[0]
			if head.kind != kind || kind == requestAttribute && head.attribute != attribute {
				c.stateMu.Unlock()
				return E.New("accessibility: a response of request type ", kind, " and attribute ", attribute,
					" came for request type ", head.kind, " and attribute ", head.attribute)
			}
			c.pending = c.pending[1:]
			if len(c.pending) == 0 && c.drained != nil {
				close(c.drained)
				c.drained = nil
			}
			c.stateMu.Unlock()
			head.response <- remoteResponse{value: response["resultData"], code: code}
		}
	}
	return nil
}

func unsigned(value any) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, true
	case int64:
		return uint64(number), number >= 0
	case uint32:
		return uint64(number), true
	case int:
		return uint64(number), number >= 0
	default:
		return 0, false
	}
}

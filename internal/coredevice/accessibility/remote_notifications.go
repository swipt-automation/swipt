package accessibility

import (
	"context"
	"slices"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
)

// dtremotedisplayd holds a screen-viewing audit activity while a stream runs
// (DDI 27B5028f dtremotedisplayd 0x10002c610, 0x100033134), and at the next
// unlock remotepairingdeviced posts one notification for the ended activities
// from com.apple.RemotePairing.AuditActivityNotifications (24B5110a
// remotepairingdeviced 0x100016de4, 0x1000170d0..0x10001711c), titled "Device
// Hub" with the body "This device was viewed remotely from Mac" (24A446
// RemotePairingDevice 0x23ebbb7c0, 0x23ebbb8b0). Both strings go through
// NSLocalizedString, yet stayed English on a device set to Chinese, where the
// cell's AX label started with "DEVICE HUB, " and ended with the body (iOS
// 27.2 24B5110a).
func screenViewingNotification(label string) bool {
	source, _, found := strings.Cut(label, ", ")
	return found && strings.EqualFold(source, "Device Hub") &&
		strings.HasSuffix(label, ", This device was viewed remotely from Mac")
}

func (c *RemoteClient) receiveScreenViewingNotifications(value any) {
	data, ok := value.(map[string]any)
	if !ok {
		return
	}
	tree, _ := data["treeDump"].([]any)
	var candidates []translationObject
	for _, item := range tree {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := ""
		if attribute, _ := unsigned(entry["attribute"]); attribute == attributeLabel {
			label, _ = entry["resultData"].(string)
		} else if values, ok := entry["resultData"].(map[any]any); ok {
			label, _ = values[uint64(attributeLabel)].(string)
		}
		obj, ok := parseTranslation(entry["associatedTranslationObject"])
		if ok && !obj.application && screenViewingNotification(label) {
			candidates = append(candidates, obj)
		}
	}
	if len(candidates) == 0 {
		return
	}
	c.stateMu.Lock()
	for _, obj := range candidates {
		if !slices.ContainsFunc(c.screenViewing, obj.same) {
			c.screenViewing = append(c.screenViewing, obj)
		}
	}
	c.stateMu.Unlock()
	select {
	case c.screenViewingPending <- struct{}{}:
	default:
	}
}

func (c *RemoteClient) ScreenViewingNotifications() <-chan struct{} {
	return c.screenViewingPending
}

func (c *RemoteClient) DismissScreenViewingNotifications(ctx context.Context) (int, error) {
	c.stateMu.Lock()
	candidates := c.screenViewing
	c.screenViewing = nil
	c.stateMu.Unlock()
	if len(candidates) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	count := 0
	for _, obj := range candidates {
		dismissed, err := c.dismissScreenViewingNotification(ctx, obj, "ListCell", "NotificationShortLookView")
		if err != nil {
			return count, err
		}
		if dismissed {
			count++
		}
	}
	return count, nil
}

func (c *RemoteClient) DismissScreenViewingBanner(ctx context.Context, element Element) (bool, error) {
	if element.object.application || element.object.pid == 0 {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.dismissScreenViewingNotification(ctx, element.object, "NotificationShortLookView")
}

func (c *RemoteClient) dismissScreenViewingNotification(ctx context.Context, obj translationObject, identifiers ...string) (bool, error) {
	application, err := c.query(ctx, requestApplication, nil, 0, 0, map[string]any{"pid": obj.pid})
	if err != nil {
		return false, err
	}
	app, ok := parseTranslation(application.value)
	if application.code != 0 || !ok || !app.application || app.pid != obj.pid {
		return false, nil
	}
	bundle, err := c.query(ctx, requestAttribute, &app, attributeBundleIdentifier, 0, nil)
	if err != nil {
		return false, err
	}
	if bundle.code != 0 || bundle.value != "com.apple.springboard" {
		return false, nil
	}
	response, err := c.query(ctx, requestAttributes, &obj, 0, 0, map[string]any{
		"attributes": []any{int64(attributeLabel), int64(attributeIdentifier), int64(attributeIsValid), int64(attributeCustomActions)},
	})
	if err != nil {
		return false, err
	}
	values, ok := response.value.(map[any]any)
	// Notification Center uses ListCell (iOS 27.2 24B5110a and 24B115),
	// while the banner uses NotificationShortLookView (iOS 27.3 24C5037i).
	kind, _ := values[uint64(attributeIdentifier)].(string)
	if response.code != 0 || !ok || values[uint64(attributeIsValid)] != true || !slices.Contains(identifiers, kind) {
		return false, nil
	}
	label, _ := values[uint64(attributeLabel)].(string)
	if !screenViewingNotification(label) {
		return false, nil
	}
	actions, _ := values[uint64(attributeCustomActions)].([]any)
	identifier := ""
	for _, value := range actions {
		action, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["AXPCustomActionIdentifier"].(string)
		// A custom action's identifier is "Name:%@\nTarget:%p\nSelector:%@"
		// with its localized name (24A446 UIAccessibility 0x1bb719180), so a
		// block-backed one ends in "\nTarget:0x0\nSelector:(null)". On the
		// Device Hub cell only Clear was, beside More with _axMore: (iOS 27.2
		// 24B5110a), and on the banner only Close (iOS 27.3 24C5037i).
		if strings.HasSuffix(id, "\nTarget:0x0\nSelector:(null)") {
			if identifier != "" {
				return false, nil
			}
			identifier = id
		}
	}
	if identifier == "" {
		return false, nil
	}
	// AXP action 4 performs the custom action selected by this identifier and
	// answers whether it ran (24A446 AccessibilityPlatformTranslation
	// 0x24fe48264..0x24fe482a8, 0x24fe48210..0x24fe48224).
	response, err = c.query(ctx, requestAction, &obj, 0, actionCustom, map[string]any{"AXPCustomActionIdentifier": identifier})
	if err != nil {
		return false, err
	}
	if response.code != 0 {
		return false, E.New("accessibility: clear screen-viewing notification: response ", response.code)
	}
	dismissed, _ := response.value.(bool)
	return dismissed, nil
}

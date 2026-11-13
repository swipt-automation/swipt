package pairing

// The host side of pair-setup is a port of pymobiledevice3's RemotePairingProtocol
// (https://github.com/doronz88/pymobiledevice3, pymobiledevice3/remote/tunnel_service.py),
// GPL-3.0, Copyright (c) doronz88.

import (
	"context"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha512"
	"fmt"
	"net"
	"slices"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/swipt-automation/swipt/internal/coredevice/opack"
	"github.com/swipt-automation/swipt/internal/coredevice/tlv8"

	"golang.org/x/crypto/chacha20poly1305"
)

// pymobiledevice3 pairs with "000000" when the device shows no setup code
// (pymobiledevice3 9c33842, pymobiledevice3/remote/tunnel_service.py:911).
const pinlessSetupCode = "000000"

type signLabels struct {
	salt string
	info string
}

var (
	accessorySignLabels  = signLabels{"Pair-Setup-Accessory-Sign-Salt", "Pair-Setup-Accessory-Sign-Info"}
	controllerSignLabels = signLabels{"Pair-Setup-Controller-Sign-Salt", "Pair-Setup-Controller-Sign-Info"}
)

type SetupOptions struct {
	HostName string
	Logger   logger.ContextLogger
}

func Setup(ctx context.Context, netConn net.Conn, options SetupOptions) (*Record, error) {
	defer netConn.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Logger == nil {
		options.Logger = logger.NOP()
	}
	stop := context.AfterFunc(ctx, func() { netConn.Close() })
	record, err := setup(newConn(netConn, originHost), options)
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, contextError(ctx, err)
	}
	return record, nil
}

func setup(c *conn, options SetupOptions) (*Record, error) {
	err := c.writePlain(object{"request": object{"_0": object{"handshake": object{"_0": object{
		"hostOptions":         object{"attemptPairVerify": false},
		"wireProtocolVersion": clientWireProtocolVersion,
	}}}}})
	if err != nil {
		return nil, E.Cause(err, "send handshake")
	}
	reply, err := c.readPlain()
	if err != nil {
		return nil, E.Cause(err, "read handshake")
	}
	handshake := lookupObject(reply, "response", "_1", "handshake", "_0")
	if handshake == nil {
		return nil, E.New("pairing: unexpected handshake reply: ", fmt.Sprint(reply))
	}
	options.Logger.Debug("remote pairing handshake: ", fmt.Sprint(handshake))
	if allowed, _ := lookup(handshake, "deviceOptions", "allowsPairSetup").(bool); !allowed {
		return nil, E.New("pairing: the device does not allow pair-setup here")
	}
	write := func(items ...tlv8.Item) error {
		return c.writePlain(pairingDataEvent("setupManualPairing", tlv8.Encode(items...), items[0].Type == tlv8.TypeMethod, options.HostName))
	}
	err = write(
		tlv8.Item{Type: tlv8.TypeMethod, Value: []byte{0}},
		tlv8.Item{Type: tlv8.TypeState, Value: []byte{1}},
	)
	if err != nil {
		return nil, E.Cause(err, "send M1")
	}
	m2, err := readSetup(c, 2)
	if err != nil {
		return nil, E.Cause(err, "read M2")
	}
	client, err := newSRPClient(pinlessSetupCode, m2[tlv8.TypeSalt], m2[tlv8.TypePublicKey])
	if err != nil {
		return nil, err
	}
	err = write(
		tlv8.Item{Type: tlv8.TypeState, Value: []byte{3}},
		tlv8.Item{Type: tlv8.TypePublicKey, Value: client.PublicKey()},
		tlv8.Item{Type: tlv8.TypeProof, Value: client.proof},
	)
	if err != nil {
		return nil, E.Cause(err, "send M3")
	}
	m4, err := readSetup(c, 4)
	if err != nil {
		return nil, E.Cause(err, "read M4")
	}
	if !client.VerifyServerProof(m4[tlv8.TypeProof]) {
		return nil, E.New("pairing: M4: SRP proof mismatch")
	}
	setupCipher, err := setupCipher(client.key)
	if err != nil {
		return nil, err
	}
	record, err := newIdentity(nil)
	if err != nil {
		return nil, err
	}
	m5, err := identityTLV(record, client.key, controllerSignLabels, options.HostName)
	if err != nil {
		return nil, err
	}
	err = write(
		tlv8.Item{Type: tlv8.TypeEncryptedData, Value: setupCipher.Seal(nil, pairingNonce("PS-Msg05"), m5, nil)},
		tlv8.Item{Type: tlv8.TypeState, Value: []byte{5}},
	)
	if err != nil {
		return nil, E.Cause(err, "send M5")
	}
	m6, err := readSetup(c, 6)
	if err != nil {
		return nil, E.Cause(err, "read M6")
	}
	plaintext, err := setupCipher.Open(nil, pairingNonce("PS-Msg06"), m6[tlv8.TypeEncryptedData], nil)
	if err != nil {
		return nil, E.Cause(err, "decrypt M6")
	}
	err = parseDeviceIdentity(plaintext, record, options.Logger)
	if err != nil {
		return nil, E.Cause(err, "parse M6")
	}
	if record.DeviceUDID == "" {
		record.DeviceUDID = lookupString(handshake, "peerDeviceInfo", "udid")
	}
	options.Logger.Info("paired with ", record.DeviceName, " (", record.DeviceUDID, ")")
	return record, nil
}

func readSetup(c *conn, state byte) (tlv8.Values, error) {
	data, err := c.readPairingData()
	if err != nil {
		return nil, err
	}
	values, err := tlv8.Decode(data)
	if err != nil {
		return nil, err
	}
	if code, failed := values.Error(); failed {
		return nil, E.New("pairing: the device reported ", tlv8.ErrorString(code))
	}
	if values.State() != state {
		return nil, E.New("pairing: expected state ", state, ", got ", values.State())
	}
	return values, nil
}

func setupCipher(key []byte) (cipher.AEAD, error) {
	// idevice uses these Pair-Setup HKDF labels for M5/M6
	// (idevice 3854a5d, idevice/src/remote_pairing/responder.rs:355-362).
	setupKey, err := hkdf.Key(sha512.New, key, []byte("Pair-Setup-Encrypt-Salt"), "Pair-Setup-Encrypt-Info", chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	return chacha20poly1305.New(setupKey)
}

func identityTLV(record *Record, key []byte, labels signLabels, hostName string) ([]byte, error) {
	signKey, err := hkdf.Key(sha512.New, key, []byte(labels.salt), labels.info, 32)
	if err != nil {
		return nil, err
	}
	signed := slices.Concat(signKey, []byte(record.Identifier), record.PublicKey)
	// Info keys and placeholder addresses match idevice PairableHost
	// (idevice 3854a5d, idevice/src/remote_pairing/responder.rs:438-447).
	info, err := opack.Marshal(map[string]any{
		"altIRK":                      record.HostIRK,
		"btAddr":                      "11:22:33:44:55:66",
		"mac":                         []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		"remotepairing_serial_number": "AAAAAAAAAAAA",
		"accountID":                   record.Identifier,
		"remotepairing_udid":          "",
		"model":                       responderModel,
		"name":                        hostName,
	})
	if err != nil {
		return nil, err
	}
	return tlv8.Encode(
		tlv8.Item{Type: tlv8.TypeIdentifier, Value: []byte(record.Identifier)},
		tlv8.Item{Type: tlv8.TypePublicKey, Value: record.PublicKey},
		tlv8.Item{Type: tlv8.TypeSignature, Value: ed25519.Sign(record.PrivateKey, signed)},
		tlv8.Item{Type: tlv8.TypeInfo, Value: info},
	), nil
}

// idevice parses the decrypted identity without verifying its signature
// (idevice 3854a5d, idevice/src/remote_pairing/responder.rs:384-386;
// idevice/src/remote_pairing/peer_device.rs:115-120).
func parseDeviceIdentity(plaintext []byte, record *Record, logger logger.ContextLogger) error {
	values, err := tlv8.Decode(plaintext)
	if err != nil {
		return E.Cause(err, "decode identity")
	}
	record.DeviceIdentifier = string(values[tlv8.TypeIdentifier])
	if devicePublicKey := values[tlv8.TypePublicKey]; len(devicePublicKey) == ed25519.PublicKeySize {
		record.DevicePublicKey = devicePublicKey
	}
	if infoData := values[tlv8.TypeInfo]; len(infoData) > 0 {
		info, err := opack.Unmarshal(infoData)
		if err != nil {
			return E.Cause(err, "decode identity info")
		}
		logger.Debug("paired device info: ", fmt.Sprint(info))
		infoMap, _ := info.(map[string]any)
		record.AltIRK, _ = infoMap["altIRK"].([]byte)
		record.DeviceName, _ = infoMap["name"].(string)
		record.DeviceModel, _ = infoMap["model"].(string)
		record.DeviceUDID, _ = infoMap["remotepairing_udid"].(string)
	}
	// idevice rejects altIRK values whose length is not 16 bytes
	// (idevice 3854a5d, idevice/src/remote_pairing/peer_device.rs:47-54).
	if len(record.AltIRK) != 16 {
		return E.New("pairing: the device sent an altIRK of ", len(record.AltIRK), " bytes, expected 16")
	}
	return nil
}

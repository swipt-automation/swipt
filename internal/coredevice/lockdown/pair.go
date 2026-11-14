package lockdown

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/rsd"

	"github.com/google/uuid"
	"howett.net/plist"
)

const certificateLife = 10 * 365 * 24 * time.Hour

type Pairing struct {
	HostID       string
	HostRecord   []byte
	DeviceRecord []byte
}

// A device keeps a pair record with these keys (iOS 27.0.1 24A446),
// and usbmuxd one with the keys of libimobiledevice c6f89de, common/userpref.c:322-341.
// The certificates follow pair_record_generate_keys_and_certs of the same file.
func NewPairing(ctx context.Context, dial Dialer, systemBUID, udid, hostName string) (*Pairing, error) {
	value, err := GetValue(ctx, dial, "DevicePublicKey")
	if err != nil {
		return nil, err
	}
	encoded, _ := value.([]byte)
	block, _ := pem.Decode(encoded)
	if block == nil {
		return nil, E.New("lockdown: the device has no public key")
	}
	devicePublicKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, E.Cause(err, "lockdown: parse the device's public key")
	}
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	hostKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          big.NewInt(0),
		Subject:               pkix.Name{},
		NotBefore:             now,
		NotAfter:              now.Add(certificateLife),
		BasicConstraintsValid: true,
	}
	root := template
	root.IsCA = true
	root.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	rootDER, err := x509.CreateCertificate(rand.Reader, &root, &root, rootKey.Public(), rootKey)
	if err != nil {
		return nil, E.Cause(err, "lockdown: create the root certificate")
	}
	issuer, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, err
	}
	leaf := template
	leaf.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	hostDER, err := x509.CreateCertificate(rand.Reader, &leaf, issuer, hostKey.Public(), rootKey)
	if err != nil {
		return nil, E.Cause(err, "lockdown: create the host certificate")
	}
	deviceDER, err := x509.CreateCertificate(rand.Reader, &leaf, issuer, devicePublicKey, rootKey)
	if err != nil {
		return nil, E.Cause(err, "lockdown: create the device certificate")
	}
	certificatePEM := func(der []byte) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	keyPEM := func(key *rsa.PrivateKey) []byte {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	}
	hostID := uuid.NewString()
	hostRecord, err := plist.Marshal(map[string]any{
		"DeviceCertificate": certificatePEM(deviceDER),
		"HostCertificate":   certificatePEM(hostDER),
		"HostID":            hostID,
		"HostPrivateKey":    keyPEM(hostKey),
		"RootCertificate":   certificatePEM(rootDER),
		"RootPrivateKey":    keyPEM(rootKey),
		"SystemBUID":        systemBUID,
		"UDID":              udid,
	}, plist.XMLFormat)
	if err != nil {
		return nil, err
	}
	deviceRecord, err := plist.Marshal(map[string]any{
		"DeviceCertificate":   certificatePEM(deviceDER),
		"HostCertificate":     certificatePEM(hostDER),
		"HostID":              hostID,
		"HostName":            hostName,
		"RootCertificate":     certificatePEM(rootDER),
		"SerialNumber":        "",
		"SystemBUID":          systemBUID,
		"TimeWhenCreated":     0,
		"WallTimeWhenCreated": now.Unix(),
	}, plist.XMLFormat)
	if err != nil {
		return nil, err
	}
	return &Pairing{HostID: hostID, HostRecord: hostRecord, DeviceRecord: deviceRecord}, nil
}

const pairRetryInterval = time.Second

// pymobiledevice3 sends Pair again every second while the device answers
// PairingDialogResponsePending, and takes the error PasswordProtected of a
// lockdown request as a passcode that is required (pymobiledevice3 9537c0a,
// pymobiledevice3/lockdown.py:943-950, 992-1004). go-ios keeps the EscrowBag
// of the answer in the host's record, with the device's WiFiAddress (go-ios
// 63c0d57, ios/pair.go:201-212, 241).
func Pair(ctx context.Context, dial Dialer, pairing *Pairing) ([]byte, error) {
	var record map[string]any
	if _, err := plist.Unmarshal(pairing.HostRecord, &record); err != nil {
		return nil, err
	}
	request := map[string]any{
		"Label":           progName,
		"ProtocolVersion": "2",
		"Request":         "Pair",
		"PairRecord": map[string]any{
			"DeviceCertificate": record["DeviceCertificate"],
			"HostCertificate":   record["HostCertificate"],
			"RootCertificate":   record["RootCertificate"],
			"HostID":            record["HostID"],
			"SystemBUID":        record["SystemBUID"],
		},
		"PairingOptions": map[string]any{"ExtendedPairingErrors": true},
	}
	conn, err := dial(ctx, Port)
	if err != nil {
		return nil, E.Cause(err, "connect to lockdown")
	}
	defer conn.Close()
	lockdown := rsd.NewPlistConn(conn)
	for {
		reply, err := lockdown.Request(ctx, request)
		if err != nil {
			return nil, E.Cause(err, "lockdown Pair")
		}
		switch reply["Error"] {
		case nil:
			record["EscrowBag"], _ = reply["EscrowBag"].([]byte)
			if address, err := GetValue(ctx, dial, "WiFiAddress"); err == nil {
				record["WiFiMACAddress"] = address
			}
			return plist.Marshal(record, plist.XMLFormat)
		case "PairingDialogResponsePending", "PasswordProtected":
		default:
			return nil, E.New("lockdown Pair: ", fmt.Sprint(reply["Error"]))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pairRetryInterval):
		}
	}
}

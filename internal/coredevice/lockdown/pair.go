package lockdown

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	E "github.com/sagernet/sing/common/exceptions"

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

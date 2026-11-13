package misagent

import (
	"encoding/asn1"
	"net"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/rsd"

	"howett.net/plist"
)

const Service = "com.apple.misagent"

type Profile struct {
	Name           string
	UUID           string
	TeamIdentifier []string
	CreationDate   time.Time
	ExpirationDate time.Time
}

// CopyAll answers with each installed profile, signed, in Payload, and Status
// 0 (go-ios 63c0d57, ios/misagent/misagent.go:34-69).
func Profiles(conn net.Conn) ([]Profile, error) {
	plistConn := rsd.NewPlistConn(conn)
	err := plistConn.Send(map[string]any{"MessageType": "CopyAll", "ProfileType": "Provisioning"})
	if err != nil {
		return nil, E.Cause(err, "misagent: list profiles")
	}
	reply, err := plistConn.Receive()
	if err != nil {
		return nil, E.Cause(err, "misagent: list profiles")
	}
	if status, _ := reply["Status"].(uint64); status != 0 {
		return nil, E.New("misagent: list profiles: status ", status)
	}
	payload, _ := reply["Payload"].([]any)
	profiles := make([]Profile, 0, len(payload))
	for _, item := range payload {
		signed, _ := item.([]byte)
		content, err := signedContent(signed)
		if err != nil {
			return nil, E.Cause(err, "misagent: read a profile")
		}
		var profile Profile
		if _, err = plist.Unmarshal(content, &profile); err != nil {
			return nil, E.Cause(err, "misagent: read a profile")
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func signedContent(signed []byte) ([]byte, error) {
	var info struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(signed, &info); err != nil {
		return nil, err
	}
	var data struct {
		Version          int
		DigestAlgorithms asn1.RawValue
		Encapsulated     struct {
			Type    asn1.ObjectIdentifier
			Content []byte `asn1:"explicit,tag:0"`
		}
		Certificates asn1.RawValue `asn1:"optional,tag:0"`
		CRLs         asn1.RawValue `asn1:"optional,tag:1"`
		SignerInfos  asn1.RawValue
	}
	if _, err := asn1.Unmarshal(info.Content.Bytes, &data); err != nil {
		return nil, err
	}
	if len(data.Encapsulated.Content) == 0 {
		return nil, E.New("no signed content")
	}
	return data.Encapsulated.Content, nil
}

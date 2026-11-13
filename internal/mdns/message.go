package mdns

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"strings"

	"github.com/sagernet/sing/common"

	"golang.org/x/net/dns/dnsmessage"
)

const cacheFlush = 1 << 15

var root = dnsmessage.MustNewName(".")

type message struct {
	header      dnsmessage.Header
	questions   []dnsmessage.Question
	answers     []dnsmessage.Resource
	authorities []dnsmessage.Resource
	additionals []dnsmessage.Resource
}

func parse(data []byte) (message, bool) {
	var parser dnsmessage.Parser
	header, err := parser.Start(data)
	if err != nil {
		return message{}, false
	}
	questions, err := parser.AllQuestions()
	if err != nil {
		return message{}, false
	}
	parsed := message{header: header, questions: questions}
	for _, section := range []struct {
		next    func() (dnsmessage.Resource, error)
		records *[]dnsmessage.Resource
	}{
		{parser.Answer, &parsed.answers},
		{parser.Authority, &parsed.authorities},
		{parser.Additional, &parsed.additionals},
	} {
		for {
			record, err := section.next()
			if errors.Is(err, dnsmessage.ErrSectionDone) {
				break
			}
			if err != nil {
				return parsed, true
			}
			*section.records = append(*section.records, record)
		}
	}
	return parsed, true
}

func (m message) records() []dnsmessage.Resource {
	return slices.Concat(m.answers, m.authorities, m.additionals)
}

func sameName(a dnsmessage.Name, b dnsmessage.Name) bool {
	return strings.EqualFold(a.String(), b.String())
}

func canonical(record dnsmessage.Resource) []byte {
	const rdataOffset = 12 + 1 + 10
	key := binary.BigEndian.AppendUint16(nil, uint16(record.Header.Class&^cacheFlush))
	key = binary.BigEndian.AppendUint16(key, uint16(record.Header.Type))
	packed, err := (&dnsmessage.Message{Answers: []dnsmessage.Resource{{
		Header: dnsmessage.ResourceHeader{Name: root, Class: dnsmessage.ClassINET},
		Body:   record.Body,
	}}}).Pack()
	if err != nil {
		return key
	}
	return append(key, packed[rdataOffset:]...)
}

func sameRecord(a dnsmessage.Resource, b dnsmessage.Resource) bool {
	return sameName(a.Header.Name, b.Header.Name) && bytes.Equal(canonical(a), canonical(b))
}

func compareRecords(a []dnsmessage.Resource, b []dnsmessage.Resource) int {
	keys := func(records []dnsmessage.Resource) [][]byte {
		sorted := common.Map(records, canonical)
		slices.SortFunc(sorted, bytes.Compare)
		return sorted
	}
	return slices.CompareFunc(keys(a), keys(b), bytes.Compare)
}

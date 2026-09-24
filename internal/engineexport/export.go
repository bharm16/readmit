// Package engineexport reads a deliberately finite, source-model export subset.
// Engine/version declarations are not certification or proof of export origin.
package engineexport

import (
	"bytes"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

const Schema = "readmit-engine-export/v1"
const MaxBytes = 16 << 20

var ErrUnsupported = errors.New("unsupported engine export declaration or XML variant")

// Plan records the selected adapter. No engine identity is inferred from bytes.
type Plan struct {
	Schema     string `json:"schema"`
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	Format     string `json:"format"`
	Terminator string `json:"terminator"`
}

func (p Plan) Validate() error {
	if p.Schema != Schema || !(p.Engine == "oie" && p.Version == "4.6.0" || p.Engine == "mirth" && p.Version == "4.5.2") || (p.Format != "raw" && p.Format != "message-xml") || (p.Terminator != "cr" && p.Terminator != "lf" && p.Terminator != "crlf") {
		return ErrUnsupported
	}
	return nil
}
func Decode(data []byte) (Plan, error) {
	var p Plan
	if len(data) > 4096 || json.Unmarshal(data, &p, json.RejectUnknownMembers(true)) != nil {
		return p, ErrUnsupported
	}
	return p, p.Validate()
}

// Record identifies the enclosing message's exact byte range in the retained
// container. Payload is decoded XML character data, never claimed as wire bytes.
type Record struct {
	Offset      int    `json:"offset"`
	Size        int    `json:"size"`
	Stage       string `json:"stage"`
	Correlation string `json:"correlation"`
	Payload     []byte `json:"-"`
}

// Extract keeps raw fallback bytes unchanged. XML supports only explicit,
// unencrypted RAW HL7V2 content. Missing stage, encrypted content and other
// content variants are refused instead of guessed or silently dropped.
func Extract(p Plan, data []byte) ([]Record, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, errors.New("engine export is empty or exceeds 16 MiB")
	}
	if p.Format == "raw" {
		return []Record{{Offset: 0, Size: len(data), Stage: "unknown", Correlation: "unknown", Payload: bytes.Clone(data)}}, nil
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	records := []Record{}
	nodes := 0
	for {
		start := int(d.InputOffset())
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrUnsupported
		}
		switch t := token.(type) {
		case xml.CharData:
			if len(bytes.TrimSpace(t)) != 0 {
				return nil, ErrUnsupported
			}
		case xml.StartElement:
			if t.Name.Local != "message" {
				return nil, ErrUnsupported
			}
			n, err := readNode(d, t, start, "", 0, &nodes)
			if err != nil {
				return nil, err
			}
			got, err := messageRecords(n, data)
			if err != nil {
				return nil, err
			}
			records = append(records, got...)
			if len(records) > 128 {
				return nil, errors.New("engine export exceeds 128 content records")
			}
		default:
			return nil, ErrUnsupported
		}
	}
	if len(records) == 0 {
		return nil, ErrUnsupported
	}
	return records, nil
}

type node struct {
	name             string
	text             string
	children         []*node
	start, end, body int
}

func readNode(d *xml.Decoder, start xml.StartElement, offset int, parent string, depth int, nodes *int) (*node, error) {
	*nodes += 1
	if depth >= 32 || *nodes > 8192 || start.Name.Space != "" {
		return nil, ErrUnsupported
	}
	// These are the map labels the two tested exporters put on unselected
	// metadata. They are retained as bytes, never interpreted or instantiated.
	// All other classes, references and executable serialization hooks refuse.
	for _, a := range start.Attr {
		if a.Name.Space != "" || a.Name.Local != "class" ||
			!(start.Name.Local == "connectorMessages" && a.Value == "linked-hash-map" ||
				start.Name.Local == "content" && allowedMapContentClass(parent, a.Value)) {
			return nil, ErrUnsupported
		}
	}
	n := &node{name: start.Name.Local, start: offset, body: int(d.InputOffset())}
	var content strings.Builder
	for {
		at := int(d.InputOffset())
		token, err := d.Token()
		if err != nil {
			return nil, ErrUnsupported
		}
		switch t := token.(type) {
		case xml.StartElement:
			child, err := readNode(d, t, at, start.Name.Local, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, child)
		case xml.EndElement:
			n.end = int(d.InputOffset())
			n.text = content.String()
			return n, nil
		case xml.CharData:
			content.Write(t)
		default:
			return nil, ErrUnsupported
		}
	}
}

func allowedMapContentClass(parent, class string) bool {
	switch parent {
	case "sourceMapContent":
		return class == "java.util.Collections$UnmodifiableMap"
	case "connectorMapContent", "channelMapContent", "responseMapContent":
		return class == "map"
	}
	return false
}
func (n *node) child(name string) (*node, error) {
	var found *node
	for _, c := range n.children {
		if c.name == name {
			if found != nil {
				return nil, ErrUnsupported
			}
			found = c
		}
	}
	if found == nil {
		return nil, ErrUnsupported
	}
	return found, nil
}
func (n *node) value(name string) (string, error) {
	c, e := n.child(name)
	if e != nil || len(c.children) != 0 {
		return "", ErrUnsupported
	}
	return c.text, nil
}
func messageRecords(n *node, data []byte) ([]Record, error) {
	for _, c := range n.children {
		if c.name == "attachments" && (len(c.children) > 0 || strings.TrimSpace(c.text) != "") {
			return nil, ErrUnsupported
		}
	}
	connectors, err := n.child("connectorMessages")
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, entry := range connectors.children {
		if entry.name != "entry" || len(entry.children) != 2 {
			return nil, ErrUnsupported
		}
		id, err := entry.value("int")
		if err != nil || id != "0" {
			return nil, ErrUnsupported
		}
		c, err := entry.child("connectorMessage")
		if err != nil {
			return nil, err
		}
		if v, e := c.value("metaDataId"); e != nil || v != "0" {
			return nil, ErrUnsupported
		}
		seenStage := map[string]bool{}
		for _, child := range c.children {
			switch child.name {
			case "processedRaw", "encoded":
				// Both engines retain these unselected source stages in their
				// ordinary whole-message export. They are never extracted as
				// evidence; accept only their explicit unencrypted HL7V2 form.
				contentType := "PROCESSED_RAW"
				if child.name == "encoded" {
					contentType = "ENCODED"
				}
				if seenStage[child.name] {
					return nil, ErrUnsupported
				}
				seenStage[child.name] = true
				for _, pair := range [][2]string{{"contentType", contentType}, {"dataType", "HL7V2"}, {"encrypted", "false"}} {
					if value, err := child.value(pair[0]); err != nil || value != pair[1] {
						return nil, ErrUnsupported
					}
				}
			case "transformed", "sent", "response", "responseTransformed", "processedResponse":
				return nil, ErrUnsupported
			}
		}
		raw, err := c.child("raw")
		if err != nil {
			return nil, err
		}
		for _, pair := range [][2]string{{"contentType", "RAW"}, {"dataType", "HL7V2"}, {"encrypted", "false"}} {
			if v, e := raw.value(pair[0]); e != nil || v != pair[1] {
				return nil, ErrUnsupported
			}
		}
		payload, err := raw.child("content")
		if err != nil || len(payload.children) != 0 || payload.text == "" {
			return nil, ErrUnsupported
		}
		// XML normalizes literal CRs. Refuse those instead of changing payload bytes;
		// an exporter may preserve a CR using &#13;, whose decoded value is exact.
		if bytes.ContainsRune(data[payload.body:payload.end], '\r') {
			return nil, ErrUnsupported
		}
		result = append(result, Record{Offset: n.start, Size: n.end - n.start, Stage: "raw", Correlation: "unknown", Payload: []byte(payload.text)})
	}
	if len(result) != 1 || strings.TrimSpace(connectors.text) != "" {
		return nil, ErrUnsupported
	}
	return result, nil
}

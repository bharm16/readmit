package fhirr4

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"io"
	"mime"
	"net/url"
	"strconv"
	"strings"
)

func validateContext(c Context) error {
	media, params, err := mime.ParseMediaType(c.MediaType)
	if err != nil || media != "application/fhir+json" {
		return invalid
	}
	for key, value := range params {
		if key == "charset" && strings.EqualFold(value, "utf-8") {
			continue
		}
		if key == "fhirversion" && value == "4.0" {
			continue
		}
		return invalid
	}
	if c.Base != "" {
		u, err := url.Parse(c.Base)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.Base) > 4096 {
			return invalid
		}
	}
	return nil
}
func pointer(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func parse(ctx context.Context, raw []byte) (*node, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	count := 0
	pointerBytes := 0
	var read func(int, string) (*node, error)
	read = func(depth int, path string) (*node, error) {
		count++
		pointerBytes += len(path)
		if len(path) > 4096 || pointerBytes > 8<<20 || count > MaxNodes || depth > MaxDepth || ctx.Err() != nil {
			return nil, invalid
		}
		start := int(decoder.InputOffset())
		for start < len(raw) && strings.ContainsRune(" \t\r\n,:", rune(raw[start])) {
			start++
		}
		token, err := decoder.ReadToken()
		if err != nil {
			return nil, invalid
		}
		n := &node{kind: byte(token.Kind()), text: token.String(), start: start, pointer: path}
		switch n.kind {
		case '{':
			for decoder.PeekKind() != '}' {
				key, err := decoder.ReadToken()
				if err != nil || key.Kind() != '"' || len(key.String()) > 1024 {
					return nil, invalid
				}
				k := key.String()
				child, err := read(depth+1, pointer(path, k))
				if err != nil {
					return nil, err
				}
				n.members = append(n.members, member{k, child})
			}
			if _, err := decoder.ReadToken(); err != nil {
				return nil, invalid
			}
		case '[':
			for decoder.PeekKind() != ']' {
				child, err := read(depth+1, pointer(path, strconv.Itoa(len(n.items))))
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, child)
			}
			if _, err := decoder.ReadToken(); err != nil {
				return nil, invalid
			}
		case '"', '0', 't', 'f', 'n':
			if len(n.text) > 1<<20 {
				return nil, invalid
			}
		default:
			return nil, invalid
		}
		n.end = int(decoder.InputOffset())
		return n, nil
	}
	n, err := read(0, "")
	if err != nil {
		return nil, err
	}
	if _, err := decoder.ReadToken(); err != io.EOF {
		return nil, invalid
	}
	return n, nil
}

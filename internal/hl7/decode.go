package hl7

import (
	"bytes"
	"encoding/hex"
	"errors"
)

// decode resolves the standard separator escapes F/S/R/T/E and X hexadecimal
// bytes. It does not transcode character sets, interpret local/formatting escapes,
// or normalize text. Unsupported escapes return a fixed error without raw values.
// Read is the one caller: it inspects the selected State first, reads MSH-1 and
// MSH-2 literally, and applies the character-set policy afterwards.
func decode(raw []byte, delimiters Delimiters) ([]byte, error) {
	if delimiters.Escape == 0 {
		return bytes.Clone(raw), nil
	}
	result := make([]byte, 0, len(raw))
	for start := 0; start < len(raw); {
		if raw[start] != delimiters.Escape {
			result = append(result, raw[start])
			start++
			continue
		}
		length := bytes.IndexByte(raw[start+1:], delimiters.Escape)
		if length < 0 {
			return nil, errors.New("unterminated HL7 escape")
		}
		end := start + 1 + length
		escape := raw[start+1 : end]
		var separator byte
		if len(escape) == 1 {
			switch escape[0] {
			case 'F':
				separator = delimiters.Field
			case 'S':
				separator = delimiters.Component
			case 'R':
				separator = delimiters.Repetition
			case 'T':
				separator = delimiters.Subcomponent
			case 'E':
				separator = delimiters.Escape
			}
		}
		if separator != 0 {
			result = append(result, separator)
		} else if len(escape) > 1 && escape[0] == 'X' {
			decoded := make([]byte, hex.DecodedLen(len(escape)-1))
			n, err := hex.Decode(decoded, escape[1:])
			if err != nil {
				return nil, errors.New("invalid HL7 hexadecimal escape")
			}
			result = append(result, decoded[:n]...)
		} else {
			return nil, errors.New("unsupported HL7 escape")
		}
		start = end + 1
	}
	return result, nil
}

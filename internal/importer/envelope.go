package importer

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrDeclaredEnvelope reports a member whose structure contradicts the recipe:
// a CSV header row that is not the declared shape or does not hold a declared
// column, or a JSON or XML document that does not hold the declared record
// path. It is the envelope's form of the framing refusal an import plan makes,
// and like that one it is corrected by declaring what the member really is.
var ErrDeclaredEnvelope = errors.New("the member's structure contradicts the declared envelope")

// maxDocumentDepth bounds the element nesting one XML member may reach. It sits
// far above the locator and record-path bounds, so it stops a crafted document
// from growing the walker's stack without limiting a real envelope.
const maxDocumentDepth = 64

// envelopeRecord is one record of an envelope member: the byte range the record
// occupies in the member, and either the values the recipe's locators name or
// the one reason the record could not be read into them. A record is never
// half-read: when reason is set, values is nil and the record's own bytes are
// retained as quarantined evidence exactly as they were found.
type envelopeRecord struct {
	offset int
	size   int
	values map[string][]byte
	reason string
}

// locatorKey identifies one locator among the at most five a recipe declares.
// Locator elements are printable labels, so a NUL can never appear inside one
// and joining on it cannot make two different locators collide.
func locatorKey(locator Locator) string { return strings.Join(locator, "\x00") }

func (r envelopeRecord) value(locator Locator) ([]byte, bool) {
	value, ok := r.values[locatorKey(locator)]
	return value, ok
}

// envelopeRecords divides one member into the records the recipe's envelope
// holds, resolving exactly the locators the recipe declares. A structural
// disagreement between the recipe and the member is refused; a record whose own
// bytes cannot be read is returned with a reason and retained by the caller.
func envelopeRecords(recipe Recipe, locators []Locator, data []byte) ([]envelopeRecord, error) {
	var records []envelopeRecord
	var err error
	switch recipe.Envelope {
	case CSVEnvelope:
		records, err = csvRecords(*recipe.CSV, locators, data)
	case TextEnvelope:
		records, err = textRecords(*recipe.Text, locators, data)
	case JSONEnvelope:
		records, err = jsonRecords(*recipe.JSON, locators, data)
	case XMLEnvelope:
		records, err = xmlRecords(*recipe.XML, locators, data)
	default:
		return nil, errors.New("envelope must be declared as csv, json, xml, or text")
	}
	if err != nil {
		return nil, err
	}
	if len(records) > MaxEnvelopeRecords {
		return nil, errors.New("a member divides into more envelope records than one import writes")
	}
	return records, nil
}

// remainder retains every byte of a member that no longer divides into
// records. It produces nothing when there are no such bytes, because an empty
// record would be a source holding no evidence.
func remainder(data []byte, at int) []envelopeRecord {
	if at = min(max(at, 0), len(data)); at == len(data) {
		return nil
	}
	return []envelopeRecord{{offset: at, size: len(data) - at, reason: ReasonRemainder}}
}

func separatorBytes(declared Separator) []byte {
	if declared == CRLFSeparator {
		return []byte{'\r', '\n'}
	}
	return []byte{'\n'}
}

// recordEnd returns where the record beginning at start ends: the offset of its
// last byte of content, and the offset the next record begins at. A member that
// does not end with a separator still ends a record.
func recordEnd(data []byte, start int, separator []byte) (int, int) {
	next := bytes.Index(data[start:], separator)
	if next < 0 {
		return len(data), len(data)
	}
	return start + next, start + next + len(separator)
}

// csvRecords divides one member under the declared CSV dialect. Nothing is
// normalized: a field's bytes are the member's own bytes with only the
// dialect's doubled quote undone, and a line ending inside a quoted field is
// kept exactly as it was written rather than rewritten to one form.
func csvRecords(dialect CSVDialect, locators []Locator, data []byte) ([]envelopeRecord, error) {
	separator := separatorBytes(dialect.RecordSeparator)
	delimiter := dialect.Delimiter[0]
	at := 0
	columns := map[string]int{}
	if dialect.Header == HeaderPresent {
		names, _, end, ok := csvRecord(data, at, delimiter, separator)
		if !ok || len(names) != dialect.Fields {
			return nil, ErrDeclaredEnvelope
		}
		for index, name := range names {
			text := string(name)
			if _, repeated := columns[text]; repeated {
				return nil, ErrDeclaredEnvelope
			}
			columns[text] = index
		}
		at = end
	}
	// Every declared column is resolved against the header once, before any
	// record is read: a locator naming a column the member does not have is a
	// disagreement about the member, not a property of one of its rows.
	indexes := make([]int, len(locators))
	for i, locator := range locators {
		var index int
		var named bool
		if dialect.Header == HeaderPresent {
			index, named = columns[locator[0]]
		} else {
			index, named = flatColumn(locator, dialect.Fields)
		}
		if !named {
			return nil, ErrDeclaredEnvelope
		}
		indexes[i] = index
	}
	records := []envelopeRecord{}
	for at < len(data) && len(records) <= MaxEnvelopeRecords {
		fields, content, end, ok := csvRecord(data, at, delimiter, separator)
		record := envelopeRecord{offset: at, size: content - at}
		switch {
		case !ok:
			record.reason = ReasonRecord
		case len(fields) != dialect.Fields:
			record.reason = ReasonFields
		default:
			record.values = make(map[string][]byte, len(locators))
			for i, locator := range locators {
				record.values[locatorKey(locator)] = fields[indexes[i]]
			}
		}
		records = append(records, record)
		at = end
	}
	return records, nil
}

// csvRecord reads one record. It reports the fields, the offset its own bytes
// end at, the offset the next record begins at, and whether it is well formed.
// A malformed record still has a boundary, so the bytes stay accounted for.
func csvRecord(data []byte, start int, delimiter byte, separator []byte) ([][]byte, int, int, bool) {
	fields := [][]byte{}
	at := start
	for {
		field, next, ok := csvField(data, at, delimiter, separator)
		if !ok {
			content, end := recordEnd(data, next, separator)
			return nil, content, end, false
		}
		fields = append(fields, field)
		if len(fields) > MaxEnvelopeFields {
			content, end := recordEnd(data, next, separator)
			return nil, content, end, false
		}
		at = next
		if at >= len(data) {
			return fields, at, at, true
		}
		if bytes.HasPrefix(data[at:], separator) {
			return fields, at, at + len(separator), true
		}
		at++
	}
}

// csvField reads one field and reports the offset just past it, which is a
// delimiter, the start of a record separator, or the end of the member. A field
// that cannot be read reports the offset to resynchronize the record from,
// which is never before the offset it started at.
func csvField(data []byte, start int, delimiter byte, separator []byte) ([]byte, int, bool) {
	if start < len(data) && data[start] == '"' {
		at := start + 1
		var value []byte
		for {
			quote := bytes.IndexByte(data[at:], '"')
			if quote < 0 {
				// An unterminated quote leaves no boundary at all: the rest of
				// the member belongs to this record and is retained with it.
				return nil, len(data), false
			}
			value = append(value, data[at:at+quote]...)
			at += quote + 1
			if at < len(data) && data[at] == '"' {
				value = append(value, '"')
				at++
				continue
			}
			break
		}
		if at < len(data) && data[at] != delimiter && !bytes.HasPrefix(data[at:], separator) {
			return nil, at, false
		}
		return value, at, true
	}
	at := start
	for at < len(data) && data[at] != delimiter && !bytes.HasPrefix(data[at:], separator) {
		if data[at] == '"' {
			return nil, at, false
		}
		at++
	}
	return data[start:at], at, true
}

// textRecords divides one member under the declared text-log dialect. There is
// no quoting and no escaping: the last declared field holds the remainder of
// the record, so a payload carrying the field separator is read whole.
func textRecords(dialect TextDialect, locators []Locator, data []byte) ([]envelopeRecord, error) {
	separator := separatorBytes(dialect.RecordSeparator)
	field := dialect.FieldSeparator[0]
	indexes := make([]int, len(locators))
	for i, locator := range locators {
		index, ok := flatColumn(locator, dialect.Fields)
		if !ok {
			return nil, ErrDeclaredEnvelope
		}
		indexes[i] = index
	}
	records := []envelopeRecord{}
	at := 0
	for at < len(data) && len(records) <= MaxEnvelopeRecords {
		content, end := recordEnd(data, at, separator)
		record := envelopeRecord{offset: at, size: content - at}
		fields := textFields(data[at:content], field, dialect.Fields)
		if len(fields) != dialect.Fields {
			record.reason = ReasonFields
		} else {
			record.values = make(map[string][]byte, len(locators))
			for i, locator := range locators {
				record.values[locatorKey(locator)] = fields[indexes[i]]
			}
		}
		records = append(records, record)
		at = end
	}
	return records, nil
}

// flatColumn reads a locator that names a column by its one-based index, which
// is how a record with no declared header names its fields.
func flatColumn(locator Locator, fields int) (int, bool) {
	column, err := strconv.Atoi(locator[0])
	if err != nil || column < 1 || column > fields {
		return 0, false
	}
	return column - 1, true
}

// textFields divides one record at the first count-1 separators. It returns
// fewer fields than asked for when the record holds fewer separators, which is
// how a record that is not the declared shape reports itself.
func textFields(record []byte, separator byte, count int) [][]byte {
	fields := make([][]byte, 0, count)
	at := 0
	for len(fields) < count-1 {
		next := bytes.IndexByte(record[at:], separator)
		if next < 0 {
			break
		}
		fields = append(fields, record[at:at+next])
		at += next + 1
	}
	return append(fields, record[at:])
}

// jsonRecords divides one member under the declared JSON dialect. The decoder
// is the strict one the rest of readmit uses: invalid UTF-8, an unescaped
// control character and a lone surrogate are all errors, so an accepted value
// is exactly the bytes the document represented and never a replacement for
// bytes it could not represent.
func jsonRecords(dialect DocumentDialect, locators []Locator, data []byte) ([]envelopeRecord, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	if !jsonDescend(decoder, dialect.RecordPath) {
		return nil, ErrDeclaredEnvelope
	}
	if open, err := decoder.ReadToken(); err != nil || open.Kind() != '[' {
		return nil, ErrDeclaredEnvelope
	}
	records := []envelopeRecord{}
	for len(records) <= MaxEnvelopeRecords {
		kind := decoder.PeekKind()
		if kind == ']' {
			return records, nil
		}
		value, err := decoder.ReadValue()
		after := int(decoder.InputOffset())
		if err != nil || after < len(value) || !bytes.Equal(data[after-len(value):after], value) {
			// The remaining bytes no longer divide into records. They are one
			// retained record rather than a silent end of the member.
			return append(records, remainder(data, after)...), nil
		}
		record := envelopeRecord{offset: after - len(value), size: len(value)}
		if value.Kind() != '{' {
			record.reason = ReasonFields
		} else {
			record.values = make(map[string][]byte, len(locators))
			for _, locator := range locators {
				located, ok := jsonMember(value, locator)
				if !ok {
					record.values, record.reason = nil, ReasonMissing
					break
				}
				record.values[locatorKey(locator)] = located
			}
		}
		records = append(records, record)
	}
	return records, nil
}

// jsonDescend reads through a path of object member names so the decoder is
// left at the value the last name holds. An empty path leaves it where it was,
// which is how a record path names the document's own root.
func jsonDescend(decoder *jsontext.Decoder, path []string) bool {
	for _, name := range path {
		if open, err := decoder.ReadToken(); err != nil || open.Kind() != '{' {
			return false
		}
		found := false
		for decoder.PeekKind() != '}' {
			key, err := decoder.ReadToken()
			if err != nil || key.Kind() != '"' {
				return false
			}
			if key.String() == name {
				found = true
				break
			}
			if _, err := decoder.ReadValue(); err != nil {
				return false
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// jsonMember reads the value one locator names inside one record object. A
// string is unescaped by the strict decoder and a number is its own literal;
// every other kind, and an absent member, leaves the record unmapped.
func jsonMember(value jsontext.Value, locator Locator) ([]byte, bool) {
	decoder := jsontext.NewDecoder(bytes.NewReader(value))
	if !jsonDescend(decoder, locator) {
		return nil, false
	}
	leaf, err := decoder.ReadToken()
	if err != nil || leaf.Kind() != '"' && leaf.Kind() != '0' {
		return nil, false
	}
	return []byte(leaf.String()), true
}

// xmlRecords divides one member under the declared XML dialect. The decoder
// finds the records and the declared elements; it never supplies their bytes.
// XML normalizes line endings in character data, so a payload read through a
// decoder's own text would silently differ from the file. Every located value
// is therefore read from the member's own bytes by xmlContent, which refuses
// exactly the bytes normalization would have changed.
func xmlRecords(dialect DocumentDialect, locators []Locator, data []byte) ([]envelopeRecord, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	records := []envelopeRecord{}
	var path []string
	record := envelopeRecord{offset: -1}
	var spans map[string][2]int
	pending, pendingAt := "", 0
	for len(records) <= MaxEnvelopeRecords {
		before := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(records) == 0 && record.offset < 0 {
				return nil, ErrDeclaredEnvelope
			}
			// A record the decoder stopped inside is retained from its own
			// first byte, so the bytes already read stay accounted for.
			at := before
			if record.offset >= 0 {
				at = record.offset
			}
			return append(records, remainder(data, at)...), nil
		}
		after := int(decoder.InputOffset())
		switch element := token.(type) {
		case xml.StartElement:
			if len(path) >= maxDocumentDepth {
				return nil, ErrDeclaredEnvelope
			}
			path = append(path, element.Name.Local)
			switch {
			case record.offset < 0 && slices.Equal(path, dialect.RecordPath):
				record, spans = envelopeRecord{offset: before}, map[string][2]int{}
			case record.offset >= 0 && pending == "":
				key := locatorKey(path[len(dialect.RecordPath):])
				if _, taken := spans[key]; !taken && slices.ContainsFunc(locators, func(l Locator) bool { return locatorKey(l) == key }) {
					pending, pendingAt = key, after
				}
			}
		case xml.EndElement:
			if pending != "" && pending == locatorKey(path[len(dialect.RecordPath):]) {
				spans[pending] = [2]int{pendingAt, min(before, len(data))}
				pending = ""
			}
			if record.offset >= 0 && slices.Equal(path, dialect.RecordPath) {
				record.size = after - record.offset
				records = append(records, xmlRecordValues(data, record, spans, locators))
				record, spans = envelopeRecord{offset: -1}, nil
			}
			path = path[:len(path)-1]
		}
	}
	if len(records) == 0 {
		return nil, ErrDeclaredEnvelope
	}
	return records, nil
}

// xmlRecordValues reads every located span out of the member's own bytes.
func xmlRecordValues(data []byte, record envelopeRecord, spans map[string][2]int, locators []Locator) envelopeRecord {
	record.values = make(map[string][]byte, len(locators))
	for _, locator := range locators {
		span, located := spans[locatorKey(locator)]
		if !located || span[1] < span[0] {
			record.values, record.reason = nil, ReasonMissing
			return record
		}
		value, err := xmlContent(data[span[0]:span[1]])
		if err != nil {
			record.values, record.reason = nil, ReasonContent
			return record
		}
		record.values[locatorKey(locator)] = value
	}
	return record
}

// xmlContent reads one element's raw character data into the bytes it stands
// for. It resolves the five predefined entities, character references, and
// CDATA sections, and refuses everything else: an unknown entity, a nested
// element, and above all a raw carriage return, which XML line-ending
// normalization does not preserve. A conforming reader and readmit would
// disagree about those bytes, so readmit refuses rather than pick one reading;
// a payload that must carry them declares a character reference or base64.
func xmlContent(raw []byte) ([]byte, error) {
	value := make([]byte, 0, len(raw))
	for at := 0; at < len(raw); {
		switch {
		case raw[at] == '\r':
			return nil, errors.New("raw carriage return in XML character data")
		case bytes.HasPrefix(raw[at:], []byte("<![CDATA[")):
			end := bytes.Index(raw[at+9:], []byte("]]>"))
			if end < 0 {
				return nil, errors.New("unterminated CDATA section")
			}
			section := raw[at+9 : at+9+end]
			if bytes.IndexByte(section, '\r') >= 0 {
				return nil, errors.New("raw carriage return in a CDATA section")
			}
			value = append(value, section...)
			at += 9 + end + 3
		case raw[at] == '<':
			return nil, errors.New("a mapped XML element holds child elements")
		case raw[at] == '&':
			end := bytes.IndexByte(raw[at:], ';')
			if end < 1 || end > 12 {
				return nil, errors.New("unterminated XML entity reference")
			}
			decoded, err := xmlReference(raw[at+1 : at+end])
			if err != nil {
				return nil, err
			}
			value = append(value, decoded...)
			at += end + 1
		default:
			value = append(value, raw[at])
			at++
		}
	}
	return value, nil
}

// xmlReference resolves one entity or character reference. Only the five
// entities XML predefines are known: a document type declaring its own is not
// read, so a reference readmit cannot resolve is an error rather than a guess.
func xmlReference(name []byte) ([]byte, error) {
	switch string(name) {
	case "amp":
		return []byte{'&'}, nil
	case "lt":
		return []byte{'<'}, nil
	case "gt":
		return []byte{'>'}, nil
	case "quot":
		return []byte{'"'}, nil
	case "apos":
		return []byte{'\''}, nil
	}
	if len(name) < 2 || name[0] != '#' {
		return nil, errors.New("unknown XML entity reference")
	}
	digits, base := string(name[1:]), 10
	if name[1] == 'x' || name[1] == 'X' {
		digits, base = string(name[2:]), 16
	}
	point, err := strconv.ParseUint(digits, base, 32)
	if err != nil || !xmlCharacter(rune(point)) {
		return nil, errors.New("XML character reference outside the characters XML allows")
	}
	return utf8.AppendRune(nil, rune(point)), nil
}

// xmlCharacter is XML 1.0's own character range. A reference outside it cannot
// stand for a byte sequence any conforming document could hold.
func xmlCharacter(point rune) bool {
	switch {
	case point == 0x09 || point == 0x0a || point == 0x0d:
		return true
	case point >= 0x20 && point <= 0xd7ff:
		return true
	case point >= 0xe000 && point <= 0xfffd:
		return true
	case point >= 0x10000 && point <= 0x10ffff:
		return true
	default:
		return false
	}
}

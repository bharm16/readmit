package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// A probe reads only the first ProbeHeadBytes of each member and proposes how
// the members could be declared. It declares nothing itself: framing,
// encoding and envelopes stay declarations, never detections. A proposal is
// made only when every member agrees on one reading; whatever is chosen is
// then declared in full, and the receipt records that declaration verbatim,
// exactly as if a person had written it. An engine export is never proposed:
// which engine wrote an XML document cannot be read from its bytes.

// ProbeHeadBytes bounds how much of one member a probe reads.
const ProbeHeadBytes = 64 << 10

// maxProbePaths bounds the key paths a sample lists.
const maxProbePaths = 256

// The shapes a member's first bytes can show.
const (
	shapeMLLP      = "mllp"
	shapeBatch     = "hl7-batch"
	shapeHL7       = "hl7"
	shapeJSON      = "json"
	shapeXML       = "xml"
	shapeDelimited = "delimited"
	shapeText      = "text"
)

// ProbeInput is one member a probe read: the declared location it belongs to
// (Container, zero-based in files, folders, archives order), its name, the
// kind of that location, its size, and whether its first bytes read as a
// format this release imports; Reason says why not.
type ProbeInput struct {
	Container int    `json:"container"`
	Name      string `json:"name"`
	Kind      Kind   `json:"kind"`
	Size      int64  `json:"size"`
	Accepted  bool   `json:"accepted"`
	Reason    string `json:"reason,omitzero"`
	// Member is the row's path inside its folder or archive, empty for a
	// file and for a folder or archive that holds no member.
	Member string `json:"member,omitzero"`
}

// ProbeFormat is one way the accepted members could be declared. A plan
// format carries the whole plan when every declaration it needs was read
// unambiguously, and no plan otherwise; a recipe format names only its
// envelope, because where a record's message, time, source, direction and
// channel are is always the person's mapping.
type ProbeFormat struct {
	Mode     string   `json:"mode"`
	Label    string   `json:"label"`
	Plan     *Plan    `json:"plan,omitzero"`
	Envelope Envelope `json:"envelope,omitzero"`
}

// ProbeSample is the structure of the first envelope member, for a mapping's
// pickers: the field count and, when the first record reads as column names,
// those names; or the member-name and element paths of a JSON or XML
// document. It carries keys only, never a value.
type ProbeSample struct {
	Envelope Envelope   `json:"envelope"`
	Fields   int        `json:"fields,omitzero"`
	Columns  []string   `json:"columns"`
	Paths    [][]string `json:"paths"`
}

// Probe is what probing found. Selected is the index of the one format that
// fits, set only when exactly one does and it is fully declarable.
type Probe struct {
	Inputs   []ProbeInput  `json:"inputs"`
	Formats  []ProbeFormat `json:"formats"`
	Selected *int          `json:"selected"`
	Sample   *ProbeSample  `json:"sample"`
}

// probed is one member's head and what it showed.
type probed struct {
	input      int
	head       []byte
	whole      bool
	shape      string
	terminator hl7.Terminator
	encoding   Encoding
	headers    int
	suffix     string
	contained  bool
}

var errNotRegular = errors.New("not one regular file")

// ProbeInputs reads the first bytes of every member of the declared locations
// and proposes the formats that could read them. It writes nothing and reads
// no member past ProbeHeadBytes.
func ProbeInputs(ctx context.Context, files, folders, archives []string) (Probe, error) {
	result := Probe{Inputs: []ProbeInput{}, Formats: []ProbeFormat{}}
	if len(files)+len(folders)+len(archives) == 0 {
		return result, errors.New("choose at least one file, folder or ZIP archive")
	}
	if len(files)+len(folders)+len(archives) > MaxContainers {
		return result, errors.New("an import declares more locations than one case holds")
	}
	var members []probed
	add := func(container int, kind Kind, name string, size int64, head []byte, whole, contained bool, refused string) {
		input := ProbeInput{Container: container, Name: name, Kind: kind, Size: size}
		if contained {
			input.Member = name
		}
		index := len(result.Inputs)
		if refused != "" {
			input.Reason = refused
			result.Inputs = append(result.Inputs, input)
			return
		}
		member := classify(head, whole)
		member.input, member.contained = index, contained
		member.suffix = strings.ToLower(path.Ext(name))
		if member.shape == "" {
			input.Reason = "its first bytes are not HL7, JSON, XML or text"
		} else {
			input.Accepted = true
			members = append(members, member)
		}
		result.Inputs = append(result.Inputs, input)
	}
	container := 0
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		head, size, err := probeFile(file)
		refused := ""
		if err != nil {
			refused = "it cannot be read as one regular file"
		}
		add(container, FileContainer, filepath.Base(file), size, head, err == nil && size <= int64(len(head)), false, refused)
		container++
	}
	// A folder or archive that holds no member is still one row, so the
	// location a person chose is never missing from what they chose.
	empty := func(location string, kind Kind, before int) {
		if len(result.Inputs) == before {
			result.Inputs = append(result.Inputs, ProbeInput{Container: container, Name: filepath.Base(location), Kind: kind, Reason: "it holds no files"})
		}
	}
	for _, folder := range folders {
		before := len(result.Inputs)
		if err := probeFolder(ctx, folder, func(name string, size int64, head []byte, err error) {
			refused := ""
			if err != nil {
				refused = "it cannot be read as one regular file"
			}
			add(container, FolderContainer, name, size, head, err == nil && size <= int64(len(head)), true, refused)
		}); err != nil {
			return result, err
		}
		empty(folder, FolderContainer, before)
		container++
	}
	for _, archive := range archives {
		before := len(result.Inputs)
		if err := probeArchive(ctx, archive, func(name string, size int64, head []byte, err error) {
			refused := ""
			if err != nil {
				refused = "it cannot be read from the archive"
			}
			add(container, ArchiveContainer, name, size, head, err == nil && size <= int64(len(head)), true, refused)
		}); err != nil {
			return result, err
		}
		empty(archive, ArchiveContainer, before)
		container++
	}
	members = excludeBesideHL7(members, result.Inputs)
	result.Formats = formatsOf(members)
	if len(result.Formats) == 1 && (result.Formats[0].Plan != nil || result.Formats[0].Mode == "recipe") {
		selected := 0
		result.Selected = &selected
	}
	for _, member := range members {
		if envelope := envelopeOf(member.shape); envelope != "" {
			result.Sample = sampleOf(envelope, member.head)
			break
		}
	}
	return result, nil
}

// classify reads what a member's first bytes show.
func classify(head []byte, whole bool) probed {
	member := probed{head: head, whole: whole}
	if len(head) == 0 {
		return member
	}
	text := head
	if !whole {
		// A head cut inside a character is not evidence of another encoding.
		for cut := 0; cut < utf8.UTFMax && len(text) > 0 && !utf8.Valid(text); cut++ {
			text = text[:len(text)-1]
		}
	}
	if utf8.Valid(text) {
		member.encoding = UTF8
	}
	trimmed := bytes.TrimLeft(head, " \t\r\n")
	switch {
	case head[0] == 0x0b:
		member.shape = shapeMLLP
	case bytes.HasPrefix(head, []byte("FHS")) || bytes.HasPrefix(head, []byte("BHS")):
		member.shape = shapeBatch
	case bytes.HasPrefix(head, []byte("MSH")):
		member.shape = shapeHL7
	case member.encoding == "":
		return member
	case len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '['):
		member.shape = shapeJSON
	case len(trimmed) > 0 && trimmed[0] == '<':
		member.shape = shapeXML
	case delimited(text):
		member.shape = shapeDelimited
	case printable(text):
		member.shape = shapeText
	}
	if member.shape == shapeMLLP || member.shape == shapeBatch || member.shape == shapeHL7 {
		member.terminator = segmentTerminator(head)
		member.headers = len(headerStarts(head, bundle.MaxSources+1))
	}
	return member
}

// segmentTerminator is the one segment terminator a head uses, or empty when
// it uses more than one. A head with no terminator at all holds one segment,
// which any terminator divides the same way.
func segmentTerminator(head []byte) hl7.Terminator {
	crlf := bytes.Count(head, []byte("\r\n"))
	cr := bytes.Count(head, []byte("\r")) - crlf
	lf := bytes.Count(head, []byte("\n")) - crlf
	switch {
	case crlf == 0 && lf == 0:
		return hl7.CR
	case cr == 0 && lf == 0:
		return hl7.CRLF
	case cr == 0 && crlf == 0:
		return hl7.LF
	}
	return ""
}

// delimited reports text whose first lines split into the same number of
// comma-separated fields, more than one.
func delimited(text []byte) bool {
	lines := bytes.Split(bytes.TrimRight(text, "\r\n"), []byte("\n"))
	if len(lines) < 2 {
		return false
	}
	fields := bytes.Count(lines[0], []byte(",")) + 1
	if fields < 2 {
		return false
	}
	for _, line := range lines[1:min(len(lines), 8)] {
		if bytes.Count(bytes.TrimRight(line, "\r"), []byte(","))+1 != fields {
			return false
		}
	}
	return true
}

func printable(text []byte) bool {
	for _, r := range string(text) {
		if r < 0x20 && r != '\t' && r != '\r' && r != '\n' || r == 0x7f {
			return false
		}
	}
	return true
}

// excludeBesideHL7 drops, from members holding HL7, the folder and archive
// entries that are not HL7 and whose suffix no HL7 member has: a proposed
// plan's member suffixes exclude them, as an import records.
func excludeBesideHL7(members []probed, inputs []ProbeInput) []probed {
	suffixes := []string{}
	for _, member := range members {
		if hl7Shape(member.shape) {
			suffixes = append(suffixes, member.suffix)
		}
	}
	if len(suffixes) == 0 {
		return members
	}
	return slices.DeleteFunc(members, func(member probed) bool {
		if hl7Shape(member.shape) || !member.contained || member.suffix == "" || slices.Contains(suffixes, member.suffix) {
			return false
		}
		inputs[member.input].Accepted = false
		inputs[member.input].Reason = ReasonSuffix
		return true
	})
}

func hl7Shape(shape string) bool {
	return shape == shapeMLLP || shape == shapeBatch || shape == shapeHL7
}

func envelopeOf(shape string) Envelope {
	switch shape {
	case shapeJSON:
		return JSONEnvelope
	case shapeXML:
		return XMLEnvelope
	case shapeText:
		return TextEnvelope
	case shapeDelimited:
		return CSVEnvelope
	}
	return ""
}

// formatsOf proposes the formats that read every accepted member.
func formatsOf(members []probed) []ProbeFormat {
	formats := []ProbeFormat{}
	if len(members) == 0 {
		return formats
	}
	var hl7Members, envelopes []probed
	for _, member := range members {
		if hl7Shape(member.shape) {
			hl7Members = append(hl7Members, member)
		} else {
			envelopes = append(envelopes, member)
		}
	}
	if len(hl7Members) > 0 {
		formats = append(formats, planFormats(hl7Members)...)
	}
	shapes := map[string]bool{}
	for _, member := range envelopes {
		shapes[member.shape] = true
	}
	for _, shape := range []string{shapeJSON, shapeXML, shapeDelimited, shapeText} {
		if !shapes[shape] {
			continue
		}
		if shape == shapeDelimited {
			// Comma-separated lines are a CSV envelope or a text log with a
			// comma separator; which one is the person's declaration.
			formats = append(formats, ProbeFormat{Mode: "recipe", Label: "CSV", Envelope: CSVEnvelope},
				ProbeFormat{Mode: "recipe", Label: "Text", Envelope: TextEnvelope})
			continue
		}
		labels := map[string]string{shapeJSON: "JSON", shapeXML: "XML", shapeText: "Text"}
		formats = append(formats, ProbeFormat{Mode: "recipe", Label: labels[shape], Envelope: envelopeOf(shape)})
	}
	return formats
}

// planFormats proposes the import plans that read every HL7 member: one fully
// declared plan when they agree on framing, terminator and encoding, and one
// undeclared choice per reading otherwise.
func planFormats(members []probed) []ProbeFormat {
	type reading struct {
		framing    Framing
		boundary   Boundary
		terminator hl7.Terminator
		encoding   Encoding
	}
	var readings []reading
	suffixes := []string{}
	unsuffixed := false
	for _, member := range members {
		read := reading{terminator: member.terminator, encoding: member.encoding}
		switch {
		case member.shape == shapeMLLP:
			read.framing = MLLPFraming
		case member.shape == shapeBatch:
			read.framing, read.boundary = BatchFraming, HL7Batch
		case member.headers <= 1 && member.whole:
			read.framing = RawFraming
		default:
			read.framing, read.boundary = BatchFraming, SegmentStart
		}
		if !slices.Contains(readings, read) {
			readings = append(readings, read)
		}
		if member.contained {
			if member.suffix == "" {
				unsuffixed = true
			} else if !slices.Contains(suffixes, member.suffix) {
				suffixes = append(suffixes, member.suffix)
			}
		}
	}
	if unsuffixed {
		suffixes = []string{}
	}
	slices.Sort(suffixes)
	formats := []ProbeFormat{}
	for _, read := range readings {
		format := ProbeFormat{Mode: "plan", Label: planLabel(read.framing, read.boundary, read.terminator, read.encoding)}
		if len(readings) == 1 && read.terminator != "" && read.encoding != "" {
			format.Plan = &Plan{Schema: PlanSchema, Framing: read.framing, BatchBoundary: read.boundary, Terminator: read.terminator,
				Encoding: read.encoding, Direction: bundle.Unknown, Members: suffixes}
		}
		formats = append(formats, format)
	}
	return formats
}

func planLabel(framing Framing, boundary Boundary, terminator hl7.Terminator, encoding Encoding) string {
	label := "HL7"
	switch {
	case framing == MLLPFraming:
		label = "MLLP"
	case boundary == HL7Batch:
		label = "HL7 batch"
	case framing == BatchFraming:
		label = "HL7 messages"
	}
	if terminator != "" {
		label += " · " + strings.ToUpper(string(terminator))
	}
	if encoding != "" {
		label += " · " + strings.ToUpper(string(encoding))
	}
	return label
}

// sampleOf reads the keys of one envelope head.
func sampleOf(envelope Envelope, head []byte) *ProbeSample {
	sample := &ProbeSample{Envelope: envelope, Columns: []string{}, Paths: [][]string{}}
	switch envelope {
	case CSVEnvelope:
		first, _, _ := bytes.Cut(head, []byte("\n"))
		cells := strings.Split(strings.TrimRight(string(first), "\r"), ",")
		sample.Fields = len(cells)
		for _, cell := range cells {
			if !columnName(cell) {
				return sample
			}
		}
		sample.Columns = cells
	case JSONEnvelope:
		sample.Paths = jsonPaths(head)
	case XMLEnvelope:
		sample.Paths = xmlPaths(head)
	}
	return sample
}

// columnName reports a cell that reads as a column's name rather than a
// value: a short word of letters, digits, spaces, underscores and hyphens
// that begins with a letter.
func columnName(cell string) bool {
	if cell == "" || len(cell) > 64 {
		return false
	}
	for i, r := range cell {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '-' || r == ' '):
		default:
			return false
		}
	}
	return true
}

// jsonPaths lists the member-name paths of the objects a JSON head holds,
// arrays passed through, until the head ends.
func jsonPaths(head []byte) [][]string {
	paths := [][]string{}
	decoder := jsontext.NewDecoder(bytes.NewReader(head))
	type level struct {
		object bool
		name   string
		key    bool
	}
	stack := []level{}
	current := func() []string {
		names := []string{}
		for _, held := range stack {
			if held.object && held.name != "" {
				names = append(names, held.name)
			}
		}
		return names
	}
	for len(paths) < maxProbePaths {
		token, err := decoder.ReadToken()
		if err != nil {
			break
		}
		top := len(stack) - 1
		if top >= 0 && stack[top].object && stack[top].key {
			// This token is a member name.
			stack[top].name, stack[top].key = token.String(), false
			path := current()
			if len(path) <= MaxLocatorElements && !slices.ContainsFunc(paths, func(held []string) bool { return slices.Equal(held, path) }) {
				paths = append(paths, path)
			}
			continue
		}
		switch token.Kind() {
		case '{':
			stack = append(stack, level{object: true, key: true})
			continue
		case '[':
			stack = append(stack, level{})
			continue
		case '}', ']':
			if top >= 0 {
				stack = stack[:top]
			}
		}
		if top = len(stack) - 1; top >= 0 && stack[top].object {
			stack[top].key = true
		}
	}
	return paths
}

// xmlPaths lists the element paths an XML head holds until the head ends.
func xmlPaths(head []byte) [][]string {
	paths := [][]string{}
	decoder := xml.NewDecoder(bytes.NewReader(head))
	decoder.Strict = false
	stack := []string{}
	for len(paths) < maxProbePaths {
		token, err := decoder.RawToken()
		if err != nil {
			break
		}
		switch element := token.(type) {
		case xml.StartElement:
			stack = append(stack, element.Name.Local)
			path := slices.Clone(stack)
			if len(path) <= MaxLocatorElements && !slices.ContainsFunc(paths, func(held []string) bool { return slices.Equal(held, path) }) {
				paths = append(paths, path)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return paths
}

// probeFile reads the head of one declared file, following a link at its
// name as an import does, and refuses anything but a regular file before it
// is opened.
func probeFile(declared string) ([]byte, int64, error) {
	resolved, err := artifactpath.Resolve(declared)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, 0, errNotRegular
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	return readHead(file, info)
}

// readHead reads at most ProbeHeadBytes of an opened file that is still the
// regular file inspected.
func readHead(file *os.File, inspected fs.FileInfo) ([]byte, int64, error) {
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(inspected, opened) {
		return nil, 0, errNotRegular
	}
	head, err := io.ReadAll(io.LimitReader(file, ProbeHeadBytes))
	if err != nil {
		return nil, 0, err
	}
	return head, opened.Size(), nil
}

// probeFolder reads the head of every regular entry of a declared folder.
func probeFolder(ctx context.Context, declared string, visit func(name string, size int64, head []byte, err error)) error {
	root, err := artifactpath.Directory(declared)
	if err != nil {
		return errors.New("a declared import folder must be an existing directory that is not a symbolic link")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return errors.New("cannot open a declared import folder")
	}
	defer opened.Close()
	seen := 0
	return fs.WalkDir(opened.FS(), ".", func(name string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot read a declared import folder")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		if seen++; seen > MaxContainerEntries {
			return errors.New("a container holds more entries than one import reads")
		}
		if item.Type() != 0 {
			visit(name, 0, nil, errNotRegular)
			return nil
		}
		info, err := opened.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			visit(name, 0, nil, errNotRegular)
			return nil
		}
		file, err := opened.Open(name)
		if err != nil {
			visit(name, 0, nil, err)
			return nil
		}
		head, size, err := readHead(file, info)
		file.Close()
		visit(name, size, head, err)
		return nil
	})
}

// probeArchive reads the head of every file entry of a declared ZIP archive,
// refusing unsafe names as an import does.
func probeArchive(ctx context.Context, declared string, visit func(name string, size int64, head []byte, err error)) error {
	resolved, err := artifactpath.Resolve(declared)
	if err != nil {
		return errors.New("cannot resolve a declared import location")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("a declared import archive must be a regular file")
	}
	if info.Size() > MaxArchiveBytes {
		return errors.New("a declared import location exceeds its size limit")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return errors.New("cannot open a declared import location")
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return errors.New("cannot open a declared import location")
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return errors.New("cannot read a declared import archive")
	}
	if len(reader.File) > MaxContainerEntries {
		return errors.New("a container holds more entries than one import reads")
	}
	items := slices.Clone(reader.File)
	slices.SortFunc(items, func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) })
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := archiveName(item.Name)
		if err != nil {
			return err
		}
		if item.FileInfo().IsDir() {
			continue
		}
		if item.Mode()&fs.ModeType != 0 {
			visit(name, 0, nil, ErrUnsafeEntry)
			continue
		}
		member, err := item.Open()
		if err != nil {
			visit(name, int64(item.UncompressedSize64), nil, err)
			continue
		}
		head, err := io.ReadAll(io.LimitReader(member, ProbeHeadBytes))
		member.Close()
		visit(name, int64(item.UncompressedSize64), head, err)
	}
	return nil
}

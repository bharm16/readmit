package desktop

import (
	"fmt"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hl7reference"
)

// RawLine and RawToken are a bounded display projection, never evidence.
// Text is rendered from the same (possibly masked) bytes as ReadableWindow.
// Paths and half-open spans always originate in the original parser tree.
type RawLine struct {
	Number int        `json:"number"`
	Tokens []RawToken `json:"tokens"`
}

type RawToken struct {
	Text     string `json:"text"`
	Path     string `json:"path,omitzero"`
	Role     string `json:"role,omitzero"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Selected bool   `json:"selected,omitzero"`
	Empty    bool   `json:"empty,omitzero"`
}

type rawOwner struct{ path, role string }

func syntaxRole(catalog *hl7reference.Catalog, node hl7.Node) string {
	if node.Kind == "segment" {
		return "type"
	}
	if catalog == nil {
		return ""
	}
	answer := referenceFor(catalog, node)
	if answer.Record == nil || answer.Record.Datatype.State != "specified" {
		return ""
	}
	switch answer.Record.Datatype.Value {
	case "DT", "DTM", "TM", "TS":
		return "date"
	case "ID", "IS":
		return "code"
	case "NM", "SI", "SN":
		return "number"
	}
	return ""
}

func attachRawLines(window *RawWindow, raw []byte, doc *hl7.Document, message int, selected hl7.Node, catalog *hl7reference.Catalog) {
	if window == nil {
		return
	}
	owners := make([]rawOwner, window.End-window.Offset)
	put := func(from, to int, owner rawOwner) {
		for i := max(from, window.Offset); i < min(to, window.End); i++ {
			owners[i-window.Offset] = owner
		}
	}
	if doc != nil {
		nodes, err := doc.SourceNodes(message, hl7.Span{Start: window.Offset, End: window.End})
		if err == nil {
			delimiters := doc.Messages[message].Delimiters
			for _, node := range nodes {
				role := syntaxRole(catalog, node)
				if node.Kind == "segment" {
					put(node.Start, node.End, rawOwner{path: node.Path})
					put(node.Start, min(node.End, node.Start+3), rawOwner{node.Path, "type"})
					continue
				}
				put(node.Start, node.End, rawOwner{node.Path, role})
				// A separator selects the position it introduces. MSH-1 owns
				// its separator; MSH-2's encoding characters are one field.
				if !(node.Segment == "MSH" && node.Field <= 2) {
					separator := delimiters.Field
					switch node.Kind {
					case "repetition":
						separator = delimiters.Repetition
					case "component":
						separator = delimiters.Component
					case "subcomponent":
						separator = delimiters.Subcomponent
					}
					if node.Start > window.MessageStart && raw[node.Start-1] == separator {
						put(node.Start-1, node.Start, rawOwner{node.Path, "punctuation"})
					}
				} else {
					put(node.Start, node.End, rawOwner{node.Path, "punctuation"})
				}
			}
		}
	}
	lineNumber := 1
	for i := window.MessageStart; i < window.Offset; i++ {
		if raw[i] == '\r' || raw[i] == '\n' && (i == window.MessageStart || raw[i-1] != '\r') {
			lineNumber++
		}
	}
	line := RawLine{Number: lineNumber, Tokens: []RawToken{}}
	appendToken := func(token RawToken) {
		if len(line.Tokens) > 0 {
			last := &line.Tokens[len(line.Tokens)-1]
			if !token.Empty && !last.Empty && last.End == token.Start && last.Path == token.Path && last.Role == token.Role && last.Selected == token.Selected {
				last.Text += token.Text
				last.End = token.End
				return
			}
		}
		line.Tokens = append(line.Tokens, token)
	}
	empty := selected.State == hl7.Empty && selected.Path != "" && selected.Start == selected.End
	for i := window.Offset; i < window.End; i++ {
		if empty && i == selected.Start {
			appendToken(RawToken{Path: selected.Path, Start: i, End: i, Selected: true, Empty: true})
		}
		b := raw[i]
		if b == '\n' && i > 0 && raw[i-1] == '\r' {
			continue
		}
		if b == '\r' || b == '\n' {
			window.Lines = append(window.Lines, line)
			line = RawLine{Number: line.Number + 1, Tokens: []RawToken{}}
			continue
		}
		text := string(b)
		if (b < 32 && b != '\t') || b >= 127 {
			text = fmt.Sprintf(`\x%02x`, b)
		}
		owner := owners[i-window.Offset]
		marked := selected.Path != "" && selected.State != hl7.Omitted && i >= selected.Start && i < selected.End
		appendToken(RawToken{Text: text, Path: owner.path, Role: owner.role, Start: i, End: i + 1, Selected: marked})
	}
	if empty && selected.Start == window.End {
		appendToken(RawToken{Path: selected.Path, Start: window.End, End: window.End, Selected: true, Empty: true})
	}
	if len(line.Tokens) > 0 || len(window.Lines) == 0 {
		window.Lines = append(window.Lines, line)
	}
}

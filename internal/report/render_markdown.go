package report

import (
	"strings"
	"unicode"
)

// renderMarkdown writes the report as readable Markdown: headings,
// paragraphs, lists and pipe tables, every value escaped so no evidence
// becomes a link, an image, raw HTML, emphasis or a new table cell. Only a
// long raw value in the appendix is a fenced code block.
func renderMarkdown(doc *Document) []byte {
	var out strings.Builder
	for _, b := range documentLayout(doc) {
		switch b.kind {
		case titleBlock:
			out.WriteString("# " + markdownText(b.text) + "\n\n")
		case subtitleBlock, paragraphBlock:
			out.WriteString(markdownLine(b.text) + "\n\n")
		case headingBlock:
			out.WriteString("## " + markdownText(b.text) + "\n\n")
		case subheadingBlock:
			out.WriteString("### " + markdownText(b.text) + "\n\n")
		case listBlock:
			for _, item := range b.items {
				out.WriteString("- " + markdownText(item) + "\n")
			}
			out.WriteString("\n")
		case rawBlock:
			fence := strings.Repeat("`", max(3, longestRun(b.text, '`')+1))
			out.WriteString(fence + "text\n" + b.text + "\n" + fence + "\n\n")
		case tableBlock:
			header, rule := "|", "|"
			for _, c := range b.columns {
				header += " " + markdownText(c.header) + " |"
				rule += " --- |"
			}
			out.WriteString(header + "\n" + rule + "\n")
			for _, row := range b.rows {
				line := "|"
				for _, cell := range row {
					line += " " + markdownText(cell) + " |"
				}
				out.WriteString(line + "\n")
			}
			out.WriteString("\n")
		}
	}
	return []byte(out.String())
}

// markdownText escapes every character Markdown could read as syntax inside
// a line: emphasis, code, links and images, raw HTML and entities, table
// cells, strikethrough, and the colon and at sign an extended autolink needs.
func markdownText(text string) string {
	var out strings.Builder
	for _, r := range text {
		if strings.ContainsRune("\\`*_[]<>|~!&:@#{}()", r) {
			out.WriteRune('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// markdownLine is markdownText for a paragraph, which also escapes what
// would start a list, a quote, a heading or a thematic break at its start.
func markdownLine(text string) string {
	escaped := markdownText(text)
	if escaped == "" {
		return escaped
	}
	first := rune(escaped[0])
	if strings.ContainsRune("-+=>", first) {
		return "\\" + escaped
	}
	digits := strings.TrimLeftFunc(escaped, unicode.IsDigit)
	if len(digits) < len(escaped) && (strings.HasPrefix(digits, ".") || strings.HasPrefix(digits, ")")) {
		return escaped[:len(escaped)-len(digits)] + "\\" + digits
	}
	return escaped
}

func longestRun(text string, r rune) int {
	longest, run := 0, 0
	for _, c := range text {
		if c == r {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

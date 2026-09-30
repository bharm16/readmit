package report

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Paper is a PDF page size. Letter portrait is the default; A4 portrait is
// the explicit alternative.
type Paper string

const (
	Letter Paper = "letter"
	A4     Paper = "a4"
)

// The print geometry: 18 mm margins, an 11 pt body on a 14 pt line, 14 pt
// section headings and a 20 pt title, in the standard Helvetica faces and
// Courier for raw appendix values, all black or neutral gray.
const (
	pdfMargin      = 18 * 72 / 25.4
	bodySize       = 11.0
	bodyLeading    = 14.0
	sectionSize    = 14.0
	sectionLeading = 18.0
	titleSize      = 20.0
	titleLeading   = 26.0
	rawSize        = 9.0
	rawLeading     = 11.0
	footerSize     = 9.0
	cellPad        = 4.0
	rowPad         = 3.0
)

type pdfFont int

const (
	regular pdfFont = iota
	bold
	mono
)

func (f pdfFont) name() string { return [...]string{"/F1", "/F2", "/F3"}[f] }

// Helvetica and Helvetica-Bold advance widths for printable ASCII, from the
// standard font metrics, in thousandths of the font size.
var helvetica = [95]int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584}

var helveticaBold = [95]int{278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584}

// winAnsi are the characters beyond ASCII the layout itself uses, with
// their WinAnsiEncoding codes and Helvetica widths.
var winAnsi = map[rune]struct {
	code  byte
	width int
}{'·': {0xb7, 278}, '—': {0x97, 1000}, '–': {0x96, 556}}

func (f pdfFont) width(text string, size float64) float64 {
	total := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case f == mono:
			total += 600
		case c >= 0x80:
			for _, glyph := range winAnsi {
				if glyph.code == c {
					total += glyph.width
				}
			}
		case f == bold:
			total += helveticaBold[c-32]
		default:
			total += helvetica[c-32]
		}
	}
	return float64(total) * size / 1000
}

// pdfText is display text as the standard fonts can draw it, one byte per
// glyph: printable ASCII and the few WinAnsi characters the layout uses as
// themselves, and every other character as its code point escape, so
// nothing is dropped or replaced by a glyph the font lacks.
func pdfText(text string) string {
	var out strings.Builder
	for _, r := range text {
		if glyph, ok := winAnsi[r]; ok {
			out.WriteByte(glyph.code)
			continue
		}
		if r >= 0x20 && r < 0x7f {
			out.WriteRune(r)
			continue
		}
		out.WriteString(`\u{` + hexRune(r) + `}`)
	}
	return out.String()
}

// wrap breaks text into lines no wider than width, at spaces where it can
// and inside a word where it must; every character is kept.
func wrap(text string, f pdfFont, size, width float64) []string {
	text = pdfText(text)
	if text == "" {
		return []string{""}
	}
	lines := []string{}
	line := ""
	for _, word := range strings.SplitAfter(text, " ") {
		if f.width(line+strings.TrimRight(word, " "), size) <= width {
			line += word
			continue
		}
		if line != "" {
			lines = append(lines, strings.TrimRight(line, " "))
			line = ""
		}
		for f.width(strings.TrimRight(word, " "), size) > width {
			cut := 1
			for cut < len(word) && f.width(word[:cut+1], size) <= width {
				cut++
			}
			lines = append(lines, word[:cut])
			word = word[cut:]
		}
		line = word
	}
	return append(lines, strings.TrimRight(line, " "))
}

type pdfPage struct{ content strings.Builder }

type pdfWriter struct {
	width, height float64
	pages         []*pdfPage
	y             float64
}

func (w *pdfWriter) textWidth() float64 { return w.width - 2*pdfMargin }
func (w *pdfWriter) top() float64       { return w.height - pdfMargin }
func (w *pdfWriter) bottom() float64    { return pdfMargin }
func (w *pdfWriter) left() float64      { return pdfMargin }
func (w *pdfWriter) page() *pdfPage     { return w.pages[len(w.pages)-1] }
func (w *pdfWriter) room() float64      { return w.y - w.bottom() }

func (w *pdfWriter) newPage() {
	w.pages = append(w.pages, &pdfPage{})
	w.y = w.top()
}

// ensure starts a new page unless height fits on this one.
func (w *pdfWriter) ensure(height float64) {
	if height > w.room() && w.y < w.top() {
		w.newPage()
	}
}

func (w *pdfWriter) text(f pdfFont, size float64, x, baseline float64, gray float64, text string) {
	fmt.Fprintf(&w.page().content, "BT %s %s Tf %s g %s %s Td (%s) Tj ET\n", f.name(), num(size), num(gray), num(x), num(baseline), pdfString(text))
}

func (w *pdfWriter) rule(y, x0, x1, gray float64) {
	fmt.Fprintf(&w.page().content, "%s G 0.5 w %s %s m %s %s l S\n", num(gray), num(x0), num(y), num(x1), num(y))
}

// lines draws wrapped lines, continuing on a new page where they run out.
func (w *pdfWriter) lines(lines []string, f pdfFont, size, leading, x, gray float64) {
	for _, line := range lines {
		w.ensure(leading)
		w.text(f, size, x, w.y-size, gray, line)
		w.y -= leading
	}
}

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// pdfString is glyph bytes as a PDF literal string, in ASCII: the
// delimiters escaped and every byte past ASCII written in octal.
func pdfString(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\\' || c == '(' || c == ')':
			out.WriteByte('\\')
			out.WriteByte(c)
		case c >= 0x80:
			fmt.Fprintf(&out, "\\%03o", c)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

// minHeight is the least of a docBlock that must follow a heading on its page:
// two lines of text, or a table's header and first row.
func (w *pdfWriter) minHeight(b docBlock) float64 {
	switch b.kind {
	case tableBlock:
		height := 0.0
		if tableHeaded(b) {
			height += w.rowHeight(b, headerCells(b), true)
		}
		if len(b.rows) > 0 {
			height += w.rowHeight(b, b.rows[0], false)
		}
		return height
	case rawBlock:
		return 2 * rawLeading
	case listBlock, paragraphBlock:
		return 2 * bodyLeading
	case subheadingBlock:
		return bodyLeading + 2*bodyLeading
	}
	return bodyLeading
}

func tableHeaded(b docBlock) bool {
	for _, c := range b.columns {
		if c.header != "" {
			return true
		}
	}
	return false
}

func headerCells(b docBlock) []string {
	cells := []string{}
	for _, c := range b.columns {
		cells = append(cells, c.header)
	}
	return cells
}

func (w *pdfWriter) cellLines(b docBlock, row []string, header bool) [][]string {
	out := [][]string{}
	for i, cell := range row {
		f := regular
		if header || (!tableHeaded(b) && i == 0) {
			f = bold
		}
		out = append(out, wrap(cell, f, bodySize, b.columns[i].width*w.textWidth()-cellPad))
	}
	return out
}

func (w *pdfWriter) rowHeight(b docBlock, row []string, header bool) float64 {
	most := 1
	for _, cell := range w.cellLines(b, row, header) {
		most = max(most, len(cell))
	}
	return float64(most)*bodyLeading + 2*rowPad
}

// drawRow draws one table row. A row that fits on a page is never split: it
// moves to the next page, under the table's header again. A row taller than
// a whole page continues across pages, each under the header.
func (w *pdfWriter) drawRow(b docBlock, row []string, header bool) {
	cells := w.cellLines(b, row, header)
	height := w.rowHeight(b, row, header)
	headed := tableHeaded(b) && !header
	headerHeight := 0.0
	if headed {
		headerHeight = w.rowHeight(b, headerCells(b), true)
	}
	if height > w.room() {
		if w.y < w.top() && height <= w.top()-w.bottom()-headerHeight {
			w.newPage()
			if headed {
				w.drawRow(b, headerCells(b), true)
			}
		}
	}
	lines := 0
	for _, cell := range cells {
		lines = max(lines, len(cell))
	}
	fonts := make([]pdfFont, len(cells))
	for i := range cells {
		fonts[i] = regular
		if header || (!tableHeaded(b) && i == 0) {
			fonts[i] = bold
		}
	}
	w.y -= rowPad
	if float64(lines)*bodyLeading <= w.room() {
		// A row on one page is drawn cell by cell, so its text reads in
		// cell order.
		x := w.left()
		for i, cell := range cells {
			for line, text := range cell {
				w.text(fonts[i], bodySize, x, w.y-bodySize-float64(line)*bodyLeading, 0, text)
			}
			x += b.columns[i].width * w.textWidth()
		}
		w.y -= float64(lines) * bodyLeading
	} else {
		for line := 0; line < lines; line++ {
			if bodyLeading+rowPad > w.room() {
				w.newPage()
				if headed {
					w.drawRow(b, headerCells(b), true)
				}
				w.y -= rowPad
			}
			x := w.left()
			for i, cell := range cells {
				if line < len(cell) {
					w.text(fonts[i], bodySize, x, w.y-bodySize, 0, cell[line])
				}
				x += b.columns[i].width * w.textWidth()
			}
			w.y -= bodyLeading
		}
	}
	w.y -= rowPad
	gray := 0.8
	if header {
		gray = 0.4
	}
	w.rule(w.y, w.left(), w.left()+w.textWidth(), gray)
}

// renderPDF lays the report out on pages of the chosen size and writes a
// fixed PDF object graph: a catalog, its pages, three standard fonts and
// each page's text. It carries no action, annotation, link, form,
// attachment, script or external resource.
func renderPDF(doc *Document, paper Paper) []byte {
	w := &pdfWriter{width: 612, height: 792}
	if paper == A4 {
		w.width, w.height = 595.28, 841.89
	}
	w.newPage()
	blocks := documentLayout(doc)
	for i, b := range blocks {
		next := docBlock{kind: paragraphBlock}
		if i+1 < len(blocks) {
			next = blocks[i+1]
		}
		switch b.kind {
		case titleBlock:
			w.lines(wrap(b.text, bold, titleSize, w.textWidth()), bold, titleSize, titleLeading, w.left(), 0)
		case subtitleBlock:
			w.lines(wrap(b.text, regular, bodySize, w.textWidth()), regular, bodySize, bodyLeading, w.left(), 0.35)
			w.y -= 6
		case headingBlock:
			lines := wrap(b.text, bold, sectionSize, w.textWidth())
			w.y -= 12
			w.ensure(float64(len(lines))*sectionLeading + w.minHeight(next))
			w.lines(lines, bold, sectionSize, sectionLeading, w.left(), 0)
			w.y -= 2
		case subheadingBlock:
			lines := wrap(b.text, bold, bodySize, w.textWidth())
			w.y -= 8
			w.ensure(float64(len(lines))*bodyLeading + w.minHeight(next))
			w.lines(lines, bold, bodySize, bodyLeading, w.left(), 0)
			w.y -= 2
		case paragraphBlock:
			w.lines(wrap(b.text, regular, bodySize, w.textWidth()), regular, bodySize, bodyLeading, w.left(), 0)
			w.y -= 4
		case listBlock:
			for _, item := range b.items {
				lines := wrap(item, regular, bodySize, w.textWidth()-12)
				w.ensure(bodyLeading)
				w.text(regular, bodySize, w.left(), w.y-bodySize, 0, "-")
				w.lines(lines, regular, bodySize, bodyLeading, w.left()+12, 0)
			}
			w.y -= 4
		case rawBlock:
			w.lines(wrap(b.text, mono, rawSize, w.textWidth()), mono, rawSize, rawLeading, w.left(), 0)
			w.y -= 6
		case tableBlock:
			if tableHeaded(b) {
				w.ensure(w.minHeight(b))
				w.drawRow(b, headerCells(b), true)
			}
			for _, row := range b.rows {
				w.drawRow(b, row, false)
			}
			w.y -= 8
		}
	}
	return pdfObjects(w)
}

func pdfObjects(w *pdfWriter) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>"}
	kids := []string{}
	for index, page := range w.pages {
		footer := fmt.Sprintf("Page %d of %d", index+1, len(w.pages))
		x := (w.width - regular.width(footer, footerSize)) / 2
		fmt.Fprintf(&page.content, "BT /F1 %s Tf 0.35 g %s %s Td (%s) Tj ET\n", num(footerSize), num(x), num(pdfMargin/2), footer)
		pageID := len(objects) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >> >> /Contents %d 0 R >>",
			num(w.width), num(w.height), pageID+1))
		content := page.content.String()
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(w.pages), strings.Join(kids, " "))
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	start := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return out.Bytes()
}

package report

import (
	"html"
	"strings"
)

// htmlStyle is the report's only presentation: system fonts and system
// colours resolved in the light scheme, a centered reading width, and print
// rules that repeat table headers and keep a row and a heading with what
// follows. It loads nothing.
const htmlStyle = `:root{color-scheme:light}
body{margin:0;background:Canvas;color:CanvasText;font:14px/20px -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
main{box-sizing:border-box;width:100%;max-width:760px;margin:0 auto;padding:32px 16px}
h1{font-size:20px;line-height:28px;margin:0 0 4px}
h2{font-size:16px;line-height:22px;margin:28px 0 8px;break-after:avoid}
h3{font-size:14px;line-height:20px;margin:20px 0 6px;break-after:avoid}
p{margin:0 0 8px}
.subtitle{color:GrayText;margin-bottom:16px}
table{width:100%;border-collapse:collapse;margin:0 0 12px;table-layout:fixed}
th,td{text-align:left;vertical-align:top;padding:6px 8px 6px 0;border-bottom:1px solid GrayText;overflow-wrap:anywhere}
th{font-weight:600}
thead{display:table-header-group}
tr{break-inside:avoid}
pre{white-space:pre-wrap;overflow-wrap:anywhere;font:13px/18px ui-monospace,Menlo,Consolas,monospace;margin:0 0 12px}
ul{margin:0 0 12px;padding-left:20px}
@page{size:letter portrait;margin:18mm}
`

// renderHTML writes the report as one self-contained semantic document:
// escaped text only, no script, event handler, form, image, link or other
// resource, under a policy that forbids loading anything.
func renderHTML(doc *Document) []byte {
	var out strings.Builder
	out.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">")
	out.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'\">")
	out.WriteString("<meta name=\"referrer\" content=\"no-referrer\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	out.WriteString("<title>" + html.EscapeString(Escape(doc.Title)) + "</title><style>" + htmlStyle + "</style></head><body><main>\n")
	for _, b := range documentLayout(doc) {
		switch b.kind {
		case titleBlock:
			out.WriteString("<h1>" + html.EscapeString(b.text) + "</h1>\n")
		case subtitleBlock:
			out.WriteString("<p class=\"subtitle\">" + html.EscapeString(b.text) + "</p>\n")
		case headingBlock:
			out.WriteString("<h2>" + html.EscapeString(b.text) + "</h2>\n")
		case subheadingBlock:
			if b.id != "" {
				out.WriteString("<h3 id=\"" + b.id + "\">" + html.EscapeString(b.text) + "</h3>\n")
			} else {
				out.WriteString("<h3>" + html.EscapeString(b.text) + "</h3>\n")
			}
		case paragraphBlock:
			out.WriteString("<p>" + html.EscapeString(b.text) + "</p>\n")
		case listBlock:
			out.WriteString("<ul>")
			for _, item := range b.items {
				out.WriteString("<li>" + html.EscapeString(item) + "</li>")
			}
			out.WriteString("</ul>\n")
		case rawBlock:
			out.WriteString("<pre>" + html.EscapeString(b.text) + "</pre>\n")
		case tableBlock:
			out.WriteString("<table>")
			headed := false
			for _, c := range b.columns {
				headed = headed || c.header != ""
			}
			out.WriteString("<colgroup>")
			for _, c := range b.columns {
				out.WriteString("<col style=\"width:" + percent(c.width) + "\">")
			}
			out.WriteString("</colgroup>")
			if headed {
				out.WriteString("<thead><tr>")
				for _, c := range b.columns {
					out.WriteString("<th scope=\"col\">" + html.EscapeString(c.header) + "</th>")
				}
				out.WriteString("</tr></thead>")
			}
			out.WriteString("<tbody>")
			for _, row := range b.rows {
				out.WriteString("<tr>")
				for i, cell := range row {
					if i == 0 && !headed {
						out.WriteString("<th scope=\"row\">" + html.EscapeString(cell) + "</th>")
						continue
					}
					out.WriteString("<td>" + html.EscapeString(cell) + "</td>")
				}
				out.WriteString("</tr>")
			}
			out.WriteString("</tbody></table>\n")
		}
	}
	out.WriteString("</main></body></html>\n")
	return []byte(out.String())
}

func percent(width float64) string {
	n := int(width*1000 + 0.5)
	return itoa(n/10) + "." + itoa(n%10) + "%"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

package report

import (
	"encoding/xml"
	"errors"
	"html"
	"strings"
)

type reviewRun struct{ Name, Status string }
type junitFailure struct {
	Message string `xml:"message,attr"`
}
type junitCase struct {
	Name    string        `xml:"name,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
	Error   *junitFailure `xml:"error,omitempty"`
}
type junitReview struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Errors   int         `xml:"errors,attr"`
	Cases    []junitCase `xml:"testcase"`
	Content  string      `xml:"system-out"`
}

func renderPortable(doc portableReport, runs []reviewRun) (map[string][]byte, error) {
	size := 0
	for _, line := range doc.Lines {
		size += len(line) + 1
		if size > 1<<20 {
			return nil, errors.New("portable report text exceeds 1 MiB; select a smaller retained packet")
		}
	}
	text := strings.Join(doc.Lines, "\n") + "\n"
	document, err := encode(doc)
	if err != nil {
		return nil, err
	}
	var markdown strings.Builder
	for _, line := range doc.Lines {
		markdown.WriteString("    " + line + "\n")
	}
	// Indented code never promotes evidence to Markdown links or raw HTML.
	htmlDoc := "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; base-uri 'none'; form-action 'none'; sandbox\"><meta name=\"referrer\" content=\"no-referrer\"><title>Readmit sensitive evidence review</title></head><body><pre>" + html.EscapeString(text) + "</pre></body></html>\n"
	suite := junitReview{Name: "retained-investigation", Tests: len(runs), Content: text}
	for _, run := range runs {
		entry := junitCase{Name: run.Name}
		switch run.Status {
		case "pass":
		case "assertion_failure":
			entry.Failure = &junitFailure{Message: "Retained assertion failure; see sensitive report content"}
			suite.Failures++
		default:
			entry.Error = &junitFailure{Message: "Execution error or unresolved lifecycle; no successful regression proof"}
			suite.Errors++
		}
		suite.Cases = append(suite.Cases, entry)
	}
	junit, err := xml.Marshal(suite)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"report.html": []byte(htmlDoc), "report.md": []byte(markdown.String()), "report.json": document, "junit.xml": append([]byte(xml.Header), junit...), "report.pdf": pdfReport(doc.Lines)}, nil
}

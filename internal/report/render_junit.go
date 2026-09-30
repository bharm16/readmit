package report

import (
	"encoding/xml"
	"errors"
)

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitDocumentCase struct {
	ClassName string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitFailure `xml:"error,omitempty"`
}

type junitDocumentSuite struct {
	XMLName    xml.Name            `xml:"testsuite"`
	Name       string              `xml:"name,attr"`
	Tests      int                 `xml:"tests,attr"`
	Failures   int                 `xml:"failures,attr"`
	Errors     int                 `xml:"errors,attr"`
	Properties []junitProperty     `xml:"properties>property"`
	Cases      []junitDocumentCase `xml:"testcase"`
	Content    string              `xml:"system-out"`
}

// renderJUnit writes the report for a CI reader: the run itself as one test
// case, which is an error when the run ended in an execution error or never
// reached a usable decided state, and one test case per check, a failed
// check a failure and a check the run never evaluated an error. The
// readable report is its standard output.
func renderJUnit(doc *Document) ([]byte, error) {
	suite := junitDocumentSuite{Name: Escape(doc.Title), Content: string(renderMarkdown(doc)),
		Properties: []junitProperty{{"outcome", OutcomeLabel(doc.Result.Outcome)}, {"packet", doc.PacketIdentity}}}
	for _, run := range doc.Runs {
		suite.Properties = append(suite.Properties, junitProperty{run.Role + "-result", run.ResultIdentity})
	}
	runCase := junitDocumentCase{ClassName: "run", Name: "Run"}
	switch doc.Result.Outcome {
	case OutcomeError:
		runCase.Error = &junitFailure{Message: "Error: " + cmpOr(Escape(doc.Result.ErrorClass), "execution error")}
	case OutcomeIncomplete:
		message := "Incomplete"
		for _, fact := range lifecycleFacts(doc.Result) {
			message += "; " + fact
		}
		runCase.Error = &junitFailure{Message: message}
	}
	suite.Cases = append(suite.Cases, runCase)
	l := &layout{}
	names := messageNames(doc.Messages)
	for _, check := range doc.Checks {
		label := CheckLabel(check.Operator, check.Selector, names[check.Message])
		entry := junitDocumentCase{ClassName: "check", Name: label}
		observed := "Unavailable: " + check.Unavailable
		if check.Observed != nil {
			observed = l.value(label, *check.Observed)
		}
		detail := "Expected " + l.value(label, check.Expected) + "; observed " + observed
		switch check.Result {
		case "failed":
			entry.Failure = &junitFailure{Message: "Failed. " + detail}
		case "not_evaluated":
			entry.Error = &junitFailure{Message: "Not evaluated. " + detail}
		}
		suite.Cases = append(suite.Cases, entry)
	}
	for _, entry := range suite.Cases {
		switch {
		case entry.Failure != nil:
			suite.Failures++
		case entry.Error != nil:
			suite.Errors++
		}
	}
	suite.Tests = len(suite.Cases)
	data, err := xml.Marshal(suite)
	if err != nil {
		return nil, errors.New("cannot render the report for JUnit")
	}
	return append(append([]byte(xml.Header), data...), '\n'), nil
}

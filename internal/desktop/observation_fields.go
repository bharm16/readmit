package desktop

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/operation"
)

// ObservationFieldChoice comes from the selected local source or its explicit
// sample export. Its locator is carried verbatim; the window never parses a
// path or manufactures a selector from the displayed field label.
type ObservationFieldChoice struct {
	ID       string           `json:"id"`
	Locator  importer.Locator `json:"locator,omitzero"`
	Selector string           `json:"selector,omitzero"`
}

func projectionFor(source observesource.Source) dataset.Projection {
	p := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "observed", Order: "source", Columns: []dataset.Column{}, Limits: observesource.DefaultDatasetLimits(source)}
	switch {
	case source.Extraction != nil:
		e := source.Extraction
		p.Format = string(e.Envelope)
		shape := e.Shape()
		p.Envelope = &dataset.Envelope{Encoding: shape.Encoding, CSV: shape.CSV, JSON: shape.JSON, XML: shape.XML, Text: shape.Text}
	case source.Database != nil:
		p.Format = "database"
		p.Order = "unordered"
	case source.Capture != nil:
		p.Format = "hl7"
	}
	return p
}

func (a *App) connectedObservationFields(ctx context.Context, request ObservationFieldsRequest, result ObservationFieldsResult) ObservationFieldsResult {
	loaded, declined := a.loadCatalog(ctx, request.Context, false)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if request.Resource != "" {
		if !slices.Contains(fhirr4.ProjectionResources(), request.Resource) {
			result.refuse(Failed, "Choose a supported resource type")
			return result
		}
		parameters, current := fhirSearchParameters(loaded, request.Environment, request.Resource)
		result.Search = parameters
		if !current {
			result.Reason = "Check capabilities to offer the server's supported criteria; identifier and logical ID remain explicit choices"
		}
		result.State = Completed
		return result
	}
	source := observesource.Source{}
	if request.Source != nil {
		source = *request.Source
	}
	p := projectionFor(source)
	result.Projection = &p
	if source.Capture != nil && source.Capture.Path != "" {
		path := source.Capture.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(loaded.root, path)
		}
		capture, err := bundle.Open(path)
		if err != nil {
			result.refuse(Failed, "Choose a readable retained capture")
			return result
		}
		seen := map[string]bool{}
		for _, event := range capture.Events[:min(len(capture.Events), 8)] {
			raw, err := capture.Raw(event.ID)
			if err != nil {
				result.refuse(Failed, "The capture cannot be read")
				return result
			}
			doc, err := hl7.Parse(raw, hl7.Options{})
			if err != nil {
				continue
			}
			for _, message := range doc.Messages {
				for _, segment := range message.Segments {
					for _, field := range segment.Fields {
						selector := segment.ID + "-" + strconv.Itoa(field.Number)
						if !seen[selector] {
							seen[selector] = true
							result.Choices = append(result.Choices, ObservationFieldChoice{ID: selector, Selector: selector})
						}
					}
				}
			}
		}
	} else if request.SchemaSample != "" {
		path := request.SchemaSample
		if !filepath.IsAbs(path) {
			path = filepath.Join(loaded.root, path)
		}
		if source.File != nil && request.SchemaSample == source.File.Path && source.File.MaxBytes < importer.ProbeHeadBytes {
			if _, err := operation.ReadInputFile(path, max(source.File.MaxBytes, 1)); err != nil {
				result.refuse(Failed, "The input file cannot be read within its maximum size")
				return result
			}
		}
		probe, err := importer.ProbeInputs(ctx, []string{path}, nil, nil)
		if err != nil || probe.Sample == nil {
			result.refuse(Failed, "Choose a local schema sample that exposes supported columns or fields")
			return result
		}
		for _, column := range probe.Sample.Columns {
			result.Choices = append(result.Choices, ObservationFieldChoice{ID: column, Locator: importer.Locator{column}})
		}
		var recordPath []string
		if source.Extraction != nil {
			if source.Extraction.JSON != nil {
				recordPath = source.Extraction.JSON.RecordPath
			} else if source.Extraction.XML != nil {
				recordPath = source.Extraction.XML.RecordPath
			}
		}
		for _, path := range probe.Sample.Paths {
			inside := len(path) >= len(recordPath) && slices.Equal(path[:len(recordPath)], recordPath)
			if source.HTTP != nil && source.Extraction != nil && source.Extraction.JSON != nil && !inside {
				result.Continuations = append(result.Continuations, ObservationFieldChoice{ID: strings.Join(path, "."), Locator: slices.Clone(importer.Locator(path))})
			}
			if !inside || len(path) == len(recordPath) {
				continue
			}
			relative := slices.Clone(importer.Locator(path[len(recordPath):]))
			result.Choices = append(result.Choices, ObservationFieldChoice{ID: strings.Join(relative, "."), Locator: relative})
		}
	} else {
		result.refuse(Failed, "Choose a local schema sample before mapping this network source")
		return result
	}
	for _, field := range result.Choices {
		result.Fields = append(result.Fields, field.ID)
	}
	result.State = Completed
	if len(result.Choices) == 0 {
		result.State = Empty
		result.Reason = "The selected source offers no supported fields"
	}
	return result
}

// fhirSearchParameters is the authoring picker and Save's admission vocabulary.
// Additional criteria come only from the exact saved environment revision's
// recorded CapabilityStatement, never from a frontend list or a remote read.
func fhirSearchParameters(c *loadedCatalog, environment, resource string) ([]fhirr4.SearchParameter, bool) {
	initial := []fhirr4.SearchParameter{{Name: "_id", Type: "token"}, {Name: "identifier", Type: "token"}}
	i := c.document.Find(environment)
	if i < 0 || c.removed(c.document.Items[i]) {
		return initial, false
	}
	item := c.read(c.document.Items[i])
	if item.Summary.Environment == nil {
		return initial, false
	}
	check := item.Summary.Environment.Capabilities
	if check == nil || check.Revision != item.Ref.Revision || check.Claims == nil {
		return initial, false
	}
	for _, rest := range check.Claims.REST {
		if rest.Mode != "server" {
			continue
		}
		for _, claims := range rest.Resources {
			if claims.Type != resource {
				continue
			}
			out := []fhirr4.SearchParameter{{Name: "_id", Type: "token"}}
			for _, parameter := range claims.Search {
				if slices.Contains([]string{"token", "reference", "date", "string", "number", "uri"}, parameter.Type) && !strings.HasPrefix(parameter.Name, "_") {
					out = append(out, parameter)
				}
			}
			return out, true
		}
	}
	return []fhirr4.SearchParameter{}, true
}

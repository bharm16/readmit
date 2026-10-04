package grid

import (
	"context"
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
)

// Selection is a complete backend-only query selection. Unknown counts are
// scoped by the same non-field axes; rendered page bounds remain unchanged.
type Selection struct {
	Records     []index.Record
	Total       int
	Undecided   int
	Undecodable int
}

// SelectAllQuery uses the same index-owned predicates as the message list,
// while collecting all bounded matching occurrences for aggregate consumers.
// It never converts a shortened retained prefix into a complete value.
func SelectAllQuery(ctx context.Context, document index.Document, at time.Time, query Query, facts Facts) (Selection, error) {
	if len(document.Records) > bundle.MaxEvents {
		return Selection{}, errors.New("aggregate scope exceeds the case occurrence bound")
	}
	if err := ValidateQuery(query); err != nil {
		return Selection{}, err
	}
	if facts == nil && (len(query.Types) > 0 || len(query.NotTypes) > 0 || query.Search != nil) {
		return Selection{}, errors.New("type or search scope needs the verified case facts")
	}
	if err := document.Usable(at); err != nil {
		return Selection{}, err
	}
	page := Page{}
	kept, open, asked, err := answered(document, at, &Filter{AckCodes: query.AckCodes, Fields: query.Fields}, &page)
	if err != nil {
		return Selection{}, err
	}
	result := Selection{Records: []index.Record{}, Total: len(document.Records)}
	for _, record := range document.Records {
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		if !keeps(query, facts, record) {
			continue
		}
		if record.ParseError != "" {
			result.Undecodable++
		}
		if open[record.ID] && !kept[record.ID] {
			result.Undecided++
		}
		include := !asked || kept[record.ID]
		switch query.Scope {
		case UndecidedRows:
			include = open[record.ID] && !kept[record.ID]
		case UndecodableRows:
			include = record.ParseError != ""
		}
		if include {
			result.Records = append(result.Records, record)
		}
	}
	return result, nil
}

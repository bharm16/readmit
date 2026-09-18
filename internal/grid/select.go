package grid

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/index"
)

// Window is the bounded view of a filtered case a caller renders. A grid over a
// large case renders a window of it and asks for the next one; nothing here
// materializes every matching occurrence for the caller.
type Window struct {
	Offset int
	Limit  int
}

// Page is one window, and everything a person needs to know about what is not
// in it.
//
// Total is every occurrence the index describes, which is every occurrence of
// the case. Matched is how many the filter kept, and Rows is the requested
// window of those. Excluded is Total minus Matched: the number of records this
// view is not showing, which is stated rather than left to be inferred from a
// scrollbar.
//
// Undecided counts the retained values this filter's questions could not settle
// either way, because the value the index kept was shortened. The index answers
// a question about a field over every occurrence it holds, so this is an upper
// bound on the uncertainty in this view: a value counted here may belong to an
// occurrence the type, source or time axis had already excluded for a definite
// reason. It over-states how much is unsettled rather than under-stating it,
// which is the only direction a caveat about a filtered view may be wrong in.
// Questions about different fields add up; the acknowledgement outcomes are
// alternatives about one field, so that axis contributes the most any one of
// them left unsettled rather than one count per alternative.
//
// Undecodable counts occurrences the case itself could not decode, whether or
// not this filter kept them: it is a fact about the case, the way `index show`
// reports it. Such an occurrence carries no indexed field, so any question
// about a field leaves it out, and reporting that as an absent value would
// claim the case does not hold something nobody ever decoded.
type Page struct {
	Rows        []index.Record
	Offset      int
	Limit       int
	Total       int
	Matched     int
	Excluded    int
	Undecided   int
	Undecodable int
}

// Select applies one filter to one index and returns the requested window.
//
// A nil filter narrows nothing and excludes nothing. The caller has already
// checked this index against the verified case through Describes; what is
// checked here is the retention the index declares, which is asked of the index
// itself — first directly, so a filter that asks no value question is refused
// by an expired index exactly as one that does, and then again inside every
// query. A question this index cannot answer — a field it does not retain, a
// substring of a digest — is refused by name rather than answered from
// something narrower.
func Select(document index.Document, at time.Time, filter *Filter, window Window) (Page, error) {
	if window.Offset < 0 {
		return Page{}, errors.New("a window begins at or after the first matching occurrence")
	}
	if window.Limit < 1 || window.Limit > MaxRows {
		return Page{}, errors.New("a window renders between 1 and " + strconv.Itoa(MaxRows) + " occurrences")
	}
	if filter != nil {
		if err := validatePredicates(*filter); err != nil {
			return Page{}, err
		}
	}
	if err := document.Usable(at); err != nil {
		return Page{}, err
	}
	page := Page{Rows: []index.Record{}, Offset: window.Offset, Limit: window.Limit, Total: len(document.Records)}
	kept, asked, err := answered(document, at, filter, &page)
	if err != nil {
		return Page{}, err
	}
	for _, record := range document.Records {
		if record.ParseError != "" {
			page.Undecodable++
		}
		if !narrows(filter, record) || (asked && !kept[record.ID]) {
			continue
		}
		page.Matched++
		if page.Matched > window.Offset && len(page.Rows) < window.Limit {
			page.Rows = append(page.Rows, record)
		}
	}
	page.Excluded = page.Total - page.Matched
	return page, nil
}

// answered asks the index every value and state question the filter carries and
// returns the occurrences that satisfied all of them. asked reports whether any
// question was asked at all, so "no question" is told apart from "no answer":
// an empty set of occurrences is a real, empty answer.
func answered(document index.Document, at time.Time, filter *Filter, page *Page) (map[string]bool, bool, error) {
	if filter == nil {
		return nil, false, nil
	}
	var kept map[string]bool
	asked := false
	for _, predicate := range filter.Fields {
		found, undecided, err := ask(document, at, index.Query{
			Field: predicate.Selector,
			Match: predicate.Match,
			Term:  []byte(predicate.Term),
			State: predicate.State,
		})
		if err != nil {
			return nil, false, err
		}
		page.Undecided += undecided
		kept, asked = intersect(kept, found, asked), true
	}
	if len(filter.AckCodes) > 0 {
		// The declared outcomes are alternatives: an occurrence carrying any
		// one of them answers this axis. They ask about one field, so what the
		// index could not settle about that field is counted once rather than
		// once per alternative, which would report the same value several
		// times over.
		either, unsettled := make(map[string]bool), 0
		for _, code := range filter.AckCodes {
			found, undecided, err := ask(document, at, index.Query{
				Field: AckCodeSelector, Match: index.Equals, Term: []byte(code),
			})
			if err != nil {
				return nil, false, err
			}
			unsettled = max(unsettled, undecided)
			for id := range found {
				either[id] = true
			}
		}
		page.Undecided += unsettled
		kept, asked = intersect(kept, either, asked), true
	}
	return kept, asked, nil
}

// ask puts one question to the index and returns the occurrences that answered
// it, with the retained values it could not settle. Retention, the retained
// form and whether the field is indexed at all are the index's own decisions,
// made inside Search.
func ask(document index.Document, at time.Time, query index.Query) (map[string]bool, int, error) {
	result, err := document.Search(at, query)
	if err != nil {
		return nil, 0, err
	}
	found := make(map[string]bool, len(result.Hits))
	for _, hit := range result.Hits {
		found[hit.Record.ID] = true
	}
	return found, result.Undecided, nil
}

// intersect narrows the occurrences kept so far by one more answered question.
// Questions on different axes are all required, so the first answer stands on
// its own and every later one can only remove occurrences.
func intersect(kept, found map[string]bool, asked bool) map[string]bool {
	if !asked {
		return found
	}
	narrowed := make(map[string]bool, min(len(kept), len(found)))
	for id := range kept {
		if found[id] {
			narrowed[id] = true
		}
	}
	return narrowed
}

// narrows reports whether one record satisfies the axes read from the index
// record itself: the occurrence type, the source it arrived on, and when it was
// observed. An empty axis constrains nothing.
func narrows(filter *Filter, record index.Record) bool {
	if filter == nil {
		return true
	}
	if len(filter.Kinds) > 0 && !slices.Contains(filter.Kinds, record.Kind) {
		return false
	}
	if len(filter.Sources) > 0 && !slices.Contains(filter.Sources, record.SourceID) {
		return false
	}
	if filter.ObservedFrom == nil && filter.ObservedUntil == nil {
		return true
	}
	// A time bound is answered from the time the case recorded. An occurrence
	// with no recorded observed time is excluded rather than admitted: unknown
	// is not a pass, and the excluded count says it was left out.
	if record.ObservedAt == nil {
		return false
	}
	if filter.ObservedFrom != nil && record.ObservedAt.Before(*filter.ObservedFrom) {
		return false
	}
	return filter.ObservedUntil == nil || record.ObservedAt.Before(*filter.ObservedUntil)
}

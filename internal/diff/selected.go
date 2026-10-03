package diff

import "errors"

// CompareSelected compares two explicitly selected occurrences through the
// same verified readers and field/segment comparator as Compare.
func CompareSelected(left, right Input, leftOccurrence, rightOccurrence string, options Options) (Report, error) {
	return compare(left, right, options, nil, &selectedPair{left: leftOccurrence, right: rightOccurrence})
}

type selectedPair struct{ left, right string }

func explicitOccurrence(evidence *evidence, id string) (*occurrence, error) {
	if id == "" || len(id) > 128 {
		return nil, errors.New("a selected comparison names one retained occurrence on each side")
	}
	for _, item := range evidence.items {
		if item.ref.Occurrence == id {
			return item, nil
		}
	}
	return nil, errors.New("a selected comparison occurrence is unavailable under this boundary")
}

package reduce

import (
	"encoding/json/v2"
	"errors"
)

// JSON encodes the report deterministically as one readmit-reduction/v1
// document. It reopens no evidence, runs no trial and re-decides nothing.
//
// A report past its size limit is refused rather than truncated: a reduction
// nobody can read back whole is not one this release reports. The bound is
// reachable, because a report records every trial it spent and the assertions
// each of them failed.
func JSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode reduction report")
	}
	if len(data)+1 > MaxReportBytes {
		return nil, errors.New("reduction report exceeds 32 MiB; reduce a shorter sequence or spend a smaller budget")
	}
	return append(data, '\n'), nil
}

package dictionary

// segmentNames are readmit-authored readable names for the segments the
// bundled v2.5.1 field labels cover. They are not adapted from the upstream
// label data and carry none of its provenance: they are the standard's segment
// titles, written here so the inspector's segment outline can read "PID ·
// Patient identification" rather than a bare code.
var segmentNames = map[string]string{
	"AIG": "Appointment information - general resource",
	"AIL": "Appointment information - location resource",
	"AIP": "Appointment information - personnel resource",
	"AIS": "Appointment information - service",
	"ERR": "Error",
	"EVN": "Event type",
	"MSA": "Message acknowledgment",
	"MSH": "Message header",
	"NTE": "Notes and comments",
	"OBX": "Observation/result",
	"PID": "Patient identification",
	"PV1": "Patient visit",
	"RGS": "Resource group",
	"SCH": "Scheduling activity information",
}

// SegmentName is the readable name of one segment code, or "" for a segment
// this release names nothing for. The exact code is always shown beside it.
func SegmentName(segment string) string { return segmentNames[segment] }

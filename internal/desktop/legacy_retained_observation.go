package desktop

import (
	"context"
	"fmt"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// RetainedObservationFamily separates archived legacy ledger snapshots from
// typed connected lifecycle datasets without changing either evidence format.
type RetainedObservationFamily string

const (
	LegacyLedgerObservation       RetainedObservationFamily = "legacy-ledger"
	ConnectedLifecycleObservation RetainedObservationFamily = "connected-lifecycle"
)

// RunRetainedObservation is the exact immutable snapshot a run detail offers
// for inspection. It contains no source values or live acquisition authority.
type RunRetainedObservation struct {
	ReceiverMode   string                    `json:"receiver_mode,omitzero"`
	Family         RetainedObservationFamily `json:"family"`
	Phase          string                    `json:"phase"`
	Dataset        string                    `json:"dataset"`
	Identity       string                    `json:"identity,omitzero"`
	SourceIdentity string                    `json:"source_identity,omitzero"`
	Available      bool                      `json:"available"`
	Reason         string                    `json:"reason,omitzero"`
	Total          int                       `json:"total"`
}

func legacyRetainedObservation(opened *runresult.Result) *RunRetainedObservation {
	if opened == nil || opened.Spec == nil || opened.Spec.Observation.Boundary != testrunner.LedgerBoundary {
		return nil
	}
	pin := &RunRetainedObservation{Family: LegacyLedgerObservation, Phase: "retained", Dataset: "appointment-ledger", Reason: "this execution kept no complete verified final observation; no absence is established"}
	if opened.Artifact == nil {
		return pin
	}
	artifact := opened.Artifact
	pin.SourceIdentity = artifact.Result.InputBundleIdentity
	pin.ReceiverMode = string(artifact.Result.ReceiverMode)
	if artifact.Result.FinalObservation != nil {
		pin.Identity = artifact.Result.FinalObservation.SHA256
	}
	if artifact.FinalObservation == nil || !artifact.FinalObservation.Consistent || artifact.Result.ErrorClass != "" {
		return pin
	}
	if opened.Durable && (opened.Lifecycle.JournalIncomplete || opened.Lifecycle.DeliveryUncertain || opened.Lifecycle.State == durablerun.Interrupted || opened.Lifecycle.State == durablerun.Cancelled || opened.Lifecycle.State == durablerun.TimedOut) {
		return pin
	}
	pin.Available, pin.Reason, pin.Total = true, "", len(artifact.FinalObservation.Records)
	return pin
}

func readLegacyRetainedObservation(ctx context.Context, path string, request ConnectedObservationRequest, result ConnectedObservationResult) ConnectedObservationResult {
	if request.Job != "" {
		if strings.ContainsAny(request.Job, "/\\") {
			result.refuse(Failed, "choose an exact declared retained suite job")
			return result
		}
		execution, err := suite.OpenExecution(path)
		known := false
		if err == nil {
			for _, declared := range execution.Queue.Jobs {
				if declared.ID == request.Job {
					known = true
				}
			}
		}
		if !known {
			result.refuse(Failed, "the retained suite holds no such declared job")
			return result
		}
		runs, err := artifactpath.Child(path, "runs")
		if err != nil {
			result.refuse(Failed, "the retained suite job path cannot be read")
			return result
		}
		path, err = artifactpath.Child(runs, request.Job)
		if err != nil {
			result.refuse(Failed, "the retained suite job path cannot be read")
			return result
		}
	}
	select {
	case <-ctx.Done():
		result.refuse(Cancelled, "retained observation read cancelled")
		return result
	default:
	}
	opened, err := runresult.Open(path)
	if err != nil {
		result.refuse(Failed, "the original retained run and snapshot cannot be verified")
		return result
	}
	pin := legacyRetainedObservation(opened)
	if pin == nil || request.Phase != pin.Phase || request.Dataset != pin.Dataset || request.Identity == "" || request.Identity != pin.Identity || request.SourceIdentity == "" || request.SourceIdentity != pin.SourceIdentity {
		result.refuse(Failed, "select the exact retained snapshot and original input identity offered by this run")
		return result
	}
	result.State = Completed
	if !pin.Available {
		result.Reason = pin.Reason
		return result
	}
	names := []string{"record_id", "patient_value", "patient_namespace", "patient_universal_id", "patient_universal_id_type", "placer_value", "placer_namespace", "placer_universal_id", "placer_universal_id_type", "filler_value", "filler_namespace", "filler_universal_id", "filler_universal_id_type", "appointment_start"}
	for _, name := range names {
		result.Columns = append(result.Columns, ConnectedObservationColumn{Name: name, Type: "text"})
	}
	snapshot := opened.Artifact.FinalObservation
	result.Available, result.Identity, result.Total = true, pin.Identity, len(snapshot.Records)
	limit := request.Limit
	if limit == 0 {
		limit = 50
	}
	start := min(request.Offset, len(snapshot.Records))
	end := start + min(limit, len(snapshot.Records)-start)
	result.Offset = start
	for index := start; index < end; index++ {
		record := snapshot.Records[index]
		values := []string{record.RecordID, record.PatientID.Value, record.PatientID.Namespace, record.PatientID.UniversalID, record.PatientID.UniversalIDType, record.PlacerID.Value, record.PlacerID.Namespace, record.PlacerID.UniversalID, record.PlacerID.UniversalIDType, record.FillerID.Value, record.FillerID.Namespace, record.FillerID.UniversalID, record.FillerID.UniversalIDType, record.AppointmentStart}
		row := ConnectedObservationRow{ID: fmt.Sprintf("row-%06d", index+1), Values: []dataset.Value{}}
		for _, text := range values {
			state := "present"
			if text == "" {
				state = "empty"
			}
			value := dataset.Value{State: state, Type: "text"}
			if request.Reveal {
				value.Text = text
			}
			row.Values = append(row.Values, value)
		}
		result.Rows = append(result.Rows, row)
	}
	return result
}

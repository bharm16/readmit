package desktop

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/report"
)

// The synthetic demonstration packets of the investigation-packet panels.
// This file reaches the existing `readmit report`, `report verify` and
// `report prepare` operations and adds no engine of its own: generation is
// report.Create into one new folder named in the host's save dialog,
// verification is report.Open, offline and read-only, over a packet folder
// chosen in the folder dialog, and preparation is report.Prepare into a new
// folder outside the packet, named in the save dialog.
//
// A synthetic packet is never the person's own evidence. It is generated from
// the one committed scenario on built-in fixtures this process starts on
// loopback, and every view here carries the packet's own synthetic-only
// provenance and limitations exactly as its verified manifest states them.
// Like the command line, none of this is admitted work: the synthetic
// walkthrough stays ungated, and reading or preparing a packet acquires no
// send or mutation authority.

// syntheticPacketOperation is the name generation runs and is cancelled
// under. Generation sends the scenario's synthetic messages to the built-in
// receivers it starts on loopback in this process, so the privacy status
// reports it with the run activity while it holds the slot.
const syntheticPacketOperation = "synthetic-packet"

// The folders ChooseSyntheticPacketPath names.
const (
	packetDestination = "packet-destination"
	packetFolder      = "packet"
	rerunDestination  = "rerun-destination"
)

// ChooseSyntheticPacketPath names a folder for the synthetic packet section:
// a new folder for a packet ("packet-destination") or for its runnable copies
// ("rerun-destination") in the host's save dialog, or an existing packet to
// verify ("packet") in the folder dialog. Choosing creates, verifies and
// contacts nothing.
func (a *App) ChooseSyntheticPacketPath(kind string) PacketPathResult {
	return run(a, true, false, func(ctx context.Context) PacketPathResult {
		var choose func(context.Context, string) (string, refusal)
		var title string
		switch kind {
		case packetDestination:
			choose, title = a.chooseDestination, "Choose a new folder for the synthetic demonstration packet"
		case packetFolder:
			choose, title = a.chooseFolder, "Choose a synthetic demonstration packet to verify"
		case rerunDestination:
			choose, title = a.chooseDestination, "Choose a new folder for the runnable copies"
		default:
			return PacketPathResult{State: Failed, Reason: "unknown synthetic packet folder kind"}
		}
		folder, declined := choose(ctx, title)
		if folder == "" {
			return PacketPathResult{State: declined.state, Reason: declined.reason}
		}
		return PacketPathResult{State: Completed, Path: folder}
	})
}

// SyntheticPacketRequest names the committed scenario, as `readmit report
// --scenario` must, and the new folder, named in the save dialog, the packet
// is generated into.
type SyntheticPacketRequest struct {
	Scenario    string `json:"scenario"`
	Destination string `json:"destination"`
}

// SyntheticRunView is one of the packet's two fixture executions, as its
// manifest labels them after verification: the receiver mode and built-in
// implementation it ran against, its verdict, and the appointment records its
// ledger kept.
type SyntheticRunView struct {
	Path           string `json:"path"`
	ReceiverMode   string `json:"receiver_mode"`
	Receiver       string `json:"receiver"`
	Status         string `json:"status"`
	LedgerCount    int    `json:"ledger_count"`
	ResultIdentity string `json:"result_identity"`
}

// SyntheticPacketView is a verified synthetic packet read back from disk. Its
// provenance and limitations are the packet's own; there is no member that
// could present it as customer evidence, and no operation behind it that
// could execute or change it.
type SyntheticPacketView struct {
	Folder                  string             `json:"folder"`
	Identity                string             `json:"identity"`
	Schema                  string             `json:"schema"`
	State                   string             `json:"state"`
	Scenario                string             `json:"scenario"`
	Provenance              string             `json:"provenance"`
	InputIdentity           string             `json:"input_identity"`
	SpecIdentity            string             `json:"spec_identity"`
	ObservationBoundary     string             `json:"observation_boundary"`
	InputChanged            bool               `json:"input_changed"`
	ReceiverBehaviorChanged bool               `json:"receiver_behavior_changed"`
	Runs                    []SyntheticRunView `json:"runs"`
	Files                   int                `json:"files"`
	Limitations             []string           `json:"limitations"`
}

// SyntheticPacketResult carries one state. Packet is present only when the
// operation verified a complete sealed packet; a refused or cancelled
// generation leaves any partial folder explicitly incomplete.
type SyntheticPacketResult struct {
	State  State                `json:"state"`
	Reason string               `json:"reason,omitzero"`
	Packet *SyntheticPacketView `json:"packet,omitzero"`
}

func (r *SyntheticPacketResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// GenerateSyntheticPacket is `readmit report`: it generates the committed
// scenario's case, runs it against fresh built-in defective and fixed
// receivers on loopback, seals the packet into the new folder and reads it
// back through the verifier. The scenario is the one the command line
// requires by name; any other is refused by the operation itself. Cancel
// stops the fixture executions; the partial folder never verifies.
func (a *App) GenerateSyntheticPacket(request SyntheticPacketRequest) SyntheticPacketResult {
	return runNamed[SyntheticPacketResult, *SyntheticPacketResult](a, profiles["GenerateSyntheticPacket"], func(ctx context.Context) SyntheticPacketResult {
		return generateSyntheticPacket(ctx, request)
	})
}

func generateSyntheticPacket(ctx context.Context, request SyntheticPacketRequest) SyntheticPacketResult {
	if !filepath.IsAbs(request.Destination) {
		return SyntheticPacketResult{State: Failed, Reason: "name a new folder for the synthetic packet in the save dialog first"}
	}
	packet, err := report.Create(ctx, request.Scenario, request.Destination)
	if err != nil {
		// The fixture executions stop at a cancellation and report it as a
		// failed execution; the person's cancellation is named as itself.
		// Every other refusal is the operation's own sentence, word for word
		// what `readmit report` prints, so packetRefusal's rewording of the
		// retained readers' errors is not applied here.
		if ctx.Err() != nil {
			return SyntheticPacketResult{State: Cancelled, Reason: "the synthetic packet generation was cancelled; the partial folder remains incomplete and cannot be verified as complete"}
		}
		return SyntheticPacketResult{State: Failed, Reason: err.Error()}
	}
	return SyntheticPacketResult{State: Completed, Packet: syntheticPacketView(request.Destination, packet)}
}

// OpenSyntheticPacket is `readmit report verify`: offline and read-only, it
// verifies the frozen case identity, both results and every derived file of
// the packet at path, and refuses a changed, incomplete or unsupported one
// with the verifier's own sentence. It resolves no historical path, contacts
// no endpoint and acquires no admission.
func (a *App) OpenSyntheticPacket(path string) SyntheticPacketResult {
	return run(a, false, false, func(context.Context) SyntheticPacketResult {
		if !filepath.IsAbs(path) {
			return SyntheticPacketResult{State: Failed, Reason: "choose the synthetic packet's folder first"}
		}
		packet, err := report.Open(path)
		if err != nil {
			return SyntheticPacketResult{State: Failed, Reason: err.Error()}
		}
		return SyntheticPacketResult{State: Completed, Packet: syntheticPacketView(path, packet)}
	})
}

func syntheticPacketView(path string, packet *report.Packet) *SyntheticPacketView {
	manifest := packet.Manifest
	view := &SyntheticPacketView{
		Folder:                  filepath.Base(path),
		Identity:                packet.Identity,
		Schema:                  manifest.Schema,
		State:                   manifest.State,
		Scenario:                manifest.Scenario,
		Provenance:              manifest.Provenance,
		InputIdentity:           manifest.InputIdentity,
		SpecIdentity:            manifest.SpecIdentity,
		ObservationBoundary:     manifest.ObservationBoundary,
		InputChanged:            manifest.InputChanged,
		ReceiverBehaviorChanged: manifest.ReceiverBehaviorChanged,
		Runs:                    []SyntheticRunView{},
		Files:                   len(manifest.Files),
		Limitations:             append([]string{}, manifest.Limitations...),
	}
	for _, trial := range manifest.Runs {
		view.Runs = append(view.Runs, SyntheticRunView{
			Path: trial.Path, ReceiverMode: string(trial.ReceiverMode), Receiver: trial.ReceiverImplementation,
			Status: string(trial.Status), LedgerCount: trial.LedgerCount, ResultIdentity: trial.ResultIdentity,
		})
	}
	return view
}

// SyntheticRerunRequest names a verified packet, the new folder outside it,
// named in the save dialog, that its runnable copies are prepared in, and the
// numeric loopback address the copies' manual reruns listen and send on.
type SyntheticRerunRequest struct {
	Packet      string `json:"packet"`
	Destination string `json:"destination"`
	Address     string `json:"address"`
}

// SyntheticRunnableSpec is one runnable copy of the historical specification.
type SyntheticRunnableSpec struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// SyntheticRerunView is the preparation `readmit report prepare` records: the
// packet and historical specification it was prepared from, the unchanged
// input identity, the loopback target's hash, the only bindings the copies
// change and each runnable copy's hash. It is not a seal and not a verdict.
type SyntheticRerunView struct {
	Folder                 string                  `json:"folder"`
	Address                string                  `json:"address"`
	PacketIdentity         string                  `json:"packet_identity"`
	HistoricalSpecIdentity string                  `json:"historical_spec_identity"`
	InputIdentity          string                  `json:"input_identity"`
	TargetSHA256           string                  `json:"target_sha256"`
	ChangedBindings        []string                `json:"changed_bindings"`
	Specs                  []SyntheticRunnableSpec `json:"specs"`
}

// SyntheticRerunResult carries one state.
type SyntheticRerunResult struct {
	State  State               `json:"state"`
	Reason string              `json:"reason,omitzero"`
	Rerun  *SyntheticRerunView `json:"rerun,omitzero"`
}

func (r *SyntheticRerunResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PrepareSyntheticRerun is `readmit report prepare`: offline, it verifies the
// packet again and writes a separate mutable workspace outside it with the
// case, a loopback target and runnable copies of the historical
// specification for the baseline, post-fix and reintroduced trials. It never
// edits the sealed packet and opens no connection; a destination inside the
// packet, an existing one and an address that is not numeric loopback are
// refused by the operation itself.
func (a *App) PrepareSyntheticRerun(request SyntheticRerunRequest) SyntheticRerunResult {
	return run(a, false, false, func(context.Context) SyntheticRerunResult {
		if !filepath.IsAbs(request.Packet) {
			return SyntheticRerunResult{State: Failed, Reason: "choose the synthetic packet's folder first"}
		}
		if !filepath.IsAbs(request.Destination) {
			return SyntheticRerunResult{State: Failed, Reason: "name a new folder for the runnable copies in the save dialog first"}
		}
		prepared, err := report.Prepare(request.Packet, request.Destination, request.Address)
		if err != nil {
			return SyntheticRerunResult{State: Failed, Reason: err.Error()}
		}
		view := &SyntheticRerunView{
			Folder:                 filepath.Base(request.Destination),
			Address:                request.Address,
			PacketIdentity:         prepared.PacketIdentity,
			HistoricalSpecIdentity: prepared.HistoricalSpecIdentity,
			InputIdentity:          prepared.InputIdentity,
			TargetSHA256:           prepared.TargetSHA256,
			ChangedBindings:        append([]string{}, prepared.ChangedBindings...),
			Specs:                  []SyntheticRunnableSpec{},
		}
		for _, spec := range prepared.Specs {
			view.Specs = append(view.Specs, SyntheticRunnableSpec{Path: spec.Path, SHA256: spec.SHA256})
		}
		return SyntheticRerunResult{State: Completed, Rerun: view}
	})
}

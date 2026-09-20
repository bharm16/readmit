package report

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/testrunner"
)

type RunnableSpec struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Preparation struct {
	Schema                 string         `json:"schema"`
	PacketIdentity         string         `json:"packet_identity"`
	HistoricalSpecIdentity string         `json:"historical_spec_identity"`
	InputIdentity          string         `json:"input_identity"`
	TargetSHA256           string         `json:"target_sha256"`
	ChangedBindings        []string       `json:"changed_bindings"`
	Specs                  []RunnableSpec `json:"specs"`
}

// Prepare makes an independently identified, writable rerun workspace from a
// verified snapshot. It never changes historical specs or opens a connection.
func Prepare(packetPath, output, address string) (*Preparation, error) {
	if !loopbackAddress(address) {
		return nil, errors.New("report preparation requires a numeric loopback address and port")
	}
	packet, err := Open(packetPath)
	if err != nil {
		return nil, err
	}
	dir, err := reserve(output)
	if err != nil {
		return nil, err
	}
	if err := copyFiles(packet.files, "reproducer/", filepath.Join(dir, "reproducer")); err != nil {
		return nil, err
	}
	targetBytes, err := encode(target(address))
	if err != nil {
		return nil, err
	}
	if err := writeFile(dir, "target.json", targetBytes); err != nil {
		return nil, err
	}
	preparation := &Preparation{Schema: "readmit-report-preparation/v1", PacketIdentity: packet.Identity, HistoricalSpecIdentity: packet.Manifest.SpecIdentity, InputIdentity: packet.Manifest.InputIdentity, TargetSHA256: digest(targetBytes), ChangedBindings: []string{"input.case", "target", "observation.path"}, Specs: []RunnableSpec{}}
	for _, trial := range []string{"baseline", "post-fix", "reintroduced"} {
		if os.Mkdir(filepath.Join(dir, trial), 0700) != nil {
			return nil, errors.New("cannot create runnable trial directory")
		}
		spec, err := testrunner.DecodeSpec(packet.files["spec.json"])
		if err != nil {
			return nil, err
		}
		spec.Input.Case = "../reproducer"
		spec.Target = "../target.json"
		spec.Observation.Path = "observation.json"
		raw, err := encode(spec)
		if err != nil {
			return nil, err
		}
		name := trial + "/spec.json"
		if err := writeFile(dir, name, raw); err != nil {
			return nil, err
		}
		preparation.Specs = append(preparation.Specs, RunnableSpec{Path: name, SHA256: digest(raw)})
	}
	instructions := append([]byte("# Prepared rerun workspace\n\nUse the directory containing the released binary as your working directory in both terminals. Commands below call this prepared workspace rerun; substitute its actual directory name if different. The retained packet is called packet. On Windows PowerShell replace ./readmit with .\\readmit.exe.\n\n"), preparedTrialInstructions(address)...)
	if err := writeFile(dir, "RERUN.md", instructions); err != nil {
		return nil, err
	}
	raw, err := encode(preparation)
	if err != nil {
		return nil, err
	}
	if err := writeFile(dir, "preparation.json", raw); err != nil {
		return nil, err
	}
	if err := writeFile(dir, "preparation.sha256", []byte(digest(raw)+"\n")); err != nil {
		return nil, err
	}
	return preparation, nil
}

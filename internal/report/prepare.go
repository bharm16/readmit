package report

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/strictdoc"
	"github.com/bharm16/readmit/internal/testrunner"
)

const PreparationSchema = "readmit-report-preparation/v1"

var preparationDocument = strictdoc.Document{
	MaxBytes:    1 << 20,
	Schema:      PreparationSchema,
	Required:    []string{"packet_identity", "historical_spec_identity", "input_identity", "target_sha256", "changed_bindings", "specs"},
	Invalid:     "invalid report preparation document",
	TooLarge:    "report preparation document is larger than this release reads",
	MustDeclare: "report preparation must declare its contract version",
	Unsupported: errors.New("unsupported report preparation version"),
	Requires:    "report preparation is missing a required member",
}

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

// ReadPreparation reads the prepared folder's own bounded, strict marker.
// It checks the marker's checksum and declarations, not the runnable evidence
// the marker references; listing a folder is not a verification of its runs.
func ReadPreparation(directory string) (*Preparation, error) {
	invalid := errors.New("invalid or changed report preparation")
	directory, err := artifactpath.Directory(directory)
	if err != nil {
		return nil, invalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, invalid
	}
	defer root.Close()
	raw, err := readPreparationFile(root, directory, "preparation.json", 1<<20)
	if err != nil {
		return nil, invalid
	}
	seal, err := readPreparationFile(root, directory, "preparation.sha256", 65)
	if err != nil || string(seal) != digest(raw)+"\n" {
		return nil, invalid
	}
	var document Preparation
	if err := preparationDocument.Decode(raw, &document); err != nil {
		return nil, err
	}
	if !preparationDigest(document.PacketIdentity) || !preparationDigest(document.HistoricalSpecIdentity) ||
		!preparationDigest(document.InputIdentity) || !preparationDigest(document.TargetSHA256) ||
		!slices.Equal(document.ChangedBindings, []string{"input.case", "target", "observation.path"}) || len(document.Specs) != 3 {
		return nil, invalid
	}
	for index, name := range []string{"baseline/spec.json", "post-fix/spec.json", "reintroduced/spec.json"} {
		if document.Specs[index].Path != name || !preparationDigest(document.Specs[index].SHA256) {
			return nil, invalid
		}
	}
	return &document, nil
}

func readPreparationFile(root *os.Root, directory, name string, limit int) ([]byte, error) {
	info, err := os.Lstat(filepath.Join(directory, name))
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("not a bounded regular preparation file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("preparation file changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, errors.New("preparation file exceeds its read limit")
	}
	return raw, nil
}

func preparationDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
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
	workspace, err := artifactdir.Create(output, preparationFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer workspace.Close()
	if err := copyFiles(workspace, packet.files, "reproducer/", "reproducer"); err != nil {
		return nil, err
	}
	targetBytes, err := encode(target(address))
	if err != nil {
		return nil, err
	}
	if err := workspace.WriteFile("target.json", targetBytes); err != nil {
		return nil, err
	}
	preparation := &Preparation{Schema: PreparationSchema, PacketIdentity: packet.Identity, HistoricalSpecIdentity: packet.Manifest.SpecIdentity, InputIdentity: packet.Manifest.InputIdentity, TargetSHA256: digest(targetBytes), ChangedBindings: []string{"input.case", "target", "observation.path"}, Specs: []RunnableSpec{}}
	for _, trial := range []string{"baseline", "post-fix", "reintroduced"} {
		if workspace.Mkdir(trial) != nil {
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
		if err := workspace.WriteFile(name, raw); err != nil {
			return nil, err
		}
		preparation.Specs = append(preparation.Specs, RunnableSpec{Path: name, SHA256: digest(raw)})
	}
	instructions := append([]byte("# Prepared rerun workspace\n\nUse the directory containing the released binary as your working directory in both terminals. Commands below call this prepared workspace rerun; substitute its actual directory name if different. The retained packet is called packet. On Windows PowerShell replace ./readmit with .\\readmit.exe.\n\n"), preparedTrialInstructions(address)...)
	if err := workspace.WriteFile("RERUN.md", instructions); err != nil {
		return nil, err
	}
	raw, err := encode(preparation)
	if err != nil {
		return nil, err
	}
	if err := workspace.WriteFile("preparation.json", raw); err != nil {
		return nil, err
	}
	if _, err := workspace.Seal(nil); err != nil {
		return nil, err
	}
	return preparation, nil
}

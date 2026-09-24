package desktop

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
)

// MaxInspectionRows bounds one window of a raw inspection: the rows the window
// shows at once, the same bound the message grid and a scan window render
// under. A file is inspected whole every time and paged by row, so a large
// file costs the window one page of rows, never every field it holds.
const MaxInspectionRows = 200

// MaxInspectionValueBytes bounds how many of one field's bytes a window shows
// when values are asked for. A longer field is shown escaped in part and says
// so, so one field of a large file cannot carry the file into the window; the
// command line prints it whole.
const MaxInspectionValueBytes = 4096

// InspectionPathResult is one native dialog answer for the raw-inspection
// screen: the file to inspect, or the folder its byte-identical copy is
// written into.
type InspectionPathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Kind   string `json:"kind,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *InspectionPathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RawInspectionRequest declares one inspection the way `readmit inspect`
// does: the file, the framing and terminator (each auto, meaning detected),
// and whether values are shown. Offset and Limit select the window of rows;
// a zero Limit is the whole bound. Expect, when set, is the digest the
// earlier pages were read from: a later page of a file that changed since is
// refused rather than joined to rows of a different file.
type RawInspectionRequest struct {
	File       string `json:"file"`
	Format     string `json:"format"`
	Terminator string `json:"terminator"`
	ShowValues bool   `json:"show_values"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	Expect     string `json:"expect,omitzero"`
}

// RawInspection is what `readmit inspect` reports about the file, one window
// of its rows at a time. Total counts every row the command prints; Rows are
// the ones from Offset, at most Limit of them, and a value is cut at or before
// ValueBytes bytes. Each row's ValueShownBytes is the actual byte count after
// respecting a UTF-8 character boundary. Bytes and SHA256 are the length and
// digest of the bytes read, so a person can see the source was read and not
// changed.
type RawInspection struct {
	Format              string                    `json:"format"`
	FormatSelection     string                    `json:"format_selection"`
	TerminatorSelection string                    `json:"terminator_selection"`
	Messages            int                       `json:"messages"`
	Bytes               int                       `json:"bytes"`
	SHA256              string                    `json:"sha256"`
	ShowValues          bool                      `json:"show_values"`
	Offset              int                       `json:"offset"`
	Limit               int                       `json:"limit"`
	ValueBytes          int                       `json:"value_bytes"`
	Total               int                       `json:"total"`
	Rows                []operation.InspectionRow `json:"rows"`
}

// RawInspectionResult carries one state. Inspection is present only when the
// file parsed under the declarations.
type RawInspectionResult struct {
	State      State          `json:"state"`
	Reason     string         `json:"reason,omitzero"`
	Inspection *RawInspection `json:"inspection,omitzero"`
}

func (r *RawInspectionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RoundTripRequest names the file, the declarations it is parsed under, and
// the new entry of a natively chosen folder its byte-identical copy is
// written to.
type RoundTripRequest struct {
	File       string `json:"file"`
	Format     string `json:"format"`
	Terminator string `json:"terminator"`
	Folder     string `json:"folder"`
	Name       string `json:"name"`
}

// RoundTripResult reports the copy that was written: where, how many bytes
// and their digest, which is the digest of the source bytes it copies.
type RoundTripResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Path   string `json:"path,omitzero"`
	Bytes  int    `json:"bytes,omitzero"`
	SHA256 string `json:"sha256,omitzero"`
}

func (r *RoundTripResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// The paths ChooseInspectionPath chooses.
const (
	inspectionFile  = "file"
	roundTripFolder = "round-trip-folder"
)

// ChooseInspectionPath presents the host's native dialog for the file to
// inspect ("file") or the folder a byte-identical copy is written into
// ("round-trip-folder"). Choosing reads and writes nothing.
func (a *App) ChooseInspectionPath(kind string) InspectionPathResult {
	return run(a, true, false, func(ctx context.Context) InspectionPathResult {
		switch kind {
		case inspectionFile:
			path, declined := a.chooseOneFile(ctx, "Choose the HL7 file to inspect")
			if path == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: path}
		case roundTripFolder:
			folder, declined := a.chooseFolder(ctx, "Choose the folder for the byte-identical copy")
			if folder == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: folder}
		default:
			return InspectionPathResult{State: Failed, Reason: "unknown inspection path kind"}
		}
	})
}

// InspectRawFile is `readmit inspect` for the window: the file is read whole,
// parsed under the declared framing and terminator by the same shared
// operation, and reported as the command reports it, one bounded window of
// rows at a time. The source is opened for reading only and nothing is
// imported, written or retained: a later page reads the file again. It runs
// to completion once it starts, so it holds the operation slot but is not
// interruptible.
func (a *App) InspectRawFile(request RawInspectionRequest) RawInspectionResult {
	return run(a, false, false, func(context.Context) RawInspectionResult {
		options, declined := inspectionOptions(request.File, request.Format, request.Terminator)
		if declined.state != "" {
			return RawInspectionResult{State: declined.state, Reason: declined.reason}
		}
		limit := request.Limit
		if limit == 0 {
			limit = MaxInspectionRows
		}
		if request.Offset < 0 || limit < 0 || limit > MaxInspectionRows {
			return RawInspectionResult{State: Failed, Reason: "an inspection window begins at or after the first row and shows at most " + strconv.Itoa(MaxInspectionRows) + " rows"}
		}
		inspected, err := operation.InspectFile(request.File, options, "")
		if err != nil {
			return RawInspectionResult{State: refusalState(err), Reason: err.Error()}
		}
		digest := inspected.Digest()
		if request.Expect != "" && request.Expect != digest {
			return RawInspectionResult{State: Failed, Reason: "the file changed since its earlier rows were read; inspect it again"}
		}
		view := &RawInspection{
			Format:              string(inspected.Format),
			FormatSelection:     inspected.FormatSelection,
			TerminatorSelection: inspected.TerminatorSelection,
			Messages:            inspected.Messages(),
			Bytes:               inspected.Bytes(),
			SHA256:              digest,
			ShowValues:          request.ShowValues,
			Offset:              request.Offset,
			Limit:               limit,
			ValueBytes:          MaxInspectionValueBytes,
			Rows:                []operation.InspectionRow{},
		}
		inspected.Rows(func(row operation.InspectionRow) bool {
			if view.Total >= request.Offset && len(view.Rows) < limit {
				if request.ShowValues {
					row.Value, row.ValueTruncated, row.ValueShownBytes = inspected.Value(row, MaxInspectionValueBytes)
				}
				view.Rows = append(view.Rows, row)
			}
			view.Total++
			return true
		})
		if request.Offset > 0 && request.Offset >= view.Total {
			return RawInspectionResult{State: Failed, Reason: "the inspection window begins after the last row"}
		}
		return RawInspectionResult{State: Completed, Inspection: view}
	})
}

// WriteRoundTrip is `readmit inspect --roundtrip`: the file is parsed under
// the declarations by the same shared operation, and only once it parsed are
// its bytes written, exactly, to one new entry of the chosen folder. An
// existing entry is never overwritten and the source is never changed. It
// writes nothing a license governs, as the command does not, so it is
// admitted without a term.
func (a *App) WriteRoundTrip(request RoundTripRequest) RoundTripResult {
	return run(a, false, false, func(context.Context) RoundTripResult {
		options, declined := inspectionOptions(request.File, request.Format, request.Terminator)
		if declined.state != "" {
			return RoundTripResult{State: declined.state, Reason: declined.reason}
		}
		root, declined := chosenFolder(request.Folder)
		if root == "" {
			return RoundTripResult{State: declined.state, Reason: declined.reason}
		}
		if artifactpath.EntryName(request.Name) != nil {
			return RoundTripResult{State: Failed, Reason: "the copy is one new entry of the chosen folder, named by one valid file name"}
		}
		destination := filepath.Join(root, request.Name)
		inspected, err := operation.InspectFile(request.File, options, destination)
		if err != nil {
			return RoundTripResult{State: refusalState(err), Reason: err.Error()}
		}
		return RoundTripResult{State: Completed, Path: destination, Bytes: inspected.Bytes(), SHA256: inspected.Digest()}
	})
}

// inspectionOptions checks the declarations through the command's own check,
// with its sentences, before anything is read. The file is named absolutely:
// the window has no working directory a relative name could mean.
func inspectionOptions(file, format, terminator string) (hl7.Options, refusal) {
	if !filepath.IsAbs(file) {
		return hl7.Options{}, refusal{Failed, "choose the file with the file dialog; an inspection reads one file named by its full path"}
	}
	options, err := operation.InspectOptions(format, terminator)
	if err != nil {
		return hl7.Options{}, refusal{Failed, err.Error()}
	}
	return options, refusal{}
}

// refusalState separates a file this account may not read, or a folder it may
// not write, from every other refusal, by the class of the error the shared
// operation returned. It touches no file: reopening one to find out could
// block on a pipe. The reason stays the operation's own sentence.
func refusalState(err error) State {
	if errors.Is(err, fs.ErrPermission) {
		return PermissionDenied
	}
	return Failed
}

// chosenFolder resolves a folder the host's dialog chose as a destination.
// It is the workspace rule — an existing folder, never a symbolic link — said
// about the folder that was chosen, since these screens have no workspace.
func chosenFolder(path string) (string, refusal) {
	root, declined := resolveFolder(path)
	if root == "" && declined.state == Failed {
		declined.reason = "the chosen folder must be an existing folder that is not a symbolic link"
	}
	return root, declined
}

// chooseOneFile presents the host's file dialog for exactly one file, with no
// filter, so a file without an extension can be chosen too. A dialog that
// returns several is refused rather than read for its first, so the window
// never inspects or scans a file the person did not single out.
func (a *App) chooseOneFile(ctx context.Context, title string) (string, refusal) {
	files, declined := a.chooseFiles(ctx, title, "", "")
	if len(files) == 0 {
		return "", declined
	}
	if len(files) != 1 {
		return "", refusal{Failed, "choose exactly one file"}
	}
	return files[0], refusal{}
}

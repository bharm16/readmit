package desktop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/operation"
)

// MaxInspectionRows bounds one window of a standalone file's message list, the
// same bound the case message list renders under.
const MaxInspectionRows = 200

// InspectionPathResult is one native dialog answer for the standalone file
// reader: the file to open, or the new file its byte-identical copy is saved
// as.
type InspectionPathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Kind   string `json:"kind,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *InspectionPathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// The paths ChooseInspectionPath chooses.
const (
	inspectionFile             = "file"
	referenceCatalogFile       = "reference-catalog"
	referenceLibraryFolder     = "reference-library"
	referenceProfileFile       = "reference-profile"
	referencePackFile          = "reference-pack"
	referenceDocumentationFile = "reference-documentation"
	copyDestination            = "copy-destination"
)

// ChooseInspectionPath presents the host's native dialog for the file to open
// ("file"), or its save dialog for the new file a byte-identical copy of
// source is written as ("copy-destination"), offering the source's own file
// name. Choosing reads and writes nothing.
func (a *App) ChooseInspectionPath(kind, source string) InspectionPathResult {
	return run(a, true, false, func(ctx context.Context) InspectionPathResult {
		switch kind {
		case referenceProfileFile, referencePackFile, referenceDocumentationFile:
			titles := map[string]string{referenceProfileFile: "Open local HL7 profile", referencePackFile: "Open pinned HL7 profile pack", referenceDocumentationFile: "Open local reference documentation"}
			path, declined := a.chooseOneFile(ctx, titles[kind])
			if path == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: path}
		case referenceLibraryFolder:
			path, declined := a.chooseFolder(ctx, "Install HL7 reference library")
			if path == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: path}
		case referenceCatalogFile:
			path, declined := a.chooseOneFile(ctx, "Open offline HL7 reference catalog")
			if path == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: path}
		case inspectionFile:
			path, declined := a.chooseOneFile(ctx, "Open HL7 file")
			if path == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: path}
		case copyDestination:
			name := ""
			if filepath.IsAbs(source) {
				name = filepath.Base(source)
			}
			path, declined := a.chooseNamedDestination(ctx, "Save copy", name)
			if path == "" {
				return InspectionPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
			}
			return InspectionPathResult{State: Completed, Kind: kind, Path: path}
		default:
			return InspectionPathResult{State: Failed, Reason: "unknown inspection path kind"}
		}
	})
}

// FileMessagesRequest lists the messages of one file named by its full path,
// parsed under the declared framing and terminator (each auto, meaning
// detected). Offset and Limit select the window of messages; a zero Limit is
// the whole bound.
type FileMessagesRequest struct {
	File       string `json:"file"`
	Format     string `json:"format"`
	Terminator string `json:"terminator"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
}

// FileMessage is one message of a standalone file: its zero-based index, its
// parsed MSH-9 code and trigger, escaped and bounded and empty where it
// declares none, and its half-open byte range in the file, MLLP included.
type FileMessage struct {
	Index        int    `json:"index"`
	MessageCode  string `json:"message_code"`
	TriggerEvent string `json:"trigger_event"`
	Start        int    `json:"start"`
	End          int    `json:"end"`
}

// FileMessagesResult is one window of a file's messages. Name, Bytes and
// SHA256 describe the bytes read and are present whenever the file was read,
// including when it did not parse under the declarations: the state is then
// failed with the parser's reason, and ReadFileBytes still shows its original
// bytes. Format and Terminator are what the parser used, whether declared or
// detected, as FormatSelection and TerminatorSelection say.
type FileMessagesResult struct {
	State               State         `json:"state"`
	Reason              string        `json:"reason,omitzero"`
	Name                string        `json:"name"`
	Bytes               int           `json:"bytes"`
	SHA256              string        `json:"sha256"`
	Format              string        `json:"format"`
	Terminator          string        `json:"terminator"`
	FormatSelection     string        `json:"format_selection"`
	TerminatorSelection string        `json:"terminator_selection"`
	Total               int           `json:"total"`
	Offset              int           `json:"offset"`
	Rows                []FileMessage `json:"rows"`
}

func (r *FileMessagesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListFileMessages reads one file whole, parses it by the same parser and
// declarations `readmit inspect` uses, and lists one window of its messages.
// Nothing is imported, written or retained; a later window reads the file
// again. It holds the operation slot but is not interruptible.
func (a *App) ListFileMessages(request FileMessagesRequest) FileMessagesResult {
	return runRead(a, false, func(context.Context) FileMessagesResult {
		fail := func(state State, reason string) FileMessagesResult {
			return FileMessagesResult{State: state, Reason: reason, Rows: []FileMessage{}}
		}
		options, declined := inspectionOptions(request.File, request.Format, request.Terminator)
		if declined.state != "" {
			return fail(declined.state, declined.reason)
		}
		limit := request.Limit
		if limit == 0 {
			limit = MaxInspectionRows
		}
		if request.Offset < 0 || limit < 1 || limit > MaxInspectionRows {
			return fail(Failed, "a message window begins at or after the first message and lists at most "+strconv.Itoa(MaxInspectionRows)+" of them")
		}
		data, err := operation.ReadInputFile(request.File, hl7.MaxInputBytes)
		if err != nil {
			return fail(refusalState(err), err.Error())
		}
		result := FileMessagesResult{State: Completed, Name: filepath.Base(request.File), Bytes: len(data), SHA256: digestOf(data),
			FormatSelection: selectionOf(request.Format), TerminatorSelection: selectionOf(request.Terminator), Rows: []FileMessage{}}
		document, err := hl7.Parse(data, options)
		if err != nil {
			result.State, result.Reason = Failed, desktopParseReason(err)
			return result
		}
		result.Format, result.Total, result.Offset = string(document.Format), len(document.Messages), request.Offset
		if len(document.Messages) > 0 {
			result.Terminator = string(document.Messages[0].Terminator)
		}
		if request.Offset > 0 && request.Offset >= result.Total {
			return fail(Failed, "the message window begins after the last message")
		}
		for i := request.Offset; i < min(result.Total, request.Offset+limit); i++ {
			code, trigger := messageType(document, i)
			span := document.Messages[i].Span
			result.Rows = append(result.Rows, FileMessage{Index: i, MessageCode: code, TriggerEvent: trigger, Start: span.Start, End: span.End})
		}
		return result
	})
}

// FileInspectRequest inspects one message of a standalone file with the same
// inspector a case occurrence is inspected with. Expect is the digest the
// message list was read from; a file that changed since is refused rather
// than joined to a list of a different file. Message is zero-based.
type FileInspectRequest struct {
	Grid               *InspectionGridRequest `json:"grid,omitzero"`
	ReferenceSelection *HL7ReferenceSelection `json:"reference_selection,omitzero"`
	ReferenceIdentity  string                 `json:"reference_identity,omitzero"`
	ReferenceCatalog   string                 `json:"reference_catalog,omitzero"`
	File               string                 `json:"file"`
	Format             string                 `json:"format"`
	Terminator         string                 `json:"terminator"`
	Expect             string                 `json:"expect"`
	Message            int                    `json:"message"`
	Path               string                 `json:"path"`
	NodeOffset         int                    `json:"node_offset"`
	ByteOffset         int                    `json:"byte_offset"`
	RawOffset          int                    `json:"raw_offset"`
	Reveal             bool                   `json:"reveal"`
	MaskPHI            bool                   `json:"mask_phi,omitzero"`
}

// InspectFileMessage inspects one message of a file named by its full path.
// Offsets are within the file. Identity is the file's digest. Nothing is
// written; it holds the operation slot but is not interruptible.
func (a *App) InspectFileMessage(request FileInspectRequest) InspectionResult {
	return runRead(a, false, func(context.Context) InspectionResult {
		fail := func(state State, reason string) InspectionResult {
			return InspectionResult{State: state, Reason: reason}
		}
		if request.NodeOffset < 0 || request.ByteOffset < -1 || request.RawOffset < -1 {
			return fail(Failed, "inspector offsets must be in range")
		}
		data, declined := expectedFile(request.File, request.Expect)
		if declined.state != "" {
			return fail(declined.state, declined.reason)
		}
		options, declined := inspectionOptions(request.File, request.Format, request.Terminator)
		if declined.state != "" {
			return fail(declined.state, declined.reason)
		}
		document, err := hl7.Parse(data, options)
		if err != nil {
			return fail(Failed, desktopParseReason(err))
		}
		view, reason := a.inspectDocument(data, document, request.Message, inspectorWindow{
			Path: request.Path, NodeOffset: request.NodeOffset, ByteOffset: request.ByteOffset, RawOffset: request.RawOffset, Reveal: request.Reveal, MaskPHI: request.MaskPHI, ReferenceCatalog: request.ReferenceCatalog, ReferenceIdentity: request.ReferenceIdentity, ReferenceSelection: request.ReferenceSelection, Grid: request.Grid,
		})
		if view == nil {
			return fail(Failed, reason)
		}
		view.Identity = request.Expect
		return InspectionResult{State: Completed, Inspection: view}
	})
}

// FileBytesRequest pages the original bytes of a file, whether or not it
// parses. Offset is read from the start of the hex row that holds it.
type FileBytesRequest struct {
	File   string `json:"file"`
	Expect string `json:"expect"`
	Offset int    `json:"offset"`
	Reveal bool   `json:"reveal"`
}

// FileBytesResult is one window of InspectorByteWindow original bytes as hex
// rows. Bytes is the file's length.
type FileBytesResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Bytes  int      `json:"bytes"`
	Offset int      `json:"offset"`
	Rows   []HexRow `json:"rows"`
}

func (r *FileBytesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReadFileBytes shows one window of a file's original bytes, which a file that
// did not parse still has. The printable column is present only when
// revealed. Nothing is written.
func (a *App) ReadFileBytes(request FileBytesRequest) FileBytesResult {
	return runRead(a, false, func(context.Context) FileBytesResult {
		fail := func(state State, reason string) FileBytesResult {
			return FileBytesResult{State: state, Reason: reason, Rows: []HexRow{}}
		}
		if request.Offset < 0 {
			return fail(Failed, "a byte window begins at or after the first byte")
		}
		data, declined := expectedFile(request.File, request.Expect)
		if declined.state != "" {
			return fail(declined.state, declined.reason)
		}
		if request.Offset > len(data) {
			return fail(Failed, "the byte window begins after the last byte")
		}
		offset := request.Offset - request.Offset%HexRowBytes
		return FileBytesResult{State: Completed, Bytes: len(data), Offset: offset, Rows: hexRows(data, offset, InspectorByteWindow, request.Reveal)}
	})
}

// SaveCopyRequest saves a byte-identical copy of the file, parsed under the
// declarations, as the new file Destination the save dialog named. Expect is
// the digest the file was listed with.
type SaveCopyRequest struct {
	File        string `json:"file"`
	Format      string `json:"format"`
	Terminator  string `json:"terminator"`
	Expect      string `json:"expect"`
	Destination string `json:"destination"`
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

// SaveFileCopy is `readmit inspect --roundtrip` for the standalone reader: the
// file is read again, checked against the digest it was listed with and
// parsed under the declarations, and only then are its exact bytes written,
// exclusively, as one new file. The source itself, by any name, an existing
// destination and a source that changed are refused, and the source is never
// changed. It writes nothing a license governs, as the command does not, so it
// is admitted without a term.
func (a *App) SaveFileCopy(request SaveCopyRequest) RoundTripResult {
	return run(a, false, false, func(context.Context) RoundTripResult {
		fail := func(state State, reason string) RoundTripResult { return RoundTripResult{State: state, Reason: reason} }
		options, declined := inspectionOptions(request.File, request.Format, request.Terminator)
		if declined.state != "" {
			return fail(declined.state, declined.reason)
		}
		if !filepath.IsAbs(request.Destination) {
			return fail(Failed, "choose the copy's destination with the save dialog")
		}
		root, declined := chosenFolder(filepath.Dir(request.Destination))
		if root == "" {
			return fail(declined.state, declined.reason)
		}
		name := filepath.Base(request.Destination)
		if artifactpath.EntryName(name) != nil {
			return fail(Failed, "the copy is one new file named by one valid file name")
		}
		destination := filepath.Join(root, name)
		if aliases(request.File, destination) {
			return fail(Failed, "the copy cannot be written over the file it copies; choose another name")
		}
		if _, err := os.Lstat(destination); err == nil {
			return fail(Failed, "a file with that name already exists; choose a new name")
		}
		data, declined := expectedFile(request.File, request.Expect)
		if declined.state != "" {
			return fail(declined.state, declined.reason)
		}
		document, err := hl7.Parse(data, options)
		if err != nil {
			return fail(Failed, desktopParseReason(err))
		}
		if err := operation.WriteNewFile(destination, document.Serialize(),
			"cannot create round-trip file; destination must be new and writable",
			"cannot write round-trip file"); err != nil {
			return fail(refusalState(err), err.Error())
		}
		return RoundTripResult{State: Completed, Path: destination, Bytes: len(data), SHA256: digestOf(data)}
	})
}

// aliases reports whether destination names the source file: the same cleaned
// path, the same path once links are resolved, or the same file by identity.
func aliases(source, destination string) bool {
	if filepath.Clean(source) == filepath.Clean(destination) {
		return true
	}
	resolvedSource, sourceErr := filepath.EvalSymlinks(source)
	resolvedDestination, destinationErr := filepath.EvalSymlinks(destination)
	if sourceErr == nil && destinationErr == nil && resolvedSource == resolvedDestination {
		return true
	}
	sourceInfo, sourceErr := os.Stat(source)
	destinationInfo, destinationErr := os.Stat(destination)
	return sourceErr == nil && destinationErr == nil && os.SameFile(sourceInfo, destinationInfo)
}

// expectedFile reads a file named by its full path and refuses it unless its
// digest is the one the window displayed.
func expectedFile(path, expect string) ([]byte, refusal) {
	if !filepath.IsAbs(path) {
		return nil, refusal{Failed, "choose the file with the file dialog; an inspection reads one file named by its full path"}
	}
	if expect == "" {
		return nil, refusal{Failed, "list the file's messages before inspecting it"}
	}
	data, err := operation.ReadInputFile(path, hl7.MaxInputBytes)
	if err != nil {
		return nil, refusal{refusalState(err), err.Error()}
	}
	if digestOf(data) != expect {
		return nil, refusal{Failed, "the file changed since it was opened; open it again"}
	}
	return data, refusal{}
}

// selectionOf says how a framing or terminator reached the parser.
func selectionOf(declared string) string {
	if declared == "" || declared == "auto" {
		return operation.InspectDetected
	}
	return operation.InspectDeclared
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

// The parser owns offsets and refusal semantics. Only its CLI recovery wording
// is translated for the desktop; no missing or mixed bytes are normalized.
func desktopParseReason(err error) string {
	var terminator *hl7.TerminatorError
	if !errors.As(err, &terminator) {
		return err.Error()
	}
	reason := ""
	switch terminator.Problem {
	case hl7.MixedTerminators:
		reason = "mixed segment terminators; supply a uniformly terminated source. File format can select its terminator but cannot repair mixed line endings"
	case hl7.MissingTerminator:
		reason = "missing segment terminator; open a source with its original final terminator. File format cannot add missing bytes"
	case hl7.MismatchedTerminator:
		reason = "segment terminator does not match the selection; choose the matching segment terminator in File format"
	default:
		return err.Error()
	}
	return fmt.Sprintf("invalid input at byte %d: %s", terminator.Offset, reason)
}

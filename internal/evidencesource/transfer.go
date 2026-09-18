package evidencesource

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/secret"
)

// The two verbs readmit appends to the operator's declared arguments. They are
// the whole of the transfer contract: `list` prints one line per entry as a
// decimal byte count, a tab and the entry's name, and `get NAME` streams that
// entry's bytes on standard output. A program that answers anything else is a
// program readmit could not read, which is an error and never an empty source.
const (
	transferList = "list"
	transferGet  = "get"
)

const (
	// TransferTimeout bounds one invocation of the declared transfer program.
	// It is the bound a replay target's timeouts are held to. A program that
	// has not answered by then leaves the source unavailable — never empty, and
	// never a pass.
	TransferTimeout = 5 * time.Minute
	// maxListingBytes bounds the listing one program may print. A listing past
	// it is refused rather than truncated, because a truncated listing is a
	// source readmit would report fewer entries for than it holds.
	maxListingBytes = 1 << 20
	// transferWaitDelay is how long a stopped transfer is given to exit and
	// release its output before readmit closes it. The timer starts only once
	// the collection is already stopping, and without it a cancellation would
	// be acknowledged when the program's own children happened to finish
	// rather than in a moment: a transfer program that starts a child leaves
	// that child holding the pipe readmit is reading, so killing the program
	// alone does not end the read.
	transferWaitDelay = time.Second
)

// errTooManyEntries refuses a listing past the bound one collection reads,
// whichever kind produced it. A listing past it is refused rather than
// truncated: a truncated listing is a source readmit would report fewer entries
// for than it holds.
var errTooManyEntries = errors.New("the declared source holds more entries than one collection reads; collect it in parts")

// listedEntry is one entry the source declares: the single local name it is
// known by and the number of bytes the source says it holds. A declared size is
// what a quota is applied to before anything is read; it is never taken as
// proof of what a read will return.
type listedEntry struct {
	name string
	size int64
}

// listing is everything one source declared. notRead counts what the source
// named and readmit deliberately did not open — a subdirectory, a symbolic
// link, a device — so a collection never implies it saw more than it did.
type listing struct {
	entries []listedEntry
	notRead int
}

// selection is what the plan's declared members select out of this listing: how
// many entries, how many bytes they declare in total, and the largest single
// one. It is the one computation a quota is applied to, so the diagnosis that
// reports a source as within its quota and the collection that refuses it are
// answering the same question rather than two similar ones.
func (l listing) selection(plan importer.Plan) (entries int, total, largest int64) {
	for _, entry := range l.entries {
		if !plan.Selects(entry.name) {
			continue
		}
		entries, total = entries+1, total+entry.size
		if entry.size > largest {
			largest = entry.size
		}
	}
	return entries, total, largest
}

// list obtains the source's own listing. A listing that could not be obtained
// is an error: a source readmit could not list is not a source with no entries,
// and reporting one as the other is exactly the confusion this package exists
// to prevent.
func list(ctx context.Context, source Source) (listing, error) {
	if source.Kind == Directory {
		return listDirectory(source)
	}
	return listTransfer(ctx, source)
}

// listDirectory reads the entries of one directory. A collection reads one
// directory's own entries: anything beneath a subdirectory is a source of its
// own, and an entry that is not regular file bytes is counted rather than
// followed, so listing a tree can never leave it, loop, or block on a device.
func listDirectory(source Source) (listing, error) {
	root, err := artifactpath.Directory(source.Root)
	if err != nil {
		return listing{}, errors.New("the declared source root must be a directory that is not a symbolic link")
	}
	found, err := os.ReadDir(root)
	if err != nil {
		return listing{}, errors.New("the declared source root could not be listed")
	}
	result := listing{}
	for _, entry := range found {
		if !entry.Type().IsRegular() || artifactpath.EntryName(entry.Name()) != nil {
			result.notRead++
			continue
		}
		info, err := entry.Info()
		if err != nil {
			result.notRead++
			continue
		}
		result.entries = append(result.entries, listedEntry{name: entry.Name(), size: info.Size()})
	}
	if len(result.entries) > MaxEntries {
		return listing{}, errTooManyEntries
	}
	return result, nil
}

// listTransfer runs the declared program's list verb and reads the listing it
// prints. Every name it returns must be one local name inside the source, so a
// listing can never name a path that would be staged outside the collection.
func listTransfer(ctx context.Context, source Source) (listing, error) {
	var out bytes.Buffer
	if err := run(ctx, source, &out, maxListingBytes, transferList); err != nil {
		return listing{}, errors.New("the declared transfer program did not list the source")
	}
	result := listing{}
	scanner := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	scanner.Buffer(make([]byte, 0, 4096), 4096)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		size, name, found := strings.Cut(line, "\t")
		declared, err := strconv.ParseInt(size, 10, 64)
		if !found || err != nil || declared < 0 {
			return listing{}, errors.New("the declared transfer program printed a listing readmit cannot read")
		}
		if artifactpath.EntryName(name) != nil {
			return listing{}, errors.New("the declared transfer program named an entry that is not one name inside the source")
		}
		result.entries = append(result.entries, listedEntry{name: name, size: declared})
		if len(result.entries) > MaxEntries {
			return listing{}, errTooManyEntries
		}
	}
	if scanner.Err() != nil {
		return listing{}, errors.New("the declared transfer program printed a listing readmit cannot read")
	}
	return result, nil
}

// open begins reading one entry. The returned stream belongs to the caller,
// which closes it; nothing here holds the entry.
func open(ctx context.Context, source Source, entry listedEntry) (io.ReadCloser, error) {
	if source.Kind == Directory {
		return openLocal(source, entry)
	}
	return openTransfer(ctx, source, entry)
}

// openLocal opens one named entry of the resolved source root and re-checks the
// opened file, so a path that changed underneath is refused rather than read.
func openLocal(source Source, entry listedEntry) (io.ReadCloser, error) {
	root, err := artifactpath.Directory(source.Root)
	if err != nil {
		return nil, errors.New("the declared source root could not be resolved")
	}
	if err := artifactpath.EntryName(entry.name); err != nil {
		return nil, err
	}
	file, err := os.Open(artifactpath.JoinReference(root, entry.name))
	if err != nil {
		return nil, errors.New("the source entry could not be opened")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("a source entry must be a regular file")
	}
	return file, nil
}

// openTransfer runs the declared program's get verb and hands back its standard
// output. The process is bounded by the same timeout the listing is, and is
// killed when the stream is closed, so a cancelled collection leaves no
// transfer running behind it.
func openTransfer(ctx context.Context, source Source, entry listedEntry) (io.ReadCloser, error) {
	if err := artifactpath.EntryName(entry.name); err != nil {
		return nil, err
	}
	command, stop, err := prepare(ctx, source, transferGet, entry.name)
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stop()
		return nil, errors.New("the declared transfer program could not be read")
	}
	if err := command.Start(); err != nil {
		stop()
		return nil, errors.New("the declared transfer program could not be started")
	}
	return &transferStream{stdout: stdout, command: command, stop: stop}, nil
}

// transferStream is one running transfer. Closing a stream that was read to its
// end waits for the program and reports a nonzero exit, so an entry whose
// transfer failed after printing some bytes is a failed read rather than a
// short one that looked complete.
type transferStream struct {
	stdout  io.ReadCloser
	command *exec.Cmd
	stop    func()
	ended   bool
}

func (t *transferStream) Read(p []byte) (int, error) {
	n, err := t.stdout.Read(p)
	if err == io.EOF {
		t.ended = true
	}
	return n, err
}

func (t *transferStream) Close() error {
	if !t.ended {
		// The caller stopped reading and already holds the reason. Stopping the
		// program before waiting for it means a cancelled or refused entry
		// never leaves a transfer running behind it, and never blocks on one
		// that is still printing.
		t.stop()
		t.command.Wait()
		return nil
	}
	err := t.command.Wait()
	t.stop()
	if err != nil {
		return errors.New("the declared transfer program did not complete the entry")
	}
	return nil
}

// prepare builds one invocation of the declared program and the function that
// releases it. The program is run by the absolute path it declares, never
// looked up on PATH, and never through a shell.
//
// A declared credential is resolved here and written to the program's standard
// input followed by one newline, and nowhere else. It is never an argument,
// because an argument is visible to every process on the machine; it is never
// an environment variable readmit sets; it is never written to a file; and it
// is carried by [secret.Value], which masks itself under every formatting verb
// and refuses to be serialized, so it cannot reach an artifact by accident.
func prepare(ctx context.Context, source Source, verb string, arguments ...string) (*exec.Cmd, func(), error) {
	path, err := artifactpath.Resolve(source.Command)
	if err != nil {
		return nil, nil, errors.New("the declared transfer program cannot be resolved")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("the declared transfer program must be a regular file")
	}
	var value secret.Value
	if source.Declared() {
		document, err := secret.ReadStore(source.SecretsFile)
		if err != nil {
			return nil, nil, errors.New("the declared secret reference document could not be read")
		}
		reference, err := secret.Bind(document, source.Credential, secret.SourceEndpoint, source.Address)
		if err != nil {
			return nil, nil, errors.New("the declared credential reference is not registered for this source's purpose and address")
		}
		if value, err = secret.Resolve(ctx, reference); err != nil {
			return nil, nil, errors.New("the credential could not be read from its declared store")
		}
	}
	bounded, cancel := context.WithTimeout(ctx, TransferTimeout)
	declared := append(append([]string{}, source.Arguments...), verb)
	command := exec.CommandContext(bounded, path, append(declared, arguments...)...)
	if source.Declared() {
		command.Stdin = bytes.NewReader(append(value.Expose(), '\n'))
	}
	// The program's own diagnostics are discarded, so a transfer that fails
	// noisily cannot write a credential or a patient value into readmit's
	// output. What went wrong is reported as the bounded diagnostic below it.
	command.Stderr = io.Discard
	command.WaitDelay = transferWaitDelay
	return command, cancel, nil
}

// run performs one bounded invocation that prints to a buffer. A program that
// prints more than the bound is stopped rather than read further.
func run(ctx context.Context, source Source, out *bytes.Buffer, limit int, verb string, arguments ...string) error {
	command, stop, err := prepare(ctx, source, verb, arguments...)
	if err != nil {
		return err
	}
	defer stop()
	command.Stdout = &boundedWriter{to: out, remaining: limit}
	return command.Run()
}

// boundedWriter stops a program that streams more than one listing's worth of
// output, so a hostile or misconfigured program cannot exhaust memory here.
type boundedWriter struct {
	to        *bytes.Buffer
	remaining int
}

func (b *boundedWriter) Write(data []byte) (int, error) {
	if len(data) > b.remaining {
		return 0, errors.New("the declared transfer program printed more than one listing")
	}
	b.remaining -= len(data)
	return b.to.Write(data)
}

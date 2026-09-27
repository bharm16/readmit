package lifecycle

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/backup"
)

// RetireState is what one retirement did to its source.
type RetireState string

const (
	// Retained: the source is where it was and nothing of it was removed.
	Retained RetireState = "retained"
	// Deleted: the source was removed from this computer. Its archive stays.
	Deleted RetireState = "deleted"
	// RemovalIncomplete: the source was renamed to its remainder path and
	// could not be removed whole. What is left is there, and the archive
	// stays; a later retirement does not delete it again.
	RemovalIncomplete RetireState = "removal-incomplete"
)

// Outcome is what one retirement did, and, when a removal stopped part way,
// the path of the remainder it left.
type Outcome struct {
	State     RetireState
	Remainder string
}

// Retire deletes a source that an existing archive already holds. It never
// writes an archive of its own: archive is the verified recovery backup taken
// earlier, and selection the retirement selection that archive was taken
// under. The source is deleted only while it is still exactly those bytes and
// the archive verifies whole and accounts for every one of them; anything
// else is refused with the source retained. A remainder an earlier removal
// left is reported, never removed again, so a retry cannot delete twice or
// quietly archive a second time.
//
// Deleting is unlinking; it is not secure erasure and revokes no copy.
func Retire(ctx context.Context, path, archive, selection string) (Outcome, error) {
	retained := Outcome{State: Retained}
	if err := ctx.Err(); err != nil {
		return retained, err
	}
	root, err := artifactpath.Directory(path)
	if err != nil {
		if remainder := filepath.Clean(path) + ".retiring"; exists(remainder) {
			return remainderOf(remainder)
		}
		return retained, err
	}
	if remainder := root + ".retiring"; exists(remainder) {
		return remainderOf(remainder)
	}
	if selection == "" {
		return retained, errors.New("a deletion requires the selection its archive was taken under")
	}
	before, err := inventory(ctx, root)
	if err != nil {
		return retained, err
	}
	if selectionOf(before) != selection {
		return retained, errors.New("the project changed since it was archived; nothing was deleted")
	}
	held, err := backup.Verify(archive)
	if err != nil {
		return retained, errors.New("the archive cannot be verified; source retained: " + err.Error())
	}
	if !held.Complete() {
		return retained, errors.New("archive is incomplete; source retained")
	}
	if err := accounts(before, held); err != nil {
		return retained, err
	}
	if err := ctx.Err(); err != nil {
		return retained, err
	}
	return unlink(root)
}

func remainderOf(remainder string) (Outcome, error) {
	return Outcome{State: RemovalIncomplete, Remainder: remainder},
		errors.New("an earlier deletion of this project left a remainder beside it; it is not deleted again and nothing was archived again")
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Copy writes every file of a project into a new directory at the same
// relative paths: the files a backup of it stores and the indexes it would
// record, under the backup's own limits and refusals, so a symbolic link or a
// device in the source is refused rather than followed. The destination must
// be new and outside the source. A copy that stops part way leaves what it
// wrote in the destination, which VerifyCopy never accepts.
func Copy(ctx context.Context, path, destination string) error {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return errors.New("cannot inspect the project directory")
	}
	target, err := artifactpath.Destination(destination, info)
	if err != nil {
		return err
	}
	source, err := os.OpenRoot(root)
	if err != nil {
		return errors.New("cannot open the project directory")
	}
	defer source.Close()
	stored, indexes, err := backup.Scan(source)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		return errors.New("cannot create the copy; destination must be new and parent writable")
	}
	written, err := os.OpenRoot(target)
	if err != nil {
		return errors.New("cannot open the copy")
	}
	defer written.Close()
	for _, name := range append(stored, indexes...) {
		if err := ctx.Err(); err != nil {
			return errors.New("copy cancelled; the incomplete copy is retained")
		}
		if err := copyOne(source, written, name); err != nil {
			return err
		}
	}
	return nil
}

func copyOne(source, target *os.Root, name string) error {
	in, err := source.Open(name)
	if err != nil {
		return errors.New("cannot read a file of the project")
	}
	defer in.Close()
	if parent := path.Dir(name); parent != "." {
		if err := target.MkdirAll(parent, 0o700); err != nil {
			return errors.New("cannot create a directory of the copy")
		}
	}
	out, err := target.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("cannot create a file of the copy")
	}
	_, copyErr := io.Copy(out, io.LimitReader(in, backup.MaxFileBytes+1))
	if copyErr == nil {
		copyErr = out.Sync()
	}
	if closeErr := out.Close(); copyErr != nil || closeErr != nil {
		return errors.New("cannot write a file of the copy")
	}
	return nil
}

// VerifyCopy checks that a copy holds exactly the files of its source, each
// with the same length and digest, measured the way a retirement measures a
// source. It changes nothing.
func VerifyCopy(ctx context.Context, source, copied string) error {
	from, err := artifactpath.Directory(source)
	if err != nil {
		return err
	}
	to, err := artifactpath.Directory(copied)
	if err != nil {
		return err
	}
	want, err := inventory(ctx, from)
	if err != nil {
		return err
	}
	got, err := inventory(ctx, to)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, got) {
		return errors.New("the copy does not match its source")
	}
	return nil
}

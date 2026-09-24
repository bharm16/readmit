package artifactdir

import (
	"errors"
	"io"
	"os"
)

var (
	ErrCreateFile = errors.New("cannot create artifact file")
	ErrSyncFile   = errors.New("cannot sync artifact file; incomplete artifact retained")
)

// Durability is the explicit choice a writer makes about flushing what it
// creates to the device. The zero value is Durable, so a writer that states
// nothing keeps the shared discipline.
type Durability uint8

const (
	// Durable syncs every file before its handle closes and, through a sealed
	// write, every directory naming one before the write answers.
	Durable Durability = iota
	// Scratch creates the same bytes through the same exclusive creates and
	// renames, and syncs nothing. It is only for a throwaway workspace that
	// its owner removes before it answers and keeps nothing from except
	// through a Durable write, so a power loss can cost only work that was
	// never going to be kept.
	Scratch
)

// SyncDirectory syncs the directory name below root, so the names it holds
// survive a power loss. Every directory sync a writer makes goes through it,
// so the platform rule is stated once: on Windows it syncs nothing.
func SyncDirectory(root *os.Root, name string) error {
	return syncDirectory(root, name)
}

// syncDirectory is the directory sync every writer makes. It is the seam
// package tests take to see which directories a write syncs, and when.
var syncDirectory = flushDirectory

// DurableFile is what one durable write needs from the file it writes: a full
// write and a sync. *os.File satisfies it, and so does a size-limited stand-in
// for a full disk, which is the seam package tests use.
type DurableFile interface {
	io.Writer
	Sync() error
}

// WriteFileSync writes data to an already-open file with the shared durable
// discipline: one full write checked for a short write, then Sync. Close
// remains the caller's, so a caller that holds a new file open across earlier
// steps — a replacement created before the bytes it will hold are known —
// still finishes through the same rule.
func WriteFileSync(file DurableFile, data []byte) error {
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	return err
}

// WriteFile creates and syncs one new regular file below root, for a writer
// of something other than a sealed directory, such as a journal's frame. It
// never replaces an existing file and returns no successfully written partial
// as complete.
func WriteFile(root *os.Root, name string, data []byte) error {
	return Durable.writeFile(root, name, data)
}

// writeFile is WriteFile, syncing the file unless d is Scratch. A Scratch file
// is still created exclusively and checked for a short write.
func (d Durability) writeFile(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrCreateFile
	}
	if err := WriteFileSync(seamedFile{file, d}, data); err != nil {
		file.Close()
		return ErrSyncFile
	}
	if err := file.Close(); err != nil {
		return ErrSyncFile
	}
	return nil
}

// Sync flushes a file its writer made unless d is Scratch. It is the same sync
// every member write makes, for a writer that holds its own file open, such as
// an append-only log.
func (d Durability) Sync(file *os.File) error {
	if d == Scratch {
		return nil
	}
	return syncFile(file)
}

// syncFile is the file sync every Durable member write and Sync makes. It is
// the seam tests take to see which files a writer built on them syncs, and
// with what bytes.
var syncFile = (*os.File).Sync

// seamedFile is a file being written, synced as its durability says.
type seamedFile struct {
	*os.File
	durability Durability
}

func (f seamedFile) Sync() error { return f.durability.Sync(f.File) }

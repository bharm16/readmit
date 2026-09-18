package importer

import (
	"archive/zip"
	"bytes"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// FuzzRecordStarts exercises the boundary reader directly. Whatever bytes it is
// given, a member is divided into contiguous records that start at its first
// byte and never overlap, or it is refused; there is no third outcome in which
// a boundary is invented or a byte is skipped.
func FuzzRecordStarts(f *testing.F) {
	for _, seed := range []string{
		"MSH|^~\\&|A\rPID|1\r",
		"MSH|^~\\&|A\rPID|1\rMSH|^~\\&|B\rPID|2\r",
		"FHS|^~\\&|A\rBHS|^~\\&|A\rMSH|^~\\&|A\rBTS|1\rFTS|1\r",
		"\x0bMSH|^~\\&|A\r\x1c\r",
		"MSH|^~\\&|A\nPID|1\nMSH|^~\\&|B\n",
		"",
		"\r\r\rMSH|",
	} {
		f.Add(seed)
	}
	framings := []Plan{
		{Framing: RawFraming, Terminator: hl7.CR},
		{Framing: MLLPFraming, Terminator: hl7.CR},
		{Framing: BatchFraming, BatchBoundary: SegmentStart, Terminator: hl7.CR},
		{Framing: BatchFraming, BatchBoundary: SegmentStart, Terminator: hl7.LF},
		{Framing: BatchFraming, BatchBoundary: SegmentStart, Terminator: hl7.CRLF},
		{Framing: BatchFraming, BatchBoundary: HL7Batch, Terminator: hl7.CR},
	}
	f.Fuzz(func(t *testing.T, member string) {
		data := []byte(member)
		for _, declared := range framings {
			starts, err := recordStarts(declared, data)
			if err != nil {
				if len(err.Error()) > 256 {
					t.Fatal("unbounded diagnostic")
				}
				continue
			}
			if len(starts) == 0 || starts[0] != 0 {
				t.Fatalf("%s records do not start at the first byte: %v", declared.Framing, starts)
			}
			if declared.Framing != BatchFraming && len(starts) != 1 {
				t.Fatalf("%s divided a member it does not divide: %v", declared.Framing, starts)
			}
			for i, start := range starts {
				if start < 0 || start >= max(len(data), 1) {
					t.Fatalf("record boundary outside the member: %v", starts)
				}
				if i > 0 && start <= starts[i-1] {
					t.Fatalf("record boundaries are not increasing: %v", starts)
				}
			}
			// Every byte of the member belongs to exactly one record, so an
			// accepted split can be reassembled into the original bytes.
			total := 0
			for i, start := range starts {
				end := len(data)
				if i+1 < len(starts) {
					end = starts[i+1]
				}
				total += end - start
			}
			if total != len(data) {
				t.Fatalf("records cover %d of %d bytes", total, len(data))
			}
		}
	})
}

// FuzzOccurrences checks that the count a preview reports is the count the case
// bundle writer would store, for both stored framings.
func FuzzOccurrences(f *testing.F) {
	f.Add("\x0bMSH|^~\\&|A\r\x1c\r\x0bMSH|^~\\&|B\r\x1c\r")
	f.Add("MSH|^~\\&|A\r")
	f.Add("\x0bMSH|truncated")
	f.Add("")
	f.Fuzz(func(t *testing.T, record string) {
		data := []byte(record)
		if len(data) > bundle.MaxSourceBytes {
			return
		}
		for _, format := range []hl7.Format{hl7.Raw, hl7.MLLP} {
			count := occurrences(data, format)
			if count < 1 {
				t.Fatalf("%s reported no occurrence for a retained record", format)
			}
			covered, start := 0, 0
			for range count {
				end, _ := bundle.NextOccurrence(data, start, format)
				covered += end - start
				start = end
			}
			if covered != len(data) {
				t.Fatalf("%s counted occurrences covering %d of %d bytes", format, covered, len(data))
			}
		}
	})
}

// FuzzArchiveName exercises the entry-name reader directly. It reads names an
// archive author fully controls, and it is the one refusal standing between a
// crafted archive and a recorded name that is not one relative path.
func FuzzArchiveName(f *testing.F) {
	for _, seed := range []string{
		"exports/one.hl7", "one.hl7", "a/b/c.hl7", "dir/", "",
		"/absolute.hl7", "../escape.hl7", "a/../../escape.hl7", "a\\b.hl7",
		"./one.hl7", "a//b.hl7", "aux", "con.hl7", "C:/one.hl7", "\xff\xfe.hl7",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, declared string) {
		name, err := archiveName(declared)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		// An accepted name is one relative path of local elements. It is
		// recorded in a receipt and never reaches the filesystem, but a name
		// that can be read as an escape must not survive this reader.
		if name == "" || len(name) > 512 || !utf8.ValidString(name) {
			t.Fatalf("accepted an empty, oversized, or invalid name: %q", name)
		}
		if strings.ContainsRune(name, '\\') || path.IsAbs(name) || filepath.IsAbs(name) || !filepath.IsLocal(filepath.FromSlash(name)) {
			t.Fatalf("accepted a name that is not one relative local path: %q", name)
		}
		for _, element := range strings.Split(name, "/") {
			if element == "" || element == "." || element == ".." {
				t.Fatalf("accepted a name with a traversing or empty element: %q", name)
			}
		}
	})
}

// FuzzArchiveEntries exercises the archive reader over crafted container bytes,
// seeded with real archives so the mutator works from valid structures. Every
// entry it returns must be a safe, distinct name within the declared bounds.
func FuzzArchiveEntries(f *testing.F) {
	f.Add(archiveBytes(f, map[string]string{"one.hl7": "MSH|^~\\&|A\r"}))
	f.Add(archiveBytes(f, map[string]string{"exports/one.hl7": "MSH|^~\\&|A\r", "notes.md": "x"}))
	f.Add(archiveBytes(f, map[string]string{"../escape.hl7": "MSH|^~\\&|A\r"}))
	f.Add([]byte("PK\x03\x04 not an archive"))
	f.Fuzz(func(t *testing.T, container []byte) {
		if len(container) > MaxArchiveBytes {
			return
		}
		entries, err := archiveEntries(entry{name: "corpus.zip", path: "/declared/corpus.zip", data: container})
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if len(entries) > MaxContainerEntries {
			t.Fatal("accepted more entries than one import reads")
		}
		seen, total := make(map[string]bool, len(entries)), 0
		for _, item := range entries {
			if _, err := archiveName(item.name); err != nil || seen[item.name] {
				t.Fatalf("accepted an unsafe or repeated entry name: %q", item.name)
			}
			seen[item.name] = true
			total += len(item.data)
			if len(item.data) > bundle.MaxSourceBytes || total > MaxContainerBytes {
				t.Fatal("accepted more bytes than one import reads")
			}
			if item.directory && len(item.data) != 0 {
				t.Fatal("a directory entry carried bytes")
			}
		}
	})
}

func archiveBytes(f *testing.F, entries map[string]string) []byte {
	f.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		file, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			f.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			f.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		f.Fatal(err)
	}
	return buffer.Bytes()
}

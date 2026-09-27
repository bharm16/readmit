//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bharm16/readmit/internal/fhirworker"
)

const maxInput = 16 << 20

var jobID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var packageName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}#[0-9]+(\.[0-9]+){1,3}([+-][A-Za-z0-9.-]+)?$`)

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func output(r fhirworker.Response) int {
	b, e := json.Marshal(r, json.Deterministic(true))
	if e != nil {
		return 125
	}
	_, e = os.Stdout.Write(append(b, '\n'))
	if e != nil {
		return 125
	}
	return 0
}
func readFrame(reader *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		piece, e := reader.ReadSlice('\n')
		if len(out)+len(piece) > max {
			return nil, errors.New("IPC limit")
		}
		out = append(out, piece...)
		if e == nil {
			return out, nil
		}
		if e != bufio.ErrBufferFull {
			return nil, e
		}
	}
}

// run is called only at the pinned image's fixed entrypoint. The inherited
// container restrictions bound its entire process tree, not just Java's heap.
func run() int {
	if len(os.Args) != 1 {
		return 126
	}
	reader := bufio.NewReaderSize(os.Stdin, 4096)
	raw, e := readFrame(reader, 64<<10)
	var request fhirworker.Request
	if e != nil || json.Unmarshal(raw, &request, json.RejectUnknownMembers(true)) != nil || request.Schema != fhirworker.RequestSchema || !jobID.MatchString(request.Job) || !hash.MatchString(request.InputSHA256) || request.TimeoutMS < 1 || request.TimeoutMS > 300000 || request.MaxOutputBytes < 1024 || request.MaxOutputBytes > 16<<20 || len(request.Profiles) > 32 || len(request.Packages) > 32 {
		return 126
	}
	for _, canonical := range request.Profiles {
		parts := strings.Split(canonical, "|")
		if len(parts) > 2 || len(canonical) > 4096 {
			return 126
		}
		u, e := url.Parse(parts[0])
		if e != nil || u.User != nil || u.Fragment != "" || !(u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "urn") || strings.ContainsAny(canonical, "\r\n\\ ") {
			return 126
		}
	}
	result := fhirworker.Response{Schema: fhirworker.ResponseSchema, Job: request.Job, State: "worker-crashed", ExitCode: -1, Outcome: []byte{}}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	ctx, deadline := context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
	defer deadline()
	var mu sync.Mutex
	stopped := ""
	stop := func(reason string) {
		mu.Lock()
		if stopped == "" {
			stopped = reason
		}
		mu.Unlock()
		cancel()
	}
	// Heartbeats ensure the JVM cannot outlive a vanished host adapter even if a
	// Docker attachment process remains alive and keeps the input pipe open.
	finish := func() int {
		mu.Lock()
		reason := stopped
		mu.Unlock()
		if reason != "" {
			result.State = reason
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.State = "timed-out"
		} else if ctx.Err() != nil {
			result.State = "cancelled"
		}
		return output(result)
	}
	heartbeat := make(chan struct{}, 1)
	go func() {
		for {
			line, e := readFrame(reader, 256)
			if e != nil {
				stop("parent-disconnected")
				return
			}
			var h fhirworker.Heartbeat
			if json.Unmarshal(line, &h, json.RejectUnknownMembers(true)) != nil || h.Schema != fhirworker.HeartbeatSchema || h.Job != request.Job {
				stop("invalid-ipc")
				return
			}
			select {
			case heartbeat <- struct{}{}:
			default:
			}
		}
	}()
	go func() {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(2 * time.Second)
			case <-timer.C:
				stop("parent-disconnected")
				return
			}
		}
	}()
	input, e := readRegular("/input/resource.json", maxInput)
	if e != nil || digest(input) != request.InputSHA256 {
		result.State = "invalid-input"
		return finish()
	}
	release, e := readRegular("/opt/java21/release", 64<<10)
	if e != nil || !bytes.Contains(release, []byte(`JAVA_VERSION="21.0.12.1"`)) {
		result.State = "unsupported-runtime"
		return finish()
	}
	for _, directory := range []string{"/work/tmp", "/work/tx-cache"} {
		if e := os.MkdirAll(directory, 0700); e != nil {
			return finish()
		}
	}
	if e = os.MkdirAll("/work/home/.fhir/packages", 0700); e != nil {
		return finish()
	}
	if e = copyPackages(ctx, "/opt/validator/packages", "/work/home/.fhir/packages"); e != nil {
		result.State = "package-unavailable"
		return finish()
	}
	if e = os.WriteFile("/work/resource.json", input, 0600); e != nil {
		return finish()
	}
	args := []string{"-Xms64m", "-Xmx768m", "-XX:MaxMetaspaceSize=256m", "-XX:ActiveProcessorCount=2", "-Djava.awt.headless=true", "-Duser.home=/work/home", "-Djava.io.tmpdir=/work/tmp", "-cp", "/opt/validator/adapter.jar:/opt/validator/validator_cli.jar", "org.readmit.fhir.OfflineValidator", "/work/resource.json", "-version", "4.0.1", "-no-http-access", "-disable-default-resource-fetcher", "-tx", "n/a", "-txCache", "/work/tx-cache", "-output", "/work/outcome.json", "-output-style", "json", "-verbose"}
	for _, name := range request.Packages {
		if !packageName.MatchString(name) {
			result.State = "package-unavailable"
			return finish()
		}
		info, e := os.Stat(filepath.Join("/opt/validator/packages", name, "package/package.json"))
		if e != nil || !info.Mode().IsRegular() {
			result.State = "package-unavailable"
			return finish()
		}
		args = append(args, "-ig", name)
	}
	for _, profile := range request.Profiles {
		args = append(args, "-profile", profile)
	}
	command := exec.CommandContext(ctx, "/opt/java21/bin/java", args...)
	command.Dir = "/work"
	command.Env = []string{"HOME=/work/home", "LANG=C.UTF-8", "PATH=/opt/java21/bin"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = time.Second
	var diagnostic bounded
	diagnostic.limit = request.MaxOutputBytes
	diagnostic.exceeded = func() { stop("output-limit") }
	command.Stdout = &diagnostic
	command.Stderr = &diagnostic
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	e = command.Run()
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	result.DiagnosticSHA256 = digest(diagnostic.bytes())
	mu.Lock()
	reason := stopped
	mu.Unlock()
	if reason != "" {
		result.State = reason
		return finish()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.State = "timed-out"
		return finish()
	}
	if ctx.Err() != nil {
		result.State = "cancelled"
		return finish()
	}
	outcome, readErr := readRegular("/work/outcome.json", request.MaxOutputBytes)
	if readErr != nil {
		if errors.Is(readErr, errLimit) {
			result.State = "output-limit"
		}
		return finish()
	}
	if e != nil && result.ExitCode < 0 {
		return finish()
	}
	result.State = "evaluated"
	result.Outcome = outcome
	return finish()
}

var errLimit = errors.New("bounded file exceeded")

func readRegular(path string, max int64) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("regular file required")
	}
	if info.Size() > max {
		return nil, errLimit
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, errors.New("file changed")
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(b)) > max {
		return nil, errLimit
	}
	return b, e
}
func copyPackages(ctx context.Context, from, to string) error {
	var count, total int64
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		relative, e := filepath.Rel(from, path)
		if e != nil || strings.HasPrefix(relative, "..") || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("unsafe package")
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		count++
		info, e := entry.Info()
		if e != nil || !info.Mode().IsRegular() || count > 50000 {
			return errors.New("package limit")
		}
		total += info.Size()
		if total > 384<<20 {
			return errLimit
		}
		raw, e := readRegular(path, 32<<20)
		if e != nil {
			return e
		}
		return os.WriteFile(target, raw, 0600)
	})
}

type bounded struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    int64
	exceeded func()
}

func (b *bounded) Write(raw []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if int64(b.data.Len()+len(raw)) > b.limit {
		b.exceeded()
		return 0, errLimit
	}
	return b.data.Write(raw)
}
func (b *bounded) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.data.Bytes())
}

package fhirvalidator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/secret"
)

var engineVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9.-]{1,64})?$`)
var containerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// The isolation every worker container is created with and then verified to
// have before it starts.
const (
	workerUser       = "10001:10001"
	workerEntrypoint = "/readmit-validator-worker"
	workerMemory     = 2 << 30
	workerProcesses  = 128
	workerCPUs       = 2
	workerTmpfs      = 512 << 20
)

// stopped is the response for a worker that ended without its own answer.
func stopped(job, state string) WorkerResponse {
	return WorkerResponse{Schema: WorkerResponseSchema, Job: job, State: state, ExitCode: -1, Outcome: []byte{}}
}

// Engine is independently selected local administration state, never decoded
// from a resource, plan or retained packet. Its executable is a fixed installed
// Docker candidate and its control channel must be a local Unix socket. Every
// invocation reports itself as a declared program (secret.DeclaredProgramStarting).
type Engine struct{ docker, host, config string }

func LocalEngine(socket string) (*Engine, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, unavailable("unsupported-runtime", "use a qualified local Linux container capability")
	}
	docker := ""
	for _, candidate := range []string{"/opt/homebrew/bin/docker", "/usr/bin/docker", "/usr/local/bin/docker", "/Applications/Docker.app/Contents/Resources/bin/docker"} {
		resolved, e := filepath.EvalSymlinks(candidate)
		if e != nil {
			continue
		}
		info, e := os.Stat(resolved)
		if e == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			docker = resolved
			break
		}
	}
	if docker == "" {
		return nil, unavailable("worker-missing", "install the optional local container runtime")
	}
	if socket == "" {
		socket = "/var/run/docker.sock"
		if _, e := os.Stat(socket); e != nil {
			home, e := os.UserHomeDir()
			if e != nil {
				return nil, invalid
			}
			socket = filepath.Join(home, ".docker/run/docker.sock")
		}
	}
	resolved, e := filepath.EvalSymlinks(socket)
	if e != nil || !filepath.IsAbs(resolved) || strings.ContainsAny(resolved, "\r\n") {
		return nil, unavailable("worker-missing", "select the local container engine")
	}
	info, e := os.Stat(resolved)
	if e != nil || info.Mode()&os.ModeSocket == 0 {
		return nil, unavailable("worker-missing", "select the local container engine")
	}
	config, e := os.MkdirTemp("", "readmit-validator-engine-")
	if e != nil {
		return nil, invalid
	}
	return &Engine{docker: docker, host: "unix://" + resolved, config: config}, nil
}
func (e *Engine) Close() {
	if e != nil && e.config != "" {
		_ = os.RemoveAll(e.config)
		e.config = ""
	}
}
func (e Engine) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "local FHIR validation engine (private)")
}
func (e *Engine) command(ctx context.Context, args ...string) *exec.Cmd {
	all := append([]string{"--host", e.host, "--config", e.config}, args...)
	c := exec.CommandContext(ctx, e.docker, all...)
	c.Env = []string{"PATH=", "LANG=C.UTF-8", "HOME=" + e.config}
	c.WaitDelay = time.Second
	return c
}
func (e *Engine) control(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c := e.command(ctx, args...)
	out := &limitedBuffer{limit: 256 << 10}
	c.Stdout = out
	c.Stderr = io.Discard
	ended := secret.DeclaredProgramStarting(ctx)
	err := c.Run()
	ended()
	if err != nil {
		return nil, unavailable("worker-unavailable", "verify the local staged container capability")
	}
	return out.copy(), nil
}

// Check is the typed local capability check for Desktop and runner setup.
func (e *Engine) Check(ctx context.Context, c *Capability) Status {
	status, _ := e.inspect(ctx, c)
	return status
}
func (e *Engine) inspect(ctx context.Context, c *Capability) (Status, EngineRecord) {
	if e == nil || e.config == "" || c == nil {
		return Status{"worker-missing", "install the optional local validation capability"}, EngineRecord{}
	}
	raw, err := e.control(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		return Status{"worker-unavailable", "start the selected local container engine"}, EngineRecord{}
	}
	var info struct {
		ServerVersion string `json:"ServerVersion"`
		OSType        string `json:"OSType"`
		Architecture  string `json:"Architecture"`
	}
	if json.Unmarshal(raw, &info) != nil || info.OSType != "linux" || (info.Architecture != "aarch64" && info.Architecture != "arm64") || !engineVersion.MatchString(info.ServerVersion) {
		return Status{"unsupported-runtime", "use the qualified Linux arm64 worker platform"}, EngineRecord{}
	}
	record := EngineRecord{Version: info.ServerVersion, OS: info.OSType, Architecture: info.Architecture}
	raw, err = e.control(ctx, "image", "inspect", c.manifest.Image, "--format", "{{json .}}")
	if err != nil {
		return Status{"worker-missing", "stage the exact offline worker image"}, record
	}
	var image struct {
		ID           string `json:"Id"`
		Architecture string `json:"Architecture"`
		OS           string `json:"Os"`
	}
	if json.Unmarshal(raw, &image) != nil || image.ID != c.manifest.Image || image.OS != "linux" || image.Architecture != "arm64" {
		return Status{"unsupported-runtime", "restage the exact qualified worker image"}, record
	}
	return Status{State: "ready"}, record
}

// PrepareInstalled combines local contract compilation with the explicit local
// runtime check used by runner/Desktop setup, before any resource is processed.
func (e *Engine) PrepareInstalled(ctx context.Context, raw, input []byte, c *Capability) (*Plan, error) {
	p, err := Prepare(raw, input, c)
	if err != nil {
		return nil, err
	}
	status := e.Check(ctx, c)
	if status.State != "ready" {
		return nil, status
	}
	return p, nil
}
func (e *Engine) run(ctx context.Context, p *Plan) (response WorkerResponse, engine EngineRecord, failure error) {
	status, engine := e.inspect(ctx, p.capability)
	if status.State != "ready" {
		return WorkerResponse{}, engine, status
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return WorkerResponse{}, engine, invalid
	}
	job := hex.EncodeToString(nonce)
	directory, err := os.MkdirTemp("", "readmit-fhir-validation-")
	if err != nil {
		return WorkerResponse{}, engine, invalid
	}
	defer os.RemoveAll(directory)
	// Docker's --mount uses commas as separators. Reject an unusual administrator
	// temp root instead of letting it become a second mount declaration.
	if strings.ContainsAny(directory, ",\r\n") {
		return WorkerResponse{}, engine, invalid
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return WorkerResponse{}, engine, invalid
	}
	inputDir := filepath.Join(directory, "input")
	if os.Mkdir(inputDir, 0755) != nil || os.WriteFile(filepath.Join(inputDir, "resource.json"), p.input, 0444) != nil {
		return WorkerResponse{}, engine, invalid
	}
	args := []string{"create", "--rm", "--pull=never", "--interactive", "--init", "--name", "readmit-fhir-" + job, "--label", "readmit.validator.job=" + job, "--network", "none", "--read-only", "--user", workerUser, "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", strconv.Itoa(workerMemory), "--memory-swap", strconv.Itoa(workerMemory), "--pids-limit", strconv.Itoa(workerProcesses), "--cpus", strconv.Itoa(workerCPUs), "--no-healthcheck", "--tmpfs", "/work:rw,noexec,nosuid,size=" + strconv.Itoa(workerTmpfs) + ",mode=0700,uid=10001,gid=10001", "--mount", "type=bind,src=" + inputDir + ",dst=/input,readonly", "--entrypoint", workerEntrypoint, p.capability.manifest.Image}
	raw, err := e.control(ctx, args...)
	if err != nil {
		return WorkerResponse{}, engine, err
	}
	id := strings.TrimSpace(string(raw))
	if !containerID.MatchString(id) {
		return WorkerResponse{}, engine, invalid
	}
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		removed, err := e.control(cleanCtx, "rm", "--force", id)
		if err == nil && strings.TrimSpace(string(removed)) == id {
			return
		}
		// Auto-removal may already have completed. A successful local daemon
		// listing distinguishes absence from an unavailable control channel.
		listed, listErr := e.control(cleanCtx, "container", "ls", "--all", "--no-trunc", "--filter", "id="+id, "--format", "{{.ID}}")
		if listErr != nil || strings.TrimSpace(string(listed)) != "" {
			failure = unavailable("cleanup-unconfirmed", "verify the owned worker container has stopped")
		}
	}()
	if err = e.checkContainer(ctx, id, job, p.capability.manifest.Image, inputDir); err != nil {
		return WorkerResponse{}, engine, err
	}
	request := WorkerRequest{Schema: WorkerRequestSchema, Job: job, InputSHA256: p.request.InputSHA256, Profiles: p.profiles, Packages: p.packages, TimeoutMS: p.request.TimeoutMS, MaxOutputBytes: p.request.MaxOutputBytes}
	frame := append(encode(request), '\n')
	if len(frame) > 64<<10 {
		return WorkerResponse{}, engine, invalid
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(p.request.TimeoutMS)*time.Millisecond+5*time.Second)
	defer cancel()
	command := e.command(runCtx, "start", "--attach", "--interactive", id)
	stdin, err := command.StdinPipe()
	if err != nil {
		return WorkerResponse{}, engine, invalid
	}
	stdout := &limitedBuffer{limit: p.request.MaxOutputBytes*2 + 65536, onLimit: cancel}
	stderr := &limitedBuffer{limit: 65536, onLimit: cancel}
	command.Stdout = stdout
	command.Stderr = stderr
	ended := secret.DeclaredProgramStarting(ctx)
	if err = command.Start(); err != nil {
		ended()
		return WorkerResponse{}, engine, unavailable("worker-crashed", "restart the staged capability")
	}
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		defer stdin.Close()
		if _, err := stdin.Write(frame); err != nil {
			return
		}
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		pulse := append(encode(Heartbeat{Schema: HeartbeatSchema, Job: job}), '\n')
		for {
			select {
			case <-heartbeatDone:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if _, err := stdin.Write(pulse); err != nil {
					return
				}
			}
		}
	}()
	err = command.Wait()
	ended()
	if ctx.Err() != nil {
		return stopped(job, "cancelled"), engine, nil
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return stopped(job, "timed-out"), engine, nil
	}
	if stdout.overflowed() || stderr.overflowed() {
		return stopped(job, "output-limit"), engine, nil
	}
	if err != nil || json.Unmarshal(stdout.copy(), &response, json.RejectUnknownMembers(true)) != nil || response.Schema != WorkerResponseSchema || response.Job != job || int64(len(response.Outcome)) > p.request.MaxOutputBytes {
		return stopped(job, "worker-crashed"), engine, nil
	}
	return response, engine, nil
}
func (e *Engine) checkContainer(ctx context.Context, id, job, image, inputDir string) error {
	raw, err := e.control(ctx, "inspect", id, "--format", "{{json .}}")
	if err != nil {
		return err
	}
	var c struct {
		Image  string `json:"Image"`
		Config struct {
			User       string            `json:"User"`
			Entrypoint []string          `json:"Entrypoint"`
			Labels     map[string]string `json:"Labels"`
		} `json:"Config"`
		Host struct {
			Network  string            `json:"NetworkMode"`
			Readonly bool              `json:"ReadonlyRootfs"`
			Memory   int64             `json:"Memory"`
			Swap     int64             `json:"MemorySwap"`
			Pids     int64             `json:"PidsLimit"`
			CPUs     int64             `json:"NanoCpus"`
			Security []string          `json:"SecurityOpt"`
			CapDrop  []string          `json:"CapDrop"`
			Tmpfs    map[string]string `json:"Tmpfs"`
		} `json:"HostConfig"`
		Mounts []struct {
			Type, Source, Destination string
			RW                        bool
		} `json:"Mounts"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Image != image || c.Config.User != workerUser || len(c.Config.Entrypoint) != 1 || c.Config.Entrypoint[0] != workerEntrypoint || c.Config.Labels["readmit.validator.job"] != job || c.Host.Network != "none" || !c.Host.Readonly || c.Host.Memory != workerMemory || c.Host.Swap != workerMemory || c.Host.Pids != workerProcesses || c.Host.CPUs != workerCPUs*1e9 || len(c.Host.Security) != 1 || c.Host.Security[0] != "no-new-privileges" || len(c.Host.CapDrop) != 1 || c.Host.CapDrop[0] != "ALL" || len(c.Host.Tmpfs) != 1 || !strings.Contains(c.Host.Tmpfs["/work"], "size="+strconv.Itoa(workerTmpfs)) {
		return unavailable("unsupported-runtime", "container isolation requirements were not applied")
	}
	inputs := 0
	for _, mount := range c.Mounts {
		switch mount.Type {
		case "bind":
			if mount.Source != inputDir || mount.Destination != "/input" || mount.RW {
				return invalid
			}
			inputs++
		case "tmpfs":
			if mount.Destination != "/work" || !mount.RW {
				return invalid
			}
		default:
			return invalid
		}
	}
	if inputs != 1 {
		return invalid
	}
	return nil
}

type limitedBuffer struct {
	mu      sync.Mutex
	data    bytes.Buffer
	limit   int64
	full    bool
	onLimit func()
}

func (b *limitedBuffer) Write(raw []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if int64(b.data.Len()+len(raw)) > b.limit {
		b.full = true
		if b.onLimit != nil {
			b.onLimit()
		}
		return 0, invalid
	}
	return b.data.Write(raw)
}
func (b *limitedBuffer) copy() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.data.Bytes())
}
func (b *limitedBuffer) overflowed() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.full }

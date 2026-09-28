package connectedlab

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// ContainerEngine is an independent local container engine for validation
// tests, written from the Docker Engine HTTP API description: it answers the
// installed docker command line over a private Unix socket, keeps each created
// container's declared isolation, attaches its streams and runs a worker that
// speaks the fixed validation worker protocol (a JSON request line, heartbeat
// lines, one JSON response). The worker reads the input file from the host
// directory the container's read-only bind mount names, exactly the bytes a
// real container would see at /input/resource.json.
//
// It stands in for a Linux arm64 engine holding the staged validator image; it
// is not the qualified validator. That image is qualified only by the opt-in
// live test (READMIT_FHIR_VALIDATOR_CAPABILITY). It shares no readmit code.
type ContainerEngine struct {
	Socket string
	// Image is the one image ID the engine holds; any other is missing.
	Image string

	mu         sync.Mutex
	containers map[string]*labContainer
	runs       []WorkerRun
	validate   func(Validation) Outcome
	isolation  bool
}

// Validation is what one worker received: the job's request line and the
// bytes its read-only input mount held.
type Validation struct {
	Job      string
	Profiles []string
	Input    []byte
	Timeout  time.Duration
}

// Outcome is what the worker answers. State is the worker state
// ("evaluated", "timed-out", "package-unavailable", ...); Outcome is the raw
// OperationOutcome for an evaluated job; Exit the validator's exit code.
type Outcome struct {
	State   string
	Exit    int
	Outcome []byte
	Log     []byte
}

// WorkerRun records one executed worker: the input digest it read and the
// isolation its container was created with.
type WorkerRun struct {
	Job         string
	InputSHA256 string
	Network     string
	ReadOnly    bool
	InputMount  string
}

type labContainer struct {
	id, name, image string
	config          map[string]any
	host            map[string]any
	mounts          []map[string]any
	attached        chan net.Conn
	started         chan struct{}
	exited          chan struct{}
	autoRemove      bool
	removed         bool
}

var apiVersion = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)

// StartContainerEngine serves the engine on a fresh private socket until the
// test ends. image is the image ID it holds (the staged capability's).
func StartContainerEngine(t testing.TB, image string) *ContainerEngine {
	t.Helper()
	dir, err := os.MkdirTemp("", "readmit-lab-engine-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "engine.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	e := &ContainerEngine{Socket: socket, Image: image, containers: map[string]*labContainer{}, validate: LabValidator(""), isolation: true}
	server := &http.Server{Handler: http.HandlerFunc(e.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return e
}

// SetValidator replaces what the worker answers.
func (e *ContainerEngine) SetValidator(v func(Validation) Outcome) {
	e.mu.Lock()
	e.validate = v
	e.mu.Unlock()
}

// DropIsolation makes the engine report every container without the network
// and filesystem isolation it was asked for, as a misconfigured engine would.
func (e *ContainerEngine) DropIsolation() { e.mu.Lock(); e.isolation = false; e.mu.Unlock() }

// Runs lists every worker that executed.
func (e *ContainerEngine) Runs() []WorkerRun {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]WorkerRun{}, e.runs...)
}

// LabValidator is a small offline FHIR checker standing in for the pinned
// validator. It echoes the resource's first business identifier and family
// name into its diagnostics, as a real validator quotes offending values, and
// reports an error when a Patient has no identifier. With terminology
// "unavailable" it reports that a required binding could not be checked.
func LabValidator(terminology string) func(Validation) Outcome {
	return func(v Validation) Outcome {
		var resource struct {
			ResourceType string `json:"resourceType"`
			Identifier   []struct {
				Value string `json:"value"`
			} `json:"identifier"`
			Name []struct {
				Family string `json:"family"`
			} `json:"name"`
		}
		if json.Unmarshal(v.Input, &resource) != nil || resource.ResourceType == "" {
			return Outcome{State: "evaluated", Exit: 1, Outcome: []byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"fatal","code":"structure","diagnostics":"Unable to parse the resource"}]}`)}
		}
		issue := func(severity, code, diagnostics, message string) map[string]any {
			i := map[string]any{"severity": severity, "code": code, "diagnostics": diagnostics, "expression": []string{resource.ResourceType}}
			if message != "" {
				i["extension"] = []map[string]string{{"url": "http://hl7.org/fhir/StructureDefinition/operationoutcome-message-id", "valueCode": message}}
			}
			return i
		}
		issues := []map[string]any{}
		quoted := resource.ResourceType
		if len(resource.Identifier) > 0 {
			quoted += " identifier '" + resource.Identifier[0].Value + "'"
		}
		if len(resource.Name) > 0 {
			quoted += " family '" + resource.Name[0].Family + "'"
		}
		issues = append(issues, issue("information", "informational", "Validated "+quoted, ""))
		exit := 0
		if resource.ResourceType == "Patient" && len(resource.Identifier) == 0 {
			issues = append(issues, issue("error", "required", "Patient.identifier: minimum required = 1, but only found 0 in "+quoted, ""))
			exit = 1
		}
		if terminology == "unavailable" {
			issues = append(issues, issue("warning", "not-supported", "The binding for "+quoted+" could not be checked without a terminology service", "Terminology_TX_Binding_NoServer"))
		}
		raw, _ := json.Marshal(map[string]any{"resourceType": "OperationOutcome", "issue": issues})
		return Outcome{State: "evaluated", Exit: exit, Outcome: raw, Log: []byte("lab validator: " + quoted + "\n")}
	}
}

// HangingValidator never answers before the job's own deadline, so the worker
// reports its deadline as a real worker does.
func HangingValidator(v Validation) Outcome {
	time.Sleep(v.Timeout)
	return Outcome{State: "timed-out", Exit: -1}
}

func (e *ContainerEngine) serve(w http.ResponseWriter, r *http.Request) {
	path := apiVersion.ReplaceAllString(r.URL.Path, "")
	w.Header().Set("API-Version", "1.47")
	w.Header().Set("OSType", "linux")
	w.Header().Set("Docker-Experimental", "false")
	switch {
	case path == "/_ping":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "OK")
	case path == "/version":
		reply(w, 200, map[string]any{"Version": "27.3.1", "ApiVersion": "1.47", "MinAPIVersion": "1.24", "Os": "linux", "Arch": "arm64"})
	case path == "/info":
		reply(w, 200, map[string]any{"ID": "readmit-lab-engine", "ServerVersion": "27.3.1", "OSType": "linux", "Architecture": "aarch64", "OperatingSystem": "readmit lab engine", "NCPU": 2, "Containers": 0, "Images": 1})
	case strings.HasPrefix(path, "/images/") && strings.HasSuffix(path, "/json"):
		name := strings.TrimSuffix(strings.TrimPrefix(path, "/images/"), "/json")
		if name != e.Image {
			reply(w, 404, map[string]string{"message": "No such image: " + name})
			return
		}
		reply(w, 200, map[string]any{"Id": e.Image, "RepoTags": []string{}, "Os": "linux", "Architecture": "arm64", "Config": map[string]any{}})
	case path == "/containers/create" && r.Method == http.MethodPost:
		e.create(w, r)
	case path == "/containers/json":
		reply(w, 200, []any{})
	case strings.HasPrefix(path, "/containers/"):
		id, action, _ := strings.Cut(strings.TrimPrefix(path, "/containers/"), "/")
		c := e.container(id)
		if c == nil {
			reply(w, 404, map[string]string{"message": "No such container: " + id})
			return
		}
		switch {
		case action == "json":
			e.inspect(w, c)
		case action == "attach":
			e.attach(w, c)
		case action == "start":
			close(c.started)
			w.WriteHeader(204)
		case action == "wait":
			// The engine answers the headers at once and the status at exit;
			// the command line starts the container only after the headers.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-c.exited
			if r.URL.Query().Get("condition") == "removed" {
				for !e.gone(c) {
					time.Sleep(5 * time.Millisecond)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"StatusCode": 0})
		case action == "" && r.Method == http.MethodDelete:
			e.remove(c)
			w.WriteHeader(204)
		default:
			reply(w, 404, map[string]string{"message": "unsupported"})
		}
	default:
		reply(w, 404, map[string]string{"message": "page not found"})
	}
}

func reply(w http.ResponseWriter, code int, body any) {
	raw, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

func (e *ContainerEngine) container(id string) *labContainer {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c := e.containers[id]; c != nil {
		return c
	}
	for _, c := range e.containers {
		if strings.TrimPrefix(c.name, "/") == id {
			return c
		}
	}
	return nil
}

func (e *ContainerEngine) gone(c *labContainer) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return c.removed
}

func (e *ContainerEngine) remove(c *labContainer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c.removed = true
	delete(e.containers, c.id)
}

func (e *ContainerEngine) create(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		reply(w, 400, map[string]string{"message": "invalid create body"})
		return
	}
	image, _ := body["Image"].(string)
	if image != e.Image {
		reply(w, 404, map[string]string{"message": "No such image: " + image})
		return
	}
	host, _ := body["HostConfig"].(map[string]any)
	if host == nil {
		host = map[string]any{}
	}
	mounts := []map[string]any{}
	if declared, ok := host["Mounts"].([]any); ok {
		for _, m := range declared {
			if mount, ok := m.(map[string]any); ok {
				mounts = append(mounts, mount)
			}
		}
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	c := &labContainer{id: hex.EncodeToString(id), name: "/" + r.URL.Query().Get("name"), image: image, config: body, host: host, mounts: mounts, attached: make(chan net.Conn, 1), started: make(chan struct{}), exited: make(chan struct{})}
	c.autoRemove, _ = host["AutoRemove"].(bool)
	e.mu.Lock()
	e.containers[c.id] = c
	e.mu.Unlock()
	go e.run(c)
	reply(w, 201, map[string]any{"Id": c.id, "Warnings": []string{}})
}

func (e *ContainerEngine) inspect(w http.ResponseWriter, c *labContainer) {
	e.mu.Lock()
	isolated := e.isolation
	e.mu.Unlock()
	host := map[string]any{}
	for k, v := range c.host {
		host[k] = v
	}
	mounts := []map[string]any{}
	for _, m := range c.mounts {
		readOnly, _ := m["ReadOnly"].(bool)
		mounts = append(mounts, map[string]any{"Type": m["Type"], "Source": m["Source"], "Destination": m["Target"], "RW": !readOnly || !isolated, "Mode": ""})
	}
	if !isolated {
		host["NetworkMode"], host["ReadonlyRootfs"] = "bridge", false
	}
	config := map[string]any{"Tty": false, "OpenStdin": true, "StdinOnce": true, "AttachStdin": true, "AttachStdout": true, "AttachStderr": true}
	for _, k := range []string{"User", "Entrypoint", "Labels", "Image", "Cmd"} {
		config[k] = c.config[k]
	}
	reply(w, 200, map[string]any{"Id": c.id, "Name": c.name, "Image": c.image, "Config": config, "HostConfig": host, "Mounts": mounts, "State": map[string]any{"Status": "created", "Running": false}})
}

func (e *ContainerEngine) attach(w http.ResponseWriter, c *labContainer) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		reply(w, 500, map[string]string{"message": "no hijack"})
		return
	}
	conn, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	_, _ = buffered.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
	_ = buffered.Flush()
	c.attached <- &bufferedConn{Conn: conn, reader: buffered.Reader}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.reader.Read(p) }

// run is the container's process: once attached and started, it reads the
// worker request line, answers heartbeats until the validator returns, and
// writes one response line on the multiplexed stdout stream.
func (e *ContainerEngine) run(c *labContainer) {
	defer func() {
		if c.autoRemove {
			e.remove(c)
		}
		close(c.exited)
	}()
	var conn net.Conn
	select {
	case conn = <-c.attached:
	case <-time.After(30 * time.Second):
		return
	}
	defer conn.Close()
	select {
	case <-c.started:
	case <-time.After(30 * time.Second):
		return
	}
	lines := bufio.NewReader(conn)
	first, err := lines.ReadBytes('\n')
	if err != nil {
		return
	}
	var request struct {
		Schema      string   `json:"schema"`
		Job         string   `json:"job"`
		InputSHA256 string   `json:"input_sha256"`
		Profiles    []string `json:"profiles"`
		TimeoutMS   int64    `json:"timeout_ms"`
	}
	if json.Unmarshal(first, &request) != nil || request.Schema != "readmit-fhir-worker-request/v1" {
		return
	}
	go func() {
		for {
			if _, err := lines.ReadBytes('\n'); err != nil {
				return
			}
		}
	}()
	source := ""
	for _, m := range c.mounts {
		if m["Target"] == "/input" {
			source, _ = m["Source"].(string)
		}
	}
	input, err := os.ReadFile(filepath.Join(source, "resource.json"))
	response := map[string]any{"schema": "readmit-fhir-worker-response/v1", "job": request.Job, "state": "invalid-input", "exit_code": -1, "outcome": []byte{}, "diagnostic_sha256": ""}
	sum := sha256.Sum256(input)
	if err == nil && hex.EncodeToString(sum[:]) == request.InputSHA256 {
		host := c.host
		network, _ := host["NetworkMode"].(string)
		readOnly, _ := host["ReadonlyRootfs"].(bool)
		e.mu.Lock()
		e.runs = append(e.runs, WorkerRun{Job: request.Job, InputSHA256: request.InputSHA256, Network: network, ReadOnly: readOnly, InputMount: source})
		validate := e.validate
		e.mu.Unlock()
		out := validate(Validation{Job: request.Job, Profiles: request.Profiles, Input: input, Timeout: time.Duration(request.TimeoutMS) * time.Millisecond})
		log := sha256.Sum256(out.Log)
		response["state"], response["exit_code"], response["outcome"], response["diagnostic_sha256"] = out.State, out.Exit, out.Outcome, hex.EncodeToString(log[:])
		if out.Outcome == nil {
			response["outcome"] = []byte{}
		}
	}
	raw, _ := json.Marshal(response)
	_ = writeFrame(conn, 1, append(raw, '\n'))
}

// writeFrame writes one multiplexed stream frame: the stream number, three
// zero bytes and the big-endian payload length, then the payload.
func writeFrame(w io.Writer, stream byte, payload []byte) error {
	if len(payload) > 1<<30 {
		return errors.New("frame too large")
	}
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

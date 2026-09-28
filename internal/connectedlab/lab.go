// Package connectedlab is an independent synthetic laboratory for connected
// FHIR tests: a FHIR R4 JSON server, a v2-to-FHIR integration engine and an
// isolation fixture adapter, written from the protocol descriptions. Its
// server and engine share no readmit FHIR or v2 code; the fixture adapter
// uses only the public wire types of the fixture protocol. Only tests import
// it; the product never links it, and nothing here qualifies a real EHR or LIS.
package connectedlab

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// FHIRLab is an independent FHIR R4 JSON server written from the HTTP and
// search specifications. It shares no readmit code: its store, versioning,
// conditional update, paging and _include handling are its own. Behaviours
// (downstream Encounter creation, outages, truncated pages, dropped create
// responses) model the system under test and its failure modes.
type FHIRLab struct {
	s          *httptest.Server
	mu         sync.Mutex
	next       int
	store      map[string]*resource
	order      []string
	Creates    atomic.Int32
	Writes     atomic.Int32
	Outage     atomic.Bool
	Truncate   atomic.Bool
	DropCreate atomic.Bool
	pageSize   int
	// extra seeds scenario records when the isolated tenant is provisioned.
	Extra func()
	// capability replaces the served CapabilityStatement when set.
	CapabilityOverride atomic.Value
	// IncludeSameType adds a same-identifier historical Appointment to every
	// Appointment search page as an included entry, never as a match.
	IncludeSameType atomic.Bool
	// smart, when set, is the registered client's public key: the lab then
	// issues bearer tokens at /token and requires one on every resource request.
	smart   *ecdsa.PublicKey
	jtis    map[string]bool
	scopes  map[string]string
	Tokens  atomic.Int32
	Bearers atomic.Int32
	// downstream is the declared application behaviour on Appointment writes:
	// "" none, "encounter" one planned Encounter per Appointment kept in step,
	// "encounter-duplicate" a defective extra Encounter on every update.
	downstream string
}
type resource struct {
	typ, id string
	version int
	body    map[string]any
	visible time.Time
}

const Capability = `{"resourceType":"CapabilityStatement","status":"active","date":"2026-01-01","kind":"instance","fhirVersion":"4.0.1","format":["application/fhir+json"],"rest":[{"mode":"server","resource":[` +
	`{"type":"Appointment","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"update"},{"code":"delete"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"Encounter","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"Patient","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"update"},{"code":"delete"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"ServiceRequest","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"update"},{"code":"delete"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"Practitioner","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"delete"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"Location","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"delete"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"DiagnosticReport","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"update"}],"searchParam":[{"name":"identifier","type":"token"}]},` +
	`{"type":"Observation","versioning":"versioned-update","searchInclude":["Observation:subject"],"interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},{"code":"update"}],"searchParam":[{"name":"identifier","type":"token"}]}]}]}`

func StartFHIRLab(t testing.TB) *FHIRLab {
	t.Helper()
	lab := &FHIRLab{store: map[string]*resource{}, pageSize: 50}
	lab.s = httptest.NewTLSServer(http.HandlerFunc(lab.serve))
	t.Cleanup(lab.s.Close)
	return lab
}
func (l *FHIRLab) Base() string { return l.s.URL + "/fhir" }

// Server is the lab's TLS listener, for its certificate and address.
func (l *FHIRLab) Server() *httptest.Server { return l.s }

// SetDownstream selects the application behaviour an Appointment write has.
func (l *FHIRLab) SetDownstream(mode string) { l.mu.Lock(); l.downstream = mode; l.mu.Unlock() }
func (l *FHIRLab) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.store = map[string]*resource{}
	l.order = nil
	l.Creates.Store(0)
	l.Writes.Store(0)
}
func outcome(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/fhir+json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing"}]}`))
}
func (l *FHIRLab) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" {
		l.token(w, r)
		return
	}
	if r.Header.Get("Accept") != "application/fhir+json" {
		outcome(w, 406)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/fhir/")
	if path == "metadata" {
		w.Header().Set("Content-Type", "application/fhir+json")
		served := Capability
		if v, ok := l.CapabilityOverride.Load().(string); ok && v != "" {
			served = v
		}
		_, _ = w.Write([]byte(served))
		return
	}
	parts := strings.Split(path, "/")
	if !l.authorized(r, parts[0]) {
		outcome(w, 401)
		return
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		if l.Outage.Load() {
			outcome(w, 503)
			return
		}
		l.search(w, r, parts[0])
	case r.Method == http.MethodGet && len(parts) == 2:
		l.mu.Lock()
		res := l.store[parts[0]+"/"+parts[1]]
		l.mu.Unlock()
		if res == nil {
			outcome(w, 404)
			return
		}
		l.respond(w, 200, res, false)
	case r.Method == http.MethodPost && len(parts) == 1:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		res, err := l.create(parts[0], body)
		if err != nil {
			outcome(w, 400)
			return
		}
		if l.DropCreate.Load() {
			// The create is committed; the response is lost in transit.
			if conn, _, e := w.(http.Hijacker).Hijack(); e == nil {
				conn.Close()
			}
			return
		}
		l.respond(w, 201, res, true)
	case r.Method == http.MethodPut && len(parts) == 2:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		res, code := l.update(parts[0], parts[1], r.Header.Get("If-Match"), body)
		if code != 200 {
			outcome(w, code)
			return
		}
		l.respond(w, 200, res, true)
	case r.Method == http.MethodDelete && len(parts) == 2:
		l.mu.Lock()
		key := parts[0] + "/" + parts[1]
		_, ok := l.store[key]
		delete(l.store, key)
		l.mu.Unlock()
		if !ok {
			outcome(w, 404)
			return
		}
		w.WriteHeader(204)
	default:
		outcome(w, 400)
	}
}
func (l *FHIRLab) respond(w http.ResponseWriter, code int, res *resource, location bool) {
	l.mu.Lock()
	raw, _ := json.Marshal(res.body)
	version := res.version
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/fhir+json")
	w.Header().Set("ETag", fmt.Sprintf(`W/"%d"`, version))
	if location {
		w.Header().Set("Location", fmt.Sprintf("%s/%s/%s/_history/%d", l.Base(), res.typ, res.id, version))
	}
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

// Put stores a new resource version; visibleAfter delays when a search sees
// it. Callers outside the lab call it only from Extra, under the lab's lock.
func (l *FHIRLab) Put(typ, id string, body map[string]any, visibleAfter time.Duration) {
	l.put(typ, id, body, visibleAfter)
}
func (l *FHIRLab) put(typ, id string, body map[string]any, visibleAfter time.Duration) *resource {
	key := typ + "/" + id
	res := l.store[key]
	if res == nil {
		res = &resource{typ: typ, id: id}
		l.store[key] = res
		l.order = append(l.order, key)
	}
	res.version++
	body["resourceType"], body["id"] = typ, id
	body["meta"] = map[string]any{"versionId": strconv.Itoa(res.version)}
	res.body = body
	res.visible = time.Now().Add(visibleAfter)
	l.Writes.Add(1)
	return res
}

// decode keeps each JSON number's exact lexeme, as a FHIR store must.
func decode(raw []byte) (map[string]any, error) {
	var body map[string]any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	return body, d.Decode(&body)
}
func (l *FHIRLab) create(typ string, raw []byte) (*resource, error) {
	body, err := decode(raw)
	if err != nil || body["resourceType"] != typ {
		return nil, fmt.Errorf("invalid body")
	}
	l.Creates.Add(1)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	res := l.put(typ, fmt.Sprintf("%s-%d", strings.ToLower(typ[:3]), l.next), body, 0)
	l.downstreamLocked(res, true)
	return res, nil
}
func (l *FHIRLab) update(typ, id, match string, raw []byte) (*resource, int) {
	body, err := decode(raw)
	if err != nil || body["resourceType"] != typ || body["id"] != id {
		return nil, 400
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	res := l.store[typ+"/"+id]
	if res == nil {
		return nil, 404
	}
	if match != fmt.Sprintf(`W/"%d"`, res.version) {
		return nil, 412
	}
	res = l.put(typ, id, body, 0)
	l.downstreamLocked(res, false)
	return res, 200
}

// downstreamLocked is the application behaviour an Appointment write triggers.
func (l *FHIRLab) downstreamLocked(res *resource, created bool) {
	if res.typ != "Appointment" || l.downstream == "" {
		return
	}
	identifier, _ := res.body["identifier"].([]any)
	status := "planned"
	if res.body["status"] == "cancelled" {
		status = "cancelled"
	}
	encounter := map[string]any{"status": status, "class": map[string]any{"system": "http://terminology.hl7.org/CodeSystem/v3-ActCode", "code": "AMB"}, "identifier": identifier, "appointment": []any{map[string]any{"reference": "Appointment/" + res.id}}, "period": map[string]any{"start": res.body["start"]}}
	existing := ""
	for _, key := range l.order {
		e := l.store[key]
		if e != nil && e.typ == "Encounter" {
			refs, _ := e.body["appointment"].([]any)
			if len(refs) == 1 && refs[0].(map[string]any)["reference"] == "Appointment/"+res.id {
				existing = e.id
			}
		}
	}
	if existing == "" || l.downstream == "encounter-duplicate" && !created {
		l.next++
		existing = fmt.Sprintf("enc-%d", l.next)
	}
	l.put("Encounter", existing, encounter, 0)
}

// search implements token identifier matching, _include=Observation:subject
// and opaque-free paging whose next link repeats the query with _getpages.
func (l *FHIRLab) search(w http.ResponseWriter, r *http.Request, typ string) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("_getpages"))
	if page < 1 {
		page = 1
	}
	if page > 1 && l.Truncate.Load() {
		outcome(w, 500)
		return
	}
	size := l.pageSize
	if n, err := strconv.Atoi(query.Get("_count")); err == nil && n > 0 {
		size = n
	}
	l.mu.Lock()
	matches := []*resource{}
	now := time.Now()
	for _, key := range l.order {
		res := l.store[key]
		if res == nil || res.typ != typ || res.visible.After(now) || !labMatches(res, query) {
			continue
		}
		matches = append(matches, res)
	}
	start := (page - 1) * size
	end := min(start+size, len(matches))
	if start > len(matches) {
		start = len(matches)
	}
	original := url.Values{}
	for k, v := range query {
		if k != "_getpages" {
			original[k] = v
		}
	}
	self := l.Base() + "/" + typ
	if encoded := original.Encode(); encoded != "" {
		self += "?" + encoded
	}
	links := []any{map[string]any{"relation": "self", "url": self}}
	if end < len(matches) || l.Truncate.Load() {
		separator := "?"
		if strings.Contains(self, "?") {
			separator = "&"
		}
		links = append(links, map[string]any{"relation": "next", "url": self + separator + "_getpages=" + strconv.Itoa(page+1)})
	}
	entries := []any{}
	included := map[string]bool{}
	for _, res := range matches[start:end] {
		entries = append(entries, map[string]any{"fullUrl": l.Base() + "/" + res.typ + "/" + res.id, "search": map[string]any{"mode": "match"}, "resource": res.body})
	}
	if typ == "Appointment" && l.IncludeSameType.Load() {
		_, value, _ := strings.Cut(query.Get("identifier"), "|")
		historical := map[string]any{"resourceType": "Appointment", "id": "historical", "identifier": []any{map[string]any{"system": AppointmentSystem, "value": value}}, "status": "noshow", "start": "2025-12-01T09:00:00Z", "participant": []any{map[string]any{"status": "accepted"}}, "meta": map[string]any{"versionId": "1"}}
		entries = append(entries, map[string]any{"fullUrl": l.Base() + "/Appointment/historical", "search": map[string]any{"mode": "include"}, "resource": historical})
	}
	if query.Get("_include") == "Observation:subject" {
		for _, res := range matches[start:end] {
			subject, _ := res.body["subject"].(map[string]any)
			ref, _ := subject["reference"].(string)
			if target := l.store[ref]; target != nil && !included[ref] {
				included[ref] = true
				entries = append(entries, map[string]any{"fullUrl": l.Base() + "/" + ref, "search": map[string]any{"mode": "include"}, "resource": target.body})
			}
		}
	}
	bundle := map[string]any{"resourceType": "Bundle", "type": "searchset", "link": links}
	if len(entries) > 0 {
		// FHIR JSON has no empty arrays: an empty page omits entry.
		bundle["entry"] = entries
	}
	raw, _ := json.Marshal(bundle)
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/fhir+json")
	_, _ = w.Write(raw)
}
func labMatches(res *resource, query url.Values) bool {
	token := query.Get("identifier")
	if token == "" {
		return true
	}
	system, value, qualified := strings.Cut(token, "|")
	identifiers, _ := res.body["identifier"].([]any)
	for _, item := range identifiers {
		id, _ := item.(map[string]any)
		if qualified && id["system"] == system && id["value"] == value || !qualified && id["value"] == system {
			return true
		}
	}
	return false
}

// Count reports how many stored resources of a type carry a business ID.
func (l *FHIRLab) Count(typ, system, value string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, res := range l.store {
		if res.typ == typ && labMatches(res, url.Values{"identifier": {system + "|" + value}}) {
			n++
		}
	}
	return n
}

// Engine is an independent v2-to-FHIR integration engine. It accepts MLLP,
// always returns an AA acknowledgement, and writes FHIR with its own client.
// Modes model a correct engine and specific defects.
type Engine struct {
	Listener net.Listener
	lab      *FHIRLab
	client   *http.Client
	mu       sync.Mutex
	mode     string
	after    func()
	done     sync.WaitGroup
}

func StartEngine(t testing.TB, lab *FHIRLab) *Engine {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Listener: l, lab: lab, client: lab.s.Client(), mode: "fixed"}
	e.done.Add(1)
	go func() {
		defer e.done.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			e.done.Add(1)
			go func() {
				defer e.done.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(time.Minute))
				reader := bufio.NewReader(c)
				for {
					frame, err := reader.ReadBytes(28)
					if err != nil {
						return
					}
					if _, err = reader.ReadByte(); err != nil {
						return
					}
					raw := strings.TrimPrefix(string(frame[:len(frame)-1]), string(byte(11)))
					control := e.handle(raw)
					e.mu.Lock()
					after := e.after
					e.mu.Unlock()
					if after != nil {
						after()
					}
					ack := "MSH|^~\\&|ENGINE|LAB|SENDER|LAB|20260101000000||ACK|ACK|P|2.5.1\rMSA|AA|" + control + "\r"
					_, _ = c.Write(append(append([]byte{11}, []byte(ack)...), 28, 13))
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); e.done.Wait() })
	return e
}
func (e *Engine) SetMode(mode string) { e.mu.Lock(); e.mode = mode; e.mu.Unlock() }
func (e *Engine) currentMode() string { e.mu.Lock(); defer e.mu.Unlock(); return e.mode }

// SetAfter runs after is called after each handled message, to inject faults.
func (e *Engine) SetAfter(after func()) { e.mu.Lock(); e.after = after; e.mu.Unlock() }
func field(raw, segment string, index int) string {
	for _, s := range strings.Split(raw, "\r") {
		parts := strings.Split(s, "|")
		if parts[0] == segment && len(parts) > index {
			return parts[index]
		}
	}
	return ""
}
func (e *Engine) fhir(method, path, match string, body map[string]any) (map[string]any, string) {
	raw, _ := json.Marshal(body)
	request, _ := http.NewRequest(method, e.lab.Base()+"/"+path, strings.NewReader(string(raw)))
	if body == nil {
		request, _ = http.NewRequest(method, e.lab.Base()+"/"+path, nil)
	} else {
		request.Header.Set("Content-Type", "application/fhir+json")
	}
	request.Header.Set("Accept", "application/fhir+json")
	if match != "" {
		request.Header.Set("If-Match", match)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return nil, ""
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	out, _ := decode(payload)
	return out, response.Header.Get("ETag")
}
func (e *Engine) find(typ, system, value string) (map[string]any, string) {
	bundle, _ := e.fhir("GET", typ+"?identifier="+url.QueryEscape(system+"|"+value), "", nil)
	entries, _ := bundle["entry"].([]any)
	for _, item := range entries {
		entry := item.(map[string]any)
		if entry["search"].(map[string]any)["mode"] == "match" {
			res := entry["resource"].(map[string]any)
			return res, fmt.Sprintf(`W/"%s"`, res["meta"].(map[string]any)["versionId"])
		}
	}
	return nil, ""
}

// handle applies one message and returns its control ID for the AA ACK.
func (e *Engine) handle(raw string) string {
	control := field(raw, "MSH", 9)
	mode := e.currentMode()
	event := field(raw, "MSH", 8)
	if strings.HasPrefix(event, "SIU") {
		key := field(raw, "SCH", 1)
		start := field(raw, "SCH", 11)
		status := "booked"
		if strings.HasSuffix(event, "S15") {
			status = "cancelled"
		}
		apply := func() {
			appointment := map[string]any{"resourceType": "Appointment", "identifier": []any{map[string]any{"system": AppointmentSystem, "value": key}}, "status": status, "start": start, "participant": []any{map[string]any{"status": "accepted", "actor": map[string]any{"display": "Synthetic patient"}}}}
			existing, version := e.find("Appointment", AppointmentSystem, key)
			if existing == nil || mode == "defective" {
				// The defective engine never updates: every event creates.
				e.fhir("POST", "Appointment", "", appointment)
				return
			}
			appointment["resourceType"], appointment["id"] = "Appointment", existing["id"]
			e.fhir("PUT", "Appointment/"+existing["id"].(string), version, appointment)
		}
		switch mode {
		case "lost":
			// Acknowledged, never written.
		case "delayed":
			e.done.Add(1)
			go func() { defer e.done.Done(); time.Sleep(60 * time.Millisecond); apply() }()
		case "late-duplicate":
			apply()
			e.done.Add(1)
			go func() {
				defer e.done.Done()
				time.Sleep(60 * time.Millisecond)
				appointment := map[string]any{"resourceType": "Appointment", "identifier": []any{map[string]any{"system": AppointmentSystem, "value": key}}, "status": status, "start": start, "participant": []any{map[string]any{"status": "accepted"}}}
				e.fhir("POST", "Appointment", "", appointment)
			}()
		default:
			apply()
		}
		return control
	}
	if strings.HasPrefix(event, "ORU") {
		e.result(raw, mode)
	}
	return control
}

// result maps an ORU^R01 observation onto a FHIR Observation for the order.
func (e *Engine) result(raw, mode string) {
	mrn := field(raw, "PID", 3)
	placer := field(raw, "OBR", 2)
	filler := field(raw, "OBR", 3)
	value := field(raw, "OBX", 5)
	units := field(raw, "OBX", 6)
	status := "final"
	if field(raw, "OBX", 11) == "C" {
		status = "corrected"
	}
	patient, _ := e.find("Patient", PatientSystem, mrn)
	order, _ := e.find("ServiceRequest", OrderSystem, placer)
	if patient == nil || order == nil {
		return
	}
	subject, basedOn := "Patient/"+patient["id"].(string), "ServiceRequest/"+order["id"].(string)
	switch mode {
	case "wrong-subject":
		decoy, _ := e.find("Patient", PatientSystem, "DECOY")
		subject = "Patient/" + decoy["id"].(string)
	case "wrong-order":
		decoy, _ := e.find("ServiceRequest", OrderSystem, "DECOY")
		basedOn = "ServiceRequest/" + decoy["id"].(string)
	case "precision":
		value = strings.TrimRight(value, "0")
	case "unit":
		units = "mg/dL"
	case "not-corrected":
		status = "final"
	}
	number := json.Number(value)
	observation := map[string]any{"resourceType": "Observation", "identifier": []any{map[string]any{"system": ResultSystem, "value": filler}}, "status": status, "code": map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "2345-7"}}}, "subject": map[string]any{"reference": subject}, "basedOn": []any{map[string]any{"reference": basedOn}}, "valueQuantity": map[string]any{"value": number, "unit": units, "system": "http://unitsofmeasure.org", "code": units}}
	existing, version := e.find("Observation", ResultSystem, filler)
	var stored map[string]any
	if existing == nil || mode == "repeated" {
		stored, _ = e.fhir("POST", "Observation", "", observation)
	} else {
		observation["resourceType"], observation["id"] = "Observation", existing["id"]
		stored, _ = e.fhir("PUT", "Observation/"+existing["id"].(string), version, observation)
	}
	if stored == nil {
		return
	}
	// The report links the order to the result it reports, in step with it.
	report := map[string]any{"resourceType": "DiagnosticReport", "identifier": []any{map[string]any{"system": ResultSystem, "value": filler}}, "status": status, "code": map[string]any{"coding": []any{map[string]any{"system": "http://loinc.org", "code": "24323-8"}}}, "subject": map[string]any{"reference": subject}, "basedOn": []any{map[string]any{"reference": basedOn}}, "result": []any{map[string]any{"reference": "Observation/" + stored["id"].(string)}}}
	current, reportVersion := e.find("DiagnosticReport", ResultSystem, filler)
	if current == nil {
		e.fhir("POST", "DiagnosticReport", "", report)
		return
	}
	report["id"] = current["id"]
	e.fhir("PUT", "DiagnosticReport/"+current["id"].(string), reportVersion, report)
}

const AppointmentSystem = "urn:readmit-lab:appointment"
const PatientSystem = "urn:readmit-lab:mrn"
const OrderSystem = "urn:readmit-lab:placer"
const ResultSystem = "urn:readmit-lab:result"

// Seed starts a freshly provisioned tenant with its unrelated reference
// records and any scenario records Extra adds.
func (l *FHIRLab) Seed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.put("Patient", "decoy-patient", map[string]any{"identifier": []any{map[string]any{"system": PatientSystem, "value": "DECOY"}}}, 0)
	l.put("ServiceRequest", "decoy-order", map[string]any{"identifier": []any{map[string]any{"system": OrderSystem, "value": "DECOY"}}, "status": "active", "intent": "order", "subject": map[string]any{"reference": "Patient/decoy-patient"}}, 0)
	if l.Extra != nil {
		l.Extra()
	}
	l.Writes.Store(0)
}

// RequireSMART registers one client key: tokens are then issued only for a
// correctly signed ES384 client assertion and every resource request needs one.
func (l *FHIRLab) RequireSMART(key *ecdsa.PublicKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.smart, l.jtis, l.scopes = key, map[string]bool{}, map[string]string{}
}

// token validates the client assertion independently of readmit's signer and
// issues an opaque token carrying exactly the requested scopes.
func (l *FHIRLab) token(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.smart == nil || r.ParseForm() != nil {
		http.Error(w, "unsupported", 400)
		return
	}
	parts := strings.Split(r.Form.Get("client_assertion"), ".")
	if len(parts) != 3 {
		http.Error(w, "unsigned", 400)
		return
	}
	var header struct{ Alg, Kid, Typ string }
	var claims struct {
		Iss, Sub, Aud, Jti string
		Exp                int64
	}
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	_ = json.Unmarshal(h, &header)
	_ = json.Unmarshal(c, &claims)
	sum := sha512.Sum384([]byte(parts[0] + "." + parts[1]))
	valid := len(sig) == 96 && ecdsa.Verify(l.smart, sum[:], new(big.Int).SetBytes(sig[:48]), new(big.Int).SetBytes(sig[48:]))
	if !valid || header.Alg != "ES384" || header.Typ != "JWT" || claims.Iss != "lab-client" || claims.Sub != claims.Iss || claims.Aud != l.s.URL+"/token" || claims.Exp <= time.Now().Unix() || len(claims.Jti) < 32 || l.jtis[claims.Jti] || r.Form.Get("grant_type") != "client_credentials" {
		http.Error(w, "invalid client assertion", 400)
		return
	}
	l.jtis[claims.Jti] = true
	l.Tokens.Add(1)
	token := fmt.Sprintf("lab-token-%d", l.Tokens.Load())
	l.scopes[token] = r.Form.Get("scope")
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":300,"scope":%q}`, token, l.scopes[token])
}

// authorized requires, when SMART is registered, a bearer whose granted
// scopes name the resource type with the interaction's permission letter.
func (l *FHIRLab) authorized(r *http.Request, typ string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.smart == nil {
		return true
	}
	scopes, ok := l.scopes[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		return false
	}
	letter := map[string]string{http.MethodGet: "s", http.MethodPost: "c", http.MethodPut: "u", http.MethodDelete: "d"}[r.Method]
	for _, scope := range strings.Fields(scopes) {
		resource, permissions, _ := strings.Cut(strings.TrimPrefix(scope, "system/"), ".")
		if resource == typ && strings.Contains(permissions, letter) {
			l.Bearers.Add(1)
			return true
		}
	}
	return false
}

// SetPageSize sets how many matches one search page holds.
func (l *FHIRLab) SetPageSize(n int) { l.mu.Lock(); l.pageSize = n; l.mu.Unlock() }

// With runs change under the lab's lock, where Put may be called.
func (l *FHIRLab) With(change func()) { l.mu.Lock(); defer l.mu.Unlock(); change() }

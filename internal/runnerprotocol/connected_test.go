package runnerprotocol_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/runnerprotocol"
)

func TestConnectedCapabilitiesAreExactAndOrderIndependent(t *testing.T) {
	good := `{"schema":"readmit-runner-capabilities/v1","engine":"dev","pins":[{"kind":"contract","id":"readmit-connected-test","version":"v1","sha256":""},{"kind":"profile","id":"hl7-251","version":"5","sha256":"` + strings.Repeat("a", 64) + `"}]}`
	c, err := runnerprotocol.DecodeCapabilities([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Identity()
	if err != nil || len(first) != 64 {
		t.Fatalf("capability identity: %q, %v", first, err)
	}
	c.Pins[0], c.Pins[1] = c.Pins[1], c.Pins[0]
	reordered, err := c.Identity()
	if err != nil || reordered != first {
		t.Fatalf("declaration order changed agreement: %q, %v", reordered, err)
	}
	c.Pins[0].Version = "6"
	changed, err := c.Identity()
	if err != nil || changed == first {
		t.Fatalf("different profile version kept agreement: %q, %v", changed, err)
	}
	for _, bad := range []string{
		`null`, `{}`, strings.Replace(good, `,"sha256":""`, ``, 1),
		strings.Replace(good, `"sha256":""`, `"sha256":null`, 1),
		strings.Replace(good, `"kind":"contract"`, `"kind":"invented"`, 1),
		strings.Replace(good, strings.Repeat("a", 64), "", 1),
		strings.Replace(good, `"pins":[`, `"extra":false,"pins":[`, 1),
		strings.Replace(good, `"version":"v1"`, `"version":"v1","extra":false`, 1),
		strings.Replace(good, `"pins":[`, `"pins":null,"unused":[`, 1),
	} {
		if _, err := runnerprotocol.DecodeCapabilities([]byte(bad)); err == nil {
			t.Errorf("admitted malformed capabilities: %s", bad)
		}
	}
	c.Pins = append(c.Pins, c.Pins[0])
	data, _ := json.Marshal(c)
	if _, err := runnerprotocol.DecodeCapabilities(data); err == nil {
		t.Fatal("admitted two meanings for one capability")
	}
	c = runnerprotocol.Capabilities{Schema: runnerprotocol.CapabilitiesSchema, Engine: "v1.0.0+build.1", Pins: []runnerprotocol.CapabilityPin{{Kind: "contract", ID: "readmit-connected-test", Version: "v1"}, {Kind: "contract", ID: "readmit-connected-test", Version: "v2"}, {Kind: "validator", ID: "temurin", Version: "21.0.12.1+1", SHA256: strings.Repeat("b", 64)}}}
	if _, err := c.Identity(); err != nil {
		t.Fatal("exact simultaneous reader and package versions refused", err)
	}
}

func connectedRequest() runnerprotocol.ConnectedRequest {
	return runnerprotocol.ConnectedRequest{Schema: runnerprotocol.ConnectedRequestSchema, Environment: "lab", Instance: "worker-one", Job: "slot-one", Engine: "dev", Input: strings.Repeat("a", 64), Resources: []string{strings.Repeat("b", 64)},
		Capabilities: runnerprotocol.Capabilities{Schema: runnerprotocol.CapabilitiesSchema, Engine: "dev", Pins: []runnerprotocol.CapabilityPin{{Kind: "contract", ID: "readmit-connected-test", Version: "v1"}}}}
}

func TestConnectedReadersKeepEveryAdmissionFieldAndFreezeV1(t *testing.T) {
	request := connectedRequest()
	pin, err := request.Capabilities.Identity()
	if err != nil {
		t.Fatal(err)
	}
	policy := runnerprotocol.ConnectedPolicy{Schema: runnerprotocol.ConnectedPolicySchema, Runners: []runnerprotocol.ConnectedGrant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: "dev", Capability: pin, MaxSeconds: 30, MaxJobs: 2}}}
	lease := runnerprotocol.ConnectedLease{Schema: runnerprotocol.ConnectedLeaseSchema, Expires: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), MaxSeconds: 30, MaxJobs: 2, Generation: 1}
	command := runnerprotocol.ConnectedCommand{Schema: runnerprotocol.ConnectedCommandSchema, Request: request, Generation: 1, Step: "effect-1"}
	for _, sample := range []struct {
		name string
		v    any
		read func([]byte) error
	}{
		{"request", request, func(b []byte) error { _, e := runnerprotocol.DecodeConnectedRequest(b); return e }},
		{"policy", policy, func(b []byte) error { _, e := runnerprotocol.DecodeConnectedPolicy(b); return e }},
		{"lease", lease, func(b []byte) error { _, e := runnerprotocol.DecodeConnectedLease(b); return e }},
		{"command", command, func(b []byte) error { _, e := runnerprotocol.DecodeConnectedCommand(b); return e }},
	} {
		t.Run(sample.name, func(t *testing.T) {
			data, _ := json.Marshal(sample.v)
			if err := sample.read(data); err != nil {
				t.Fatal("valid document refused", err)
			}
			var members map[string]jsontext.Value
			if err := json.Unmarshal(data, &members); err != nil {
				t.Fatal(err)
			}
			for key, original := range members {
				delete(members, key)
				missing, _ := json.Marshal(members)
				if err := sample.read(missing); err == nil {
					t.Errorf("accepted absent %s", key)
				}
				members[key] = jsontext.Value("null")
				null, _ := json.Marshal(members)
				if err := sample.read(null); err == nil {
					t.Errorf("accepted null %s", key)
				}
				members[key] = original
			}
			members["extra"] = jsontext.Value("true")
			extra, _ := json.Marshal(members)
			if sample.read(extra) == nil {
				t.Error("accepted an unknown field")
			}
		})
	}
	for _, read := range []func([]byte) error{
		func(b []byte) error { _, err := runnerprotocol.DecodeRequest(b); return err },
		func(b []byte) error { _, err := runnerprotocol.DecodePolicy(b); return err },
		func(b []byte) error { _, err := runnerprotocol.DecodeLease(b); return err },
	} {
		for _, v := range []any{request, policy, lease} {
			data, _ := json.Marshal(v)
			if read(data) == nil {
				t.Fatal("frozen v1 reader accepted a new connected document")
			}
		}
	}
	wrong := request
	wrong.Engine = "different"
	data, _ := json.Marshal(wrong)
	if _, err := runnerprotocol.DecodeConnectedRequest(data); err == nil {
		t.Fatal("request engine disagreed with capability engine")
	}
	wrong = request
	wrong.Resources = append(wrong.Resources, wrong.Resources[0])
	data, _ = json.Marshal(wrong)
	if _, err := runnerprotocol.DecodeConnectedRequest(data); err == nil {
		t.Fatal("duplicate resource digests admitted")
	}
	command.Outcome = "settled"
	data, _ = json.Marshal(command)
	if _, err := runnerprotocol.DecodeConnectedCommand(data); err == nil {
		t.Fatal("effect and settlement combined into one command")
	}
}

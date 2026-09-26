package collection_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
)

const faultPolicy = `{"schema":"readmit-receiver-policy/v3","name":"fault-sink","source_label":"synthetic-test","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA","application_delivery":"same-connection","application_endpoint":"","approved_transport":false},"faults":{"environment_class":"nonproduction","approved_test_endpoints":["127.0.0.1:2575"],"steps":[{"message":1,"stage":"application","action":"delay","delay_ms":50}]}}`

func TestFaultPolicyRefusesUnboundedUnapprovedAndExecutableDeclarations(t *testing.T) {
	for name, input := range map[string]string{
		"production":                   strings.Replace(faultPolicy, `"nonproduction"`, `"production"`, 1),
		"unclassified":                 strings.Replace(faultPolicy, `"nonproduction"`, `"unclassified"`, 1),
		"missing classification":       strings.Replace(faultPolicy, `"environment_class":"nonproduction",`, ``, 1),
		"named endpoint":               strings.Replace(faultPolicy, `127.0.0.1:2575`, `localhost:2575`, 1),
		"wildcard endpoint":            strings.Replace(faultPolicy, `127.0.0.1:2575`, `0.0.0.0:2575`, 1),
		"mapped wildcard endpoint":     strings.Replace(faultPolicy, `127.0.0.1:2575`, `[::ffff:0.0.0.0]:2575`, 1),
		"ephemeral endpoint":           strings.Replace(faultPolicy, `127.0.0.1:2575`, `127.0.0.1:0`, 1),
		"no approved endpoints":        strings.Replace(faultPolicy, `["127.0.0.1:2575"]`, `[]`, 1),
		"duplicated endpoint":          strings.Replace(faultPolicy, `["127.0.0.1:2575"]`, `["127.0.0.1:2575","[::ffff:127.0.0.1]:2575"]`, 1),
		"delay too long":               strings.Replace(faultPolicy, `"delay_ms":50`, `"delay_ms":30001`, 1),
		"delay negative":               strings.Replace(faultPolicy, `"delay_ms":50`, `"delay_ms":-1`, 1),
		"missing response unbounded":   strings.Replace(strings.Replace(faultPolicy, `"delay"`, `"missing-response"`, 1), `"delay_ms":50`, `"delay_ms":0`, 1),
		"reject delay":                 strings.Replace(faultPolicy, `"delay"`, `"reject"`, 1),
		"too many messages":            strings.Replace(faultPolicy, `"message":1`, `"message":4001`, 1),
		"zero message":                 strings.Replace(faultPolicy, `"message":1`, `"message":0`, 1),
		"bad stage":                    strings.Replace(faultPolicy, `"stage":"application"`, `"stage":"all"`, 1),
		"executable action":            strings.Replace(faultPolicy, `"action":"delay"`, `"action":"exec"`, 1),
		"script field":                 strings.Replace(faultPolicy, `"delay_ms":50`, `"delay_ms":50,"command":"SECRET"`, 1),
		"unknown fault member":         strings.Replace(faultPolicy, `"faults":{`, `"faults":{"allow_production":true,`, 1),
		"null fault":                   strings.Replace(faultPolicy, `"faults":{"environment_class":"nonproduction","approved_test_endpoints":["127.0.0.1:2575"],"steps":[{"message":1,"stage":"application","action":"delay","delay_ms":50}]}`, `"faults":null`, 1),
		"v2 fault":                     strings.Replace(faultPolicy, "readmit-receiver-policy/v3", "readmit-receiver-policy/v2", 1),
		"unapproved separate endpoint": strings.Replace(strings.Replace(faultPolicy, `"application_delivery":"same-connection"`, `"application_delivery":"separate-endpoint"`, 1), `"application_endpoint":""`, `"application_endpoint":"127.0.0.1:2576"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := collection.DecodePolicy([]byte(input)); err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unsafe declaration accepted or leaked: %v", err)
			}
		})
	}
}

func TestFaultPolicyBoundsAndRoundTrip(t *testing.T) {
	p, err := collection.DecodePolicy([]byte(faultPolicy))
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"127.0.0.1:2576", "localhost:2575", "0.0.0.0:2575"} {
		if p.Faults.ApproveEndpoint(address) == nil {
			t.Fatal("unapproved endpoint accepted")
		}
	}
	if err := p.Faults.ApproveEndpoint("[::ffff:127.0.0.1]:2575"); err != nil {
		t.Fatal(err)
	}
	data, err := collection.EncodePolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	q, err := collection.DecodePolicy(data)
	if err != nil || q.Faults.Step(1).DelayMS != 50 || q.Faults.Step(2) != nil {
		t.Fatal("fault plan changed")
	}
	for i := 2; i <= 64; i++ {
		p.Faults.Steps = append(p.Faults.Steps, collection.FaultStep{Message: i, Stage: "accept", Action: "reject"})
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Faults.Steps = append(p.Faults.Steps, collection.FaultStep{Message: 65, Stage: "accept", Action: "reject"})
	if p.Validate() == nil {
		t.Fatal("unbounded step count accepted")
	}
	p.Faults.Steps = p.Faults.Steps[:64]
	p.Faults.Steps[1].Message = 1
	if p.Validate() == nil {
		t.Fatal("duplicate message step accepted")
	}
}

func TestFaultRecordVersionAndExecutionEventsRoundTrip(t *testing.T) {
	record := sample(t)
	record.Schema = collection.FaultSchema
	record.Policy = policy(t, faultPolicy)
	record.Received[0].Application.Destination = collection.SameConnection
	record.Received[0].Fault = &collection.FaultEvent{Action: "delay", Stage: "application", Status: "completed"}
	data, err := collection.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := collection.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := collection.Encode(decoded)
	if err != nil || string(again) != string(data) {
		t.Fatal("v3 record round trip changed evidence")
	}
	for name, input := range map[string]string{
		"older record":           strings.Replace(string(data), "readmit-collection/v3", "readmit-collection/v2", 1),
		"older policy":           strings.Replace(string(data), "readmit-receiver-policy/v3", "readmit-receiver-policy/v2", 1),
		"undeclared action":      strings.Replace(string(data), `"status":"completed"`, `"status":"pass"`, 1),
		"wrong execution action": strings.Replace(string(data), `"fault":{"action":"delay"`, `"fault":{"action":"reject"`, 1),
		"null execution":         strings.Replace(string(data), `{"action":"delay","stage":"application","status":"completed"}`, `null`, 1),
		"unknown event member":   strings.Replace(string(data), `"status":"completed"`, `"status":"completed","script":"SECRET"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := collection.Decode([]byte(input)); err == nil {
				t.Fatal("invalid execution event accepted")
			}
		})
	}
	legacy, err := collection.Encode(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	legacyWithFault := strings.Replace(string(legacy), `"received":[{`, `"received":[{"fault":null,`, 1)
	if _, err := collection.Decode([]byte(legacyWithFault)); err == nil {
		t.Fatal("v2 member set accepted fault null")
	}
}

// Every fault action offered is one a step can declare, with a delay exactly
// when it waits.
func TestFaultActionsAreTheActionsAStepDeclares(t *testing.T) {
	for _, offered := range collection.FaultActions() {
		for _, delay := range []int{0, 50} {
			policy := collection.FaultPolicy{
				EnvironmentClass: "nonproduction", ApprovedTestEndpoints: []string{"127.0.0.1:2575"},
				Steps: []collection.FaultStep{{Message: 1, Stage: collection.ApplicationStage, Action: offered.Action, DelayMS: delay}},
			}
			if err := policy.Validate(); (err == nil) != (offered.Waits == (delay > 0)) {
				t.Errorf("%s (waits %t) with delay %d: %v", offered.Action, offered.Waits, delay, err)
			}
		}
		if collection.FaultWaits(offered.Action) != offered.Waits {
			t.Errorf("%s is offered as waiting %t", offered.Action, offered.Waits)
		}
	}
}

package connectedrun_test

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func capturePrepared(t *testing.T, dir string, target *target) (*connectedrun.Prepared, string) {
	t.Helper()
	intervalPrepared(t, dir, target)
	old, err := connectedtest.OpenPlan(filepath.Join(dir, "interval-plan"))
	if err != nil {
		t.Fatal(err)
	}
	d := old.Document().Test
	members := old.Files()
	supplied := map[string][]byte{}
	retain := func(ref connectedtest.Reference) { supplied[ref.File] = members["dependencies/"+ref.SHA256] }
	retain(d.Checks)
	for _, step := range d.Steps {
		retain(step.V2.Input)
	}
	for _, ds := range d.Datasets {
		retain(*ds.Projection)
		retain(*ds.Completion.Policy)
	}
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	reserve.Close()
	_, port, _ := net.SplitHostPort(address)
	number, _ := strconv.Atoi(port)
	raw, err := os.ReadFile(filepath.Join(dir, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var policy sendpolicy.ScopedPolicy
	if err = json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: "after", Operation: sendpolicy.CaptureListen, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
	write(t, filepath.Join(dir, "policy.json"), policy)
	raw, _ = os.ReadFile(filepath.Join(dir, "policy.json"))
	d.Environment.AddressPolicyIdentity = dataset.Digest(raw)
	source := observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchema, Address: address, ReceiverPolicy: collection.Policy{Schema: collection.PolicySchemaV1, Name: "owned", SourceLabel: "independent-output", Acknowledgement: collection.AckRule{Operator: collection.FixedCodeOperator, Code: "AA"}, AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}, TimeoutMS: 10000, MaxFrameBytes: 4096, MaxBytes: 65536, MaxMessages: 10, MaxConnections: 4, MaxSessions: 10, RunSelector: "ZRN-1"}
	write(t, filepath.Join(dir, "capture-source.json"), source)
	sourceRaw, _ := os.ReadFile(filepath.Join(dir, "capture-source.json"))
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "outputs", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "SCH-1.1", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	raw, _ = json.Marshal(projection)
	supplied["output-projection.json"] = raw
	ref := connectedtest.Reference{Project: d.Project, ID: "outputs", Schema: dataset.ProjectionSchema, File: "output-projection.json", SHA256: dataset.Digest(raw)}
	for i := range d.Datasets {
		ds := &d.Datasets[i]
		if ds.ID != "after" {
			continue
		}
		ds.Source = dataset.Digest(sourceRaw)
		ds.Projection = &ref
		def, err := observeinterval.Decode(supplied[ds.Completion.Policy.File])
		if err != nil {
			t.Fatal(err)
		}
		def.Source = ds.Source
		def.Mode = "stream"
		def.Freshness = "ingress"
		raw, _ = json.Marshal(def)
		supplied[ds.Completion.Policy.File] = raw
		ds.Completion.Policy.SHA256 = dataset.Digest(raw)
	}
	var checks assertion.DatasetSetDocument
	if err = json.Unmarshal(supplied[d.Checks.File], &checks); err != nil {
		t.Fatal(err)
	}
	two := 2
	checks.Assertions = []assertion.DatasetAssertion{checks.Assertions[0], {ID: "two-outputs", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &two}}
	for i := range checks.Bindings {
		if checks.Bindings[i].Name == "after" {
			checks.Bindings[i].Source = dataset.Digest(sourceRaw)
			checks.Bindings[i].ProjectionIdentity = projection.Identity()
		}
	}
	raw, _ = json.Marshal(checks)
	supplied[d.Checks.File] = raw
	d.Checks.SHA256 = dataset.Digest(raw)
	raw, _ = json.Marshal(d)
	plan, err := connectedtest.Compile(raw, supplied, old.Document().Generation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "capture-plan")
	if err = plan.Write(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	var config connectedrun.ConfigV2
	raw, _ = os.ReadFile(filepath.Join(dir, "interval-execution.json"))
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config.Definition.Sources["after"] = connectedrun.SourceSelection{Path: "capture-source.json", Grant: &connectedrun.Grant{Path: "capture-grant.json", Actor: "collector", Generation: "1"}}
	configPath := filepath.Join(dir, "capture-execution.json")
	write(t, configPath, config)
	prepared, err := connectedrun.Prepare(path, configPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, binding := range prepared.Bindings() {
		file, actor := "grant.json", "runner"
		if name == "dataset:after" {
			file, actor = "capture-grant.json", "collector"
		}
		write(t, filepath.Join(dir, file), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: actor, Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	return prepared, address
}
func independentOutput(t *testing.T, address, run string) {
	t.Helper()
	c, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	raw := "\x0bMSH|^~\\&|ENGINE|LAB|||20260101||SIU^S12|OUTPUT|P|2.5.1\rSCH|SAME\rZRN|" + run + "\r\x1c\r"
	if _, err = c.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(c)
	if _, err = reader.ReadString('\x1c'); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.ReadByte(); err != nil {
		t.Fatal(err)
	}
}
func TestConnectedCaptureStartsBeforeACKAndRetainsLateDuplicate(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	target.notifications = make(chan int, 2)
	prepared, address := capturePrepared(t, dir, target)
	var beforeACK atomic.Int32
	target.setOutput(func(_ int, _ string) { independentOutput(t, address, "run-one"); beforeACK.Add(1) })
	clock := &intervalClock{base: time.Now().UTC(), waits: make(chan chan time.Duration)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type answer struct {
		r   connectedrun.Result
		err error
	}
	done := make(chan answer, 1)
	output := filepath.Join(dir, "run")
	go func() {
		r, err := connectedrun.ExecuteWithClock(ctx, prepared, "run-one", output, clock)
		done <- answer{r, err}
	}()
	samples := 0
	for {
		select {
		case advance := <-clock.waits:
			samples++
			if samples == 1 {
				for i := 0; i < 2; i++ {
					select {
					case <-target.notifications:
					case <-ctx.Done():
						t.Fatal("no target ACK")
					}
				}
				waitStimulusFinish(t, ctx, output)
				if beforeACK.Load() != 2 {
					t.Fatal("output did not precede ACK")
				}
			}
			if samples == 8 {
				independentOutput(t, address, "run-one")
			}
			advance <- 10 * time.Millisecond
		case answer := <-done:
			if answer.err != nil || answer.r.State != "complete" || answer.r.Verdict != assertion.VerdictFail || answer.r.Boundaries()["after"] != "full-bounded-horizon" {
				t.Fatal(answer.r, answer.err)
			}
			result, capture, err := networkaction.OpenCapture(filepath.Join(output, "intervals", "after", "capture"))
			if err != nil || result.State != "captured" {
				t.Fatal(result, err)
			}
			count := 0
			for _, event := range capture.Events {
				if event.Direction == bundle.Inbound {
					count++
					raw, _ := capture.Raw(event.ID)
					if !strings.Contains(string(raw), "SCH|SAME") {
						t.Fatal("wire evidence changed")
					}
				}
			}
			if count != 3 || target.received.Load() != 2 {
				t.Fatal(count, target.received.Load())
			}
			if _, err = connectedrun.Open(ctx, output); err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal("capture runtime did not finish")
		}
	}
}

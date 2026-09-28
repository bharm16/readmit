package report

import (
	"context"
	"errors"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/exportreview"
)

// One disclosure pipeline behind both extract versions. Each policy version
// stays an adapter at this seam: it decodes its document, builds its derived
// material and names its own sentences, while opening, inventorying,
// blocking, approving, publishing and verifying run here once.

// disclosedPacket is a retained packet opened for disclosure with its
// directory and inventory.
type disclosedPacket struct {
	packet    *ConnectedPacket
	dir       string
	inventory []InventoryItem
}

func openDisclosedPacket(ctx context.Context, packetPath string) (*disclosedPacket, error) {
	packet, err := OpenConnected(ctx, packetPath)
	if err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(packetPath)
	if err != nil {
		return nil, err
	}
	inventory, err := packet.Inventory(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &disclosedPacket{packet: packet, dir: dir, inventory: inventory}, nil
}

// applyDisclosurePolicy maps every inventoried surface through the policy and
// lists every reason the extract cannot be published: credential material,
// which no policy can admit, or a surface the policy does not map.
func applyDisclosurePolicy(inventory []InventoryItem, surfaces map[string]string) []string {
	blocked := []string{}
	for i, item := range inventory {
		switch {
		case item.Surface == SurfaceCredential:
			blocked = append(blocked, "credential material (a token or private key) is retained in "+strconv.Itoa(item.Files)+" files; no policy can admit it and no share-oriented export is made")
		case surfaces[item.Surface] == "":
			blocked = append(blocked, "the policy does not map the "+item.Surface+" surface held in "+strconv.Itoa(item.Files)+" files")
		default:
			inventory[i].Disposition = surfaces[item.Surface]
		}
	}
	return blocked
}

// scanResidual scans one encoded extract for retained values and appends the
// policy's block reason when one reached it through any path.
func scanResidual(blocked []string, scanned map[string][]byte, terms [][]byte, reason string) ([]string, exportreview.Scan) {
	scan := exportreview.Residual(scanned, terms)
	if scan.Status != "passed" {
		blocked = append(blocked, reason)
	}
	return blocked, scan
}

// disclosedPublish is one prepared extract behind the shared publish gate.
type disclosedPublish struct {
	packet, policy string
	blocked        []string
	identity       string
	// changed names what a fresh preparation may have found different:
	// "evidence or policy", or "evidence, policy or key".
	changed  string
	family   artifactdir.Family
	repare   func(context.Context) (files []disclosedFile, blocked []string, identity string, err error)
	approval string
	output   string
}

// disclosedFile is one file of a published extract, in write order.
type disclosedFile struct {
	name string
	data []byte
}

// publishDisclosed writes one prepared extract only when nothing blocks it
// and a fresh preparation yields exactly the approved bytes. It transmits
// nothing.
func publishDisclosed(ctx context.Context, publish disclosedPublish) error {
	if len(publish.blocked) > 0 {
		return errors.New("the extract is blocked: " + strings.Join(publish.blocked, "; "))
	}
	files, blocked, identity, err := publish.repare(ctx)
	if err != nil {
		return err
	}
	if publish.approval != publish.identity || identity != publish.identity || len(blocked) > 0 {
		return errors.New("the extract requires approval of its exact current identity; changed " + publish.changed + " requires review again")
	}
	protected := []os.FileInfo{}
	for _, path := range []string{publish.packet, publish.policy} {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		protected = append(protected, info)
	}
	path, err := artifactpath.Destination(publish.output, protected...)
	if err != nil {
		return err
	}
	w, err := artifactdir.Create(path, publish.family, artifactdir.Durable)
	if err != nil {
		return err
	}
	defer w.Close()
	for _, file := range files {
		if err := w.WriteFile(file.name, file.data); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = w.Seal(nil)
	return err
}

// verifyDisclosedSeal reads one published extract directory and verifies its
// seal over the raw document, returning both.
func verifyDisclosedSeal(dir string, layout artifactdir.Layout, invalid error) (map[string][]byte, []byte, error) {
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, nil, err
	}
	files, err := artifactdir.Read(dir, layout)
	if err != nil {
		return nil, nil, invalid
	}
	raw := files["extract.json"]
	if string(files["identity.sha256"]) != digest(raw)+"\n" {
		return nil, nil, invalid
	}
	return files, raw, nil
}

// inventoryAdmitted reports whether every inventoried surface carries a
// disposition the version admits. Credential material is never admitted.
func inventoryAdmitted(inventory []InventoryItem, admit func(InventoryItem) bool) bool {
	for _, item := range inventory {
		if item.Surface == SurfaceCredential || !admit(item) {
			return false
		}
	}
	return true
}

// valueFreeRuns summarizes every retained lifecycle by position and claim:
// states, verdicts, declared boundaries and check outcomes, never authored
// text or a value.
func valueFreeRuns(packet *ConnectedPacket) []ExtractRun {
	out := []ExtractRun{}
	runs := map[string]*ConnectedRun{"current": &packet.Manifest.Current, "baseline": packet.Manifest.Baseline, "replay": packet.Manifest.Replay}
	for _, section := range connectedSections {
		r := runs[section]
		if r == nil {
			continue
		}
		run := ExtractRun{Section: section, Identity: r.Identity, Schema: r.Schema, Plan: r.Plan, State: r.State, Verdict: r.Verdict, Setup: r.Setup, Cleanup: r.Cleanup, Boundary: r.Boundary, Classification: r.Environment.Classification, Boundaries: []string{}, Phases: []ExtractPhase{}}
		for _, q := range r.Qualification {
			run.Boundaries = append(run.Boundaries, q.Boundary)
		}
		for i, phase := range r.Phases {
			p := ExtractPhase{Position: i + 1, State: phase.State, Verdict: phase.Verdict, Checks: []ExtractCheck{}}
			for j, check := range phase.Checks {
				p.Checks = append(p.Checks, ExtractCheck{Position: j + 1, Claim: checkClaim(check.ID), Outcome: string(check.Outcome)})
			}
			run.Phases = append(run.Phases, p)
		}
		out = append(out, run)
	}
	return out
}

// valueFreeComparison is the packet's baseline comparison by dimension state,
// behavior and record counts only.
func valueFreeComparison(packet *ConnectedPacket) *ExtractComparison {
	cmp := packet.Comparison
	if cmp == nil {
		return nil
	}
	ec := &ExtractComparison{Dimensions: []string{}, Attribution: cmp.Attribution.Outcome, Behavior: []string{}, Records: []string{}}
	for _, d := range cmp.Dimensions {
		ec.Dimensions = append(ec.Dimensions, d.Dimension+": "+d.State)
	}
	for _, check := range cmp.Checks {
		ec.Behavior = append(ec.Behavior, check.Baseline+" -> "+check.Current+"; definition "+check.Definition+"; behavior "+check.Behavior)
	}
	for _, r := range cmp.Records {
		if r.State != "compared" {
			ec.Records = append(ec.Records, r.State)
			continue
		}
		for _, k := range r.Keys {
			ec.Records = append(ec.Records, strconv.Itoa(k.Baseline)+" -> "+strconv.Itoa(k.Current)+"; "+k.State+"; "+strconv.Itoa(len(k.Values))+" field columns; "+strconv.Itoa(len(k.Identities))+" server-assigned columns")
		}
	}
	return ec
}

// knownValues are the values the packet's lifecycles observed, bound and
// declared: every retained record field, every server-assigned identity, the
// server bases, environment names and target revision. Short and purely
// numeric values would match the extract's own counts and digests, so a
// known value needs four characters and a letter.
func knownValues(p *ConnectedPacket) [][]byte {
	seen := map[string]bool{}
	add := func(v string) {
		if len(v) >= 4 && strings.IndexFunc(v, unicode.IsLetter) >= 0 && !hexDigest(v) {
			seen[v] = true
		}
	}
	for _, e := range p.evidence {
		env := e.Plan.Document().Test.Environment
		for _, v := range []string{env.Project, env.ID, env.Name, env.Endpoint, env.TargetRevision.Value} {
			add(v)
		}
		for _, s := range e.Plan.Document().Test.Servers {
			add(s.Base)
		}
		for _, phase := range e.Phases {
			for _, t := range phase.Tables {
				for _, row := range t.Rows {
					for _, v := range row.Values {
						add(v.Text)
						for _, item := range v.Items {
							add(item.Text)
						}
					}
				}
			}
			for _, v := range phase.Bound {
				add(v)
			}
			for _, v := range phase.Inputs {
				add(v)
			}
		}
	}
	terms := [][]byte{}
	for _, v := range slices.Sorted(maps.Keys(seen)) {
		terms = append(terms, []byte(v))
	}
	return terms
}

func hexDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	return strings.IndexFunc(v, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) }) < 0
}

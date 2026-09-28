package desktop

import (
	"context"
	"errors"
	"net"
	"slices"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/replay"
)

// Saving an environment never approves its transport. A person approves the
// transport to one address as its own reviewed action, which records the
// approval in a new revision of the environment; an edit that changes the
// address, the transport or a TLS setting clears it again, and a check, a
// send or a reset through an address that needs an approval is refused
// until one is recorded.

// ApproveTransportAction records the approval of an environment's transport.
const ApproveTransportAction ActionID = "environment.approve-transport"

// TransportReview is exactly what one approval covers: the address, the
// transport and its TLS settings, as the environment's revision records them.
type TransportReview struct {
	Address           string `json:"address"`
	Transport         string `json:"transport"`
	ServerName        string `json:"server_name,omitzero"`
	CAFile            string `json:"ca_file,omitzero"`
	ClientCertificate string `json:"client_certificate,omitzero"`
	Classification    string `json:"classification"`
}

// approvalRequired reports whether a target's address is one the target
// reader refuses to reach without an approved transport: anything but a
// loopback address literal.
func approvalRequired(target replay.Target) bool {
	host, _, err := net.SplitHostPort(target.Address)
	ip := net.ParseIP(host)
	return err != nil || ip == nil || !ip.IsLoopback()
}

// approvalReason words a target refused for its unapproved transport as the
// action that settles it.
func approvalReason(err error) string {
	if errors.Is(err, replay.ErrTransportNotApproved) {
		return "the transport to this address is not approved; approve the transport first"
	}
	return err.Error()
}

type transportBinding struct {
	item, base string
	members    []catalog.Staged
}

func bindApproveTransport(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != EnvironmentItem {
		return nil, refusal{Failed, "a transport approval is reviewed for one environment"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	members, err := loaded.environmentOf(records[0])
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	target := members.target
	ready, reason := !target.ApprovedTransport, ""
	if !ready {
		reason = "the transport to this address is already approved"
	}
	approved := target
	approved.ApprovedTransport = true
	_, data, err := declaredTarget(approved)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	staged := []catalog.Staged{{Role: "target", File: "target.json", Data: data}}
	roles := []string{}
	for role := range members.paths {
		if role != "target" {
			roles = append(roles, role)
		}
	}
	slices.Sort(roles)
	for _, role := range roles {
		data, err := boundedFile(members.paths[role], catalog.MaxMemberBytes)
		if err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		staged = append(staged, catalog.Staged{Role: role, File: role + ".json", Data: data})
	}
	review := &TransportReview{Address: target.Address, Transport: target.Transport, ServerName: target.ServerName, CAFile: target.CAFile,
		ClientCertificate: target.ClientCertificate, Classification: string(target.Environment().Classification)}
	parts := []string{string(ApproveTransportAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
		records[0].ID, items[0].Ref.Revision}
	for _, role := range append([]string{"target"}, roles...) {
		parts = append(parts, role, memberDigest(records[0], role))
	}
	return &boundAction{action: ApproveTransportAction, origin: request,
		transport: &transportBinding{item: records[0].ID, base: items[0].Ref.Revision, members: staged},
		binding:   binding(parts...),
		review: ActionReview{Items: items, Ready: ready, Refusal: reason, Transport: review,
			Destination: ReviewDestination{Name: target.Name, Classification: review.Classification, Address: target.Address}}}, noRefusal
}

// executeApproveTransport publishes the environment's revision again with
// its transport approved and every other member exactly as reviewed.
func executeApproveTransport(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{Outcome: ActionRefused}
	if err := a.admitAuthor(); err != nil {
		result.refuse(PermissionDenied, err.Error())
		return result
	}
	loaded, declined := a.loadCatalog(ctx, bound.origin.Context, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	index := loaded.document.Find(bound.transport.item)
	if index < 0 {
		result.refuse(Failed, "the project holds no such environment")
		return result
	}
	intent := "approve-" + bound.binding[:32]
	saved, err := loaded.store.Save(catalog.Draft{Kind: string(EnvironmentItem), ItemID: bound.transport.item, Name: loaded.document.Items[index].Name,
		Base: bound.transport.base, Intent: intent, Digest: bound.binding, Author: a.reviewerName(), Members: bound.transport.members},
		verifierFor(EnvironmentItem), catalog.Options{Now: a.now, Fault: a.saveFault})
	if err != nil {
		result.refuse(Failed, "the approval could not be recorded; the environment is unchanged")
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	result.Approved = &ItemRef{Kind: EnvironmentItem, ID: saved.Item.ID, Revision: revisionLabel(saved.Revision)}
	return result
}

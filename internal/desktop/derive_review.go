package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/redact"
)

// A derivation of an export review is a reviewed action of its own: the
// original-artifact inventory it runs over declares its scope complete, and
// that declaration is the person's, made for exactly the inventory the review
// shows, with the final click. It is a specific decision the derivation
// requires, never the generic consent of a Send or an Export, and a stored
// flag in the inventory alone never stands in for it.

// DeriveReviewAction derives an export review of one case.
const DeriveReviewAction ActionID = "export.derive-review"

// DeriveConsent: the displayed export review and its private local state are
// written as new entries of the project; nothing leaves the project.
const DeriveConsent Consent = "derive"

// InventoryDeclarationRequirement: the original-artifact inventory is
// declared complete, for the exact inventory the review shows, in the final
// click.
const InventoryDeclarationRequirement ReviewRequirement = "inventory-declaration"

// DeriveReviewOptions name the original specification, the disclosure policy
// and the original-artifact inventory an export review of the scoped case is
// derived with, each one entry of the project.
type DeriveReviewOptions struct {
	Spec      string `json:"spec"`
	Policy    string `json:"policy"`
	Inventory string `json:"inventory"`
}

// DeriveReviewView is what one derivation reads and writes: its inputs by
// entry, the inventory whose completeness the person declares, and the
// review and private local-state entries it writes.
type DeriveReviewView struct {
	Spec      string               `json:"spec"`
	Policy    string               `json:"policy"`
	Inventory InventoryDeclaration `json:"inventory"`
	Review    string               `json:"review"`
	Private   string               `json:"private"`
}

// InventoryDeclaration is the exact inventory a declaration is made for: its
// entry, every original artifact it lists, how many known residual values it
// holds (never the values) and its digest, which the declaration names.
type InventoryDeclaration struct {
	Entry          string                    `json:"entry"`
	Artifacts      []redact.OriginalArtifact `json:"artifacts"`
	ResidualValues int                       `json:"residual_values"`
	Digest         string                    `json:"digest"`
}

func bindDeriveReview(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != CaseItem || request.DeriveReview == nil {
		return nil, refusal{Failed, "an export review is derived from one case"}
	}
	options := *request.DeriveReview
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	entry := records[0].Entry
	facts, _, err := operation.VerifiedCase(loaded.root, entry)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	derivation := PrivacyReviewRequest{Workspace: loaded.root, Case: entry, Spec: options.Spec, Policy: options.Policy, Inventory: options.Inventory}
	_, specPath, policyPath, inventoryPath, reason, ok := privacyInputs(loaded.root, derivation)
	if !ok {
		return nil, refusal{Failed, reason}
	}
	data, err := boundedFile(inventoryPath, privacyDocumentLimit)
	if err != nil {
		return nil, refusal{Failed, "the original-artifact inventory cannot be read"}
	}
	inventory, err := redact.DecodeInventory(data)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	review, refused := destinationFor(loaded.root, "", "review")
	if refused.state != "" {
		return nil, refused
	}
	private, refused := destinationFor(loaded.root, "", "review-private")
	if refused.state != "" {
		return nil, refused
	}
	derivation.Output, derivation.LocalState = review.Name, private.Name
	declaration := InventoryDeclaration{Entry: options.Inventory, Artifacts: inventory.Artifacts, ResidualValues: len(inventory.ResidualValues),
		Digest: fileDigest(inventoryPath)}
	if declaration.Artifacts == nil {
		declaration.Artifacts = []redact.OriginalArtifact{}
	}
	ready := review.Fresh && private.Fresh
	return &boundAction{action: DeriveReviewAction, origin: request, derive: derivation,
		binding: binding(string(DeriveReviewAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
			facts.Identity, entry, options.Spec, fileDigest(specPath), options.Policy, fileDigest(policyPath), options.Inventory, declaration.Digest,
			review.Name, private.Name),
		review: ActionReview{Items: items, Ready: ready, Refusal: destinationsReason(review, private),
			Derive:      &DeriveReviewView{Spec: options.Spec, Policy: options.Policy, Inventory: declaration, Review: review.Name, Private: private.Name},
			Destination: ReviewDestination{Output: review.Name}}}, noRefusal
}

// destinationsReason is why either of a derivation's two destinations cannot be
// written, or nothing.
func destinationsReason(review, private RunDestination) string {
	if !review.Fresh {
		return review.Reason
	}
	if !private.Fresh {
		return private.Reason
	}
	return ""
}

// executeDeriveReview derives the bound review under the declaration made
// for the inventory it showed.
func executeDeriveReview(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	derived := deriveExportReview(ctx, bound.derive)
	result := ReviewedActionResult{State: derived.State, Reason: derived.Reason, Derived: derived.Outcome, Outcome: ActionCompleted}
	switch {
	case derived.State == Cancelled:
		result.Outcome = ActionCancelled
	case derived.Outcome == nil:
		result.Outcome = ActionRefused
	}
	return result
}

// declaredInventory is the refusal of a derivation whose final click did not
// declare the exact inventory it showed complete, or nothing.
func declaredInventory(bound *boundAction, decisions ReviewDecisions) string {
	if bound.review.Derive == nil || decisions.DeclaredInventory != bound.review.Derive.Inventory.Digest {
		return "the original-artifact inventory shown is declared complete before a derivation; nothing was written"
	}
	return ""
}

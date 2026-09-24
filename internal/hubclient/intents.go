package hubclient

import (
	"context"
	"errors"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

var (
	// ErrReleaseReviewKind reports a release review of another kind.
	ErrReleaseReviewKind = errors.New("a release review posts review-request or approval")

	// ErrSupportReviewKind reports a sharing decision of another kind.
	ErrSupportReviewKind = errors.New("a sharing decision posts support-policy, support-request or support-approval")

	// ErrNoReleaseRequest reports an approval of release content no review
	// request names.
	ErrNoReleaseRequest = errors.New("no review request names this exact release content; ask the author to request review, then approve the new request")

	// ErrNoSupportPolicy reports a support request in a project with no
	// sharing policy in force.
	ErrNoSupportPolicy = errors.New("the project has no sharing policy in force; announce it with support-policy before requesting approval")

	// ErrOtherSupportPolicy reports a support request for a summary made under
	// a policy other than the one in force.
	ErrOtherSupportPolicy = errors.New("this summary names another sharing policy; announce the policy it names, or publish under the policy in force")

	// ErrNoSupportRequest reports an approval of a summary no support
	// request names.
	ErrNoSupportRequest = errors.New("no support request names this exact summary; ask the publisher to request review, then approve the new request")
)

// ReleaseReview is an expectation-release review decision: a review request
// naming a subject, or the approval of the outstanding request that names the
// same release content. Release is the SHA-256 digest of the exact bytes the
// application verified as a released expectation, and Path is the file
// holding them, uploaded with a request.
type ReleaseReview struct {
	Project   string
	ID        string
	Kind      string
	Release   string
	Path      string
	Recipient string
	Text      string
}

// PostReleaseReview records a release review against the project's history
// as it stands now. A review request uploads the release, checks the hub
// stored exactly the verified bytes, and names their digest as evidence and
// release; an approval answers the last review request naming the same
// release, with its evidence. The command expects the head that read
// answered, so a decision recorded meanwhile is a conflict to renew, never
// overwritten. Identity is the session's; the hub re-reads the release and
// enforces the request and approval chain.
func (c *Client) PostReleaseReview(ctx context.Context, r ReleaseReview) (hubprotocol.ReviewEvent, bool, error) {
	var zero hubprotocol.ReviewEvent
	if r.Kind != "review-request" && r.Kind != "approval" {
		return zero, false, ErrReleaseReviewKind
	}
	history, err := c.ListHistory(ctx, r.Project)
	if err != nil {
		return zero, false, err
	}
	command := hubprotocol.ReviewCommand{
		Schema: hubprotocol.ReviewCommandV1,
		ID:     r.ID, Expected: history.Head, Kind: r.Kind,
		Evidence: r.Release, Release: r.Release, Text: r.Text,
	}
	if r.Kind == "approval" {
		request, ok := hubprotocol.DeriveReviews(history.Events).ReleaseRequest(r.Release)
		if !ok {
			return zero, false, ErrNoReleaseRequest
		}
		command.Parent = request.Command.ID
		command.Evidence = request.Command.Evidence
	} else {
		command.Recipient = r.Recipient
		if err := c.uploadExactly(ctx, r.Project, r.Path, r.Release, "the uploaded release does not name the reviewed bytes; nothing was requested"); err != nil {
			return zero, false, err
		}
	}
	return c.PostReview(ctx, r.Project, command)
}

// SupportReview is a sharing decision: announcing the project's sharing
// policy (support-policy), asking a subject to approve a published value-free
// summary (support-request), or approving the request that names the same
// summary (support-approval). Digest is the SHA-256 of the exact bytes the
// application verified — the policy, or the summary — and Path is the file
// holding them, uploaded with a policy or a request. SummaryPolicy is the
// policy identity a requested summary names.
type SupportReview struct {
	Project       string
	ID            string
	Kind          string
	Digest        string
	Path          string
	Recipient     string
	SummaryPolicy string
}

// PostSupportReview records a sharing decision against the project's history
// as it stands now. A request binds to the policy in force, which the history
// derives as the hub does, and is refused before anything is sent when there
// is none or the summary names another; an approval answers the last support
// request naming the same summary. The uploaded bytes must be the verified
// ones. The hub stays the authority for roles, chains and the policy in force.
func (c *Client) PostSupportReview(ctx context.Context, r SupportReview) (hubprotocol.ReviewEvent, bool, error) {
	var zero hubprotocol.ReviewEvent
	switch r.Kind {
	case "support-policy", "support-request", "support-approval":
	default:
		return zero, false, ErrSupportReviewKind
	}
	history, err := c.ListHistory(ctx, r.Project)
	if err != nil {
		return zero, false, err
	}
	reviews := hubprotocol.DeriveReviews(history.Events)
	command := hubprotocol.ReviewCommand{
		Schema: hubprotocol.ReviewCommandV2,
		ID:     r.ID, Expected: history.Head, Kind: r.Kind,
		Evidence: r.Digest, Text: "support",
	}
	switch r.Kind {
	case "support-policy":
		if err := c.uploadExactly(ctx, r.Project, r.Path, r.Digest, "the uploaded policy does not name the reviewed bytes; nothing was announced"); err != nil {
			return zero, false, err
		}
	case "support-request":
		policy, ok := reviews.PolicyInForce()
		if !ok {
			return zero, false, ErrNoSupportPolicy
		}
		if r.SummaryPolicy != policy.Evidence {
			return zero, false, ErrOtherSupportPolicy
		}
		command.Release = policy.Evidence
		command.Parent = policy.ID
		command.Recipient = r.Recipient
		if err := c.uploadExactly(ctx, r.Project, r.Path, r.Digest, "the uploaded summary does not name the reviewed bytes; nothing was requested"); err != nil {
			return zero, false, err
		}
	case "support-approval":
		request, ok := reviews.SupportRequest(r.Digest)
		if !ok {
			return zero, false, ErrNoSupportRequest
		}
		command.Parent = request.Command.ID
		command.Release = request.Command.Release
	}
	return c.PostReview(ctx, r.Project, command)
}

// uploadExactly uploads the file at path and refuses, with mismatch, a store
// that does not name digest: the file changed after it was verified.
func (c *Client) uploadExactly(ctx context.Context, project, path, digest, mismatch string) error {
	transfer, err := c.UploadArtifact(ctx, project, path)
	if err != nil {
		return err
	}
	if transfer.Digest != digest {
		return errors.New(mismatch)
	}
	return nil
}

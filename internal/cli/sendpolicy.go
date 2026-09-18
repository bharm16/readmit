package cli

import (
	"fmt"
	"io"

	"github.com/bharm16/readmit/internal/sendpolicy"
)

// readSendPolicy reads the approved-destination document a command selected, or
// reports that none was selected. One reader serves every command, so a policy
// that a diagnosis accepts is the policy a send is held to.
func readSendPolicy(path string) (*sendpolicy.Policy, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readInputFile(path, sendpolicy.MaxPolicyBytes)
	if err != nil {
		return nil, err
	}
	policy, err := sendpolicy.DecodePolicy(data)
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

// writeDecisionLines renders one decision and states its boundary. A decision
// is reached before any byte leaves and that is the whole of what it can
// promise: nothing readmit does afterwards can retract bytes already sent.
func writeDecisionLines(w io.Writer, decision sendpolicy.Decision) {
	verdict := "denied"
	if decision.Allowed {
		verdict = "allowed"
	}
	fmt.Fprintf(w, "Send policy: %s (%s)\n", verdict, decision.Reason)
	fmt.Fprintf(w, "Destination: %s resolved to %s\n", decision.Address, list(decision.ResolvedAddresses))
	if decision.PolicySelected {
		fmt.Fprintf(w, "Approved destinations: %s\n", list(decision.ApprovedDestinations))
	} else {
		fmt.Fprintln(w, "Approved destinations: no policy was selected, so only a literal loopback address may be sent to")
	}
	if decision.Reason == sendpolicy.SendNotExplicit {
		fmt.Fprintln(w, "Nothing about this destination refuses a send; an explicit --send is what would request one.")
	}
	fmt.Fprintln(w, "A decision is reached before any byte leaves: it can stop a send, and it cannot retract bytes already sent.")
}

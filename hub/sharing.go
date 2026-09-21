package hub

func supportCommand(c ReviewCommand) bool  { return c.Schema == "readmit-hub-review-command/v2" }
func supportApproval(c ReviewCommand) bool { return supportCommand(c) && c.Kind == "support-approval" }
func supportRequest(c ReviewCommand) bool  { return supportCommand(c) && c.Kind == "support-request" }
func validateSupportShape(c ReviewCommand) error {
	if c.Text != "support" {
		return errAccess
	}
	switch c.Kind {
	case "support-policy":
		if c.Release != "" || c.Parent != "" || c.Recipient != "" {
			return errAccess
		}
	case "support-request":
		if !validDigest(c.Release) || c.Parent == "" || c.Recipient == "" {
			return errAccess
		}
	case "support-approval":
		if !validDigest(c.Release) || c.Parent == "" || c.Recipient != "" {
			return errAccess
		}
	default:
		return errAccess
	}
	return nil
}
func validReviewEventVersion(e ReviewEvent, allowV2 bool) bool {
	if supportCommand(e.Command) {
		return allowV2 && e.Schema == "readmit-hub-review-event/v2"
	}
	return e.Command.Schema == "readmit-hub-review-command/v1" && e.Schema == "readmit-hub-review-event/v1"
}

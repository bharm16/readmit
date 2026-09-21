package hub

// Routes is the inventory of the HTTP surface this service serves, declared
// beside the handlers that answer it. The handlers dispatch by hand rather
// than through a table, so this inventory is the one place a reviewer can see
// the whole route space — and it is the check surface for the capability
// ledger: every route here has a ledger row, and every ledger hub row names a
// route here.
//
// Adding a route means adding it here in the same change, so the coverage
// check beside the ledger sees it. A route that is not customer work at all
// still appears here; its ledger row carries a disposition instead of a
// screen, which keeps the exemption visible rather than silent.

// Route is one address the service answers, with the service that serves it
// and what it does, in the service's own words.
type Route struct {
	Service string // "operator", "team" or "runner"
	Method  string
	Path    string
	Purpose string
}

// Routes is the full inventory, grouped by service. Path templates name the
// variable segments in braces; the handlers validate each against its own
// grammar.
func Routes() []Route {
	return []Route{
		// The operator service: opaque immutable artifacts by content digest.
		{Service: "operator", Method: "GET", Path: "/health/live", Purpose: "liveness probe"},
		{Service: "operator", Method: "GET", Path: "/health/ready", Purpose: "readiness probe"},
		{Service: "operator", Method: "GET", Path: "/v1/artifacts/{digest}", Purpose: "read one stored artifact by digest"},
		{Service: "operator", Method: "PUT", Path: "/v1/artifacts/{digest}", Purpose: "store one artifact under its digest"},

		// The team service: project evidence and collaboration under identity
		// and role. v1 and v2 differ in the routes they carry, not the
		// admission rule a route applies.
		{Service: "team", Method: "GET", Path: "/v1/projects/{project}/artifacts/{digest}", Purpose: "download project evidence"},
		{Service: "team", Method: "PUT", Path: "/v1/projects/{project}/artifacts/{digest}", Purpose: "publish project evidence"},
		{Service: "team", Method: "GET", Path: "/v1/projects/{project}/exports/{digest}", Purpose: "download a shared export"},
		{Service: "team", Method: "POST", Path: "/v1/projects/{project}/execution", Purpose: "execute a hub-authorized job"},
		{Service: "team", Method: "POST", Path: "/v1/projects/{project}/approvals", Purpose: "record an approval"},
		{Service: "team", Method: "POST", Path: "/v1/projects/{project}/enrollment", Purpose: "register a project member"},
		{Service: "team", Method: "GET", Path: "/v1/projects/{project}/reviews", Purpose: "read the collaboration review"},
		{Service: "team", Method: "POST", Path: "/v1/projects/{project}/reviews", Purpose: "record a collaboration decision"},
		{Service: "team", Method: "GET", Path: "/v2/projects/{project}/artifacts/{digest}", Purpose: "download project evidence"},
		{Service: "team", Method: "PUT", Path: "/v2/projects/{project}/artifacts/{digest}", Purpose: "publish project evidence"},
		{Service: "team", Method: "GET", Path: "/v2/projects/{project}/exports/{digest}", Purpose: "download a support export"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/execution", Purpose: "execute a hub-authorized job"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/approvals", Purpose: "record an approval"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/enrollment", Purpose: "register a project member"},
		{Service: "team", Method: "GET", Path: "/v2/projects/{project}/reviews", Purpose: "read the collaboration review"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/reviews", Purpose: "record a collaboration decision"},
		{Service: "team", Method: "GET", Path: "/v2/projects/{project}/history", Purpose: "read the review history"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/history", Purpose: "record a history event"},
		{Service: "team", Method: "GET", Path: "/v2/projects/{project}/notifications", Purpose: "read notifications"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/notifications", Purpose: "record a notification"},
		{Service: "team", Method: "GET", Path: "/v2/projects/{project}/lifecycle", Purpose: "read the project lifecycle log"},
		{Service: "team", Method: "POST", Path: "/v2/projects/{project}/lifecycle", Purpose: "record a lifecycle command"},

		// The runner service: short-lived execution leases for enrolled
		// customer runners.
		{Service: "runner", Method: "POST", Path: "/v1/projects/{project}/runner", Purpose: "acquire an execution lease"},
		{Service: "runner", Method: "DELETE", Path: "/v1/projects/{project}/runner", Purpose: "release an execution lease"},
	}
}

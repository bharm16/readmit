// Package docs carries the checked capability ledger into the build, so the
// desktop window derives what it still lacks from the ledger itself rather
// than from a hand-kept copy that every delivery must edit.
package docs

import _ "embed"

// CapabilityLedger is docs/capability-ledger.json as this build was made from.
//
//go:embed capability-ledger.json
var CapabilityLedger []byte

// Package hl7 inspects HL7 v2 syntax as views over immutable source bytes.
// It does not validate message semantics, decode character sets, or edit
// evidence: a rewrite produces new bytes beside the source. The one meaning it
// holds is which positions a date shift moves.
package hl7

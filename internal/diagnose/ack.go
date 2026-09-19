package diagnose

import (
	"fmt"
	"github.com/bharm16/readmit/internal/hl7"
)

// maxACKSegments bounds the MSA and ERR segments one acknowledgement is decoded
// through, here and wherever else one is read.
const maxACKSegments = 128

func (e *evaluator) ack(m message) {
	msaCount, errCount := m.segmentCount("MSA"), m.segmentCount("ERR")
	if msaCount > maxACKSegments || errCount > maxACKSegments {
		e.unsupportedItem("unsupported_ack_cardinality", m.event.ID, "", fmt.Sprintf("The bounded ACK decoder supports at most %d MSA and %d ERR segments per occurrence.", maxACKSegments, maxACKSegments))
		return
	}
	if e.rules[ACKOutcome] {
		for i := 1; i <= msaCount; i++ {
			path := fmt.Sprintf("MSA[%d]-1", i)
			if _, supported := e.value(m, path); !supported {
				continue
			}
			referencePath := fmt.Sprintf("MSA[%d]-2", i)
			if _, supported := e.value(m, referencePath); !supported {
				continue
			}
			code, ok := e.text(m, path)
			outcomes := map[string]string{"AA": "application accept", "AE": "application error", "AR": "application reject", "CA": "commit accept", "CE": "commit error", "CR": "commit reject"}
			outcome, known := outcomes[code]
			if !ok || !known {
				e.unsupportedItem("unsupported_ack_code", m.event.ID, path, "MSA-1 is missing or is not a supported acknowledgement code.")
				continue
			}
			e.finding(ACKOutcome, "observed_fact", fmt.Sprintf("The captured ACK declares MSA-1 %s (%s). This is an acknowledgement outcome, not proof of business-state persistence.", code, outcome), "", m.evidence(path), m.evidence(referencePath))
		}
		if msaCount == 0 {
			e.unsupportedItem("missing_ack_outcome", m.event.ID, "MSA-1", "The ACK contains no MSA segment; its outcome could not be evaluated.")
		}
	}
	if e.rules[ACKError] {
		for i := 1; i <= errCount; i++ {
			codePath, severityPath := fmt.Sprintf("ERR[%d]-3.1", i), fmt.Sprintf("ERR[%d]-4", i)
			if _, supported := e.value(m, severityPath); !supported {
				continue
			}
			systemPath := fmt.Sprintf("ERR[%d]-3.3", i)
			system, supported := e.value(m, systemPath)
			if !supported {
				continue
			}
			if system.State == hl7.Null {
				e.unsupportedItem("unsupported_err_coding_system", m.event.ID, systemPath, "Explicit-null ERR coding system cannot be interpreted as the supported table.")
				continue
			}
			if system.State == hl7.Present {
				name, ok := e.text(m, systemPath)
				if !ok || name != "HL70357" {
					e.unsupportedItem("unsupported_err_coding_system", m.event.ID, systemPath, "Only ERR codes with omitted or HL70357 coding system are interpreted.")
					continue
				}
			}
			code, codeOK := e.text(m, codePath)
			severity, severityOK := e.text(m, severityPath)
			codes := map[string]string{"0": "message accepted", "100": "segment sequence error", "101": "required field missing", "102": "data type error", "103": "table value not found", "200": "unsupported message type", "201": "unsupported event code", "202": "unsupported processing ID", "203": "unsupported version ID", "204": "unknown key identifier", "205": "duplicate key identifier", "206": "application record locked", "207": "application internal error"}
			severities := map[string]string{"I": "information", "W": "warning", "E": "error", "F": "fatal error"}
			decoded, known := codes[code]
			level, levelKnown := severities[severity]
			if !codeOK || !known || !severityOK || !levelKnown {
				e.unsupportedItem("unsupported_err_outcome", m.event.ID, codePath, "ERR code or severity is absent or outside the supported HL7 2.5.1 table subset; free text is not interpreted.")
				continue
			}
			e.finding(ACKError, "observed_fact", fmt.Sprintf("The captured ACK ERR declares code %s (%s), severity %s (%s).", code, decoded, severity, level), "", m.evidence(codePath), m.evidence(severityPath), m.evidence(systemPath))
		}
	}
}

package connectedlab

import "testing"

// The lab peer reads arrivals through the HL7 owner: standard arrivals read
// as the hand-split fields did, and arrivals the splitter could never read —
// custom separators, escaped values — read as HL7 means them.
func TestSelectTextReadsArrivalsThroughTheHL7Owner(t *testing.T) {
	raw := "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||SIU^S15|CTRL-1|P|2.5.1\rSCH|KEY-1||||||||||20260101120000\rPID|1||MRN-1\rOBR|1|PLACER-1|FILLER-1\rOBX|1|NM|2345-7^GLUCOSE||90|mg/dL|||||F\r"
	for path, want := range map[string]string{
		"MSH-10": "CTRL-1", "MSH-9": "SIU^S15", "SCH-1": "KEY-1", "SCH-11": "20260101120000",
		"PID-3": "MRN-1", "OBR-2": "PLACER-1", "OBR-3": "FILLER-1",
		"OBX-5": "90", "OBX-6": "mg/dL", "OBX-11": "F",
	} {
		if got := selectText(raw, path); got != want {
			t.Errorf("selectText(%q) = %q, want %q", path, got, want)
		}
	}
	custom := "MSH$%~\\&$SENDER$LAB$ENGINE$LAB$20260101000000$$SIU%S15$CTRL-9$P$2.5.1\rSCH$KEY-9$$$$$$$$$$20260101120000\r"
	for path, want := range map[string]string{
		"MSH-10": "CTRL-9", "MSH-9": "SIU%S15", "SCH-1": "KEY-9", "SCH-11": "20260101120000",
	} {
		if got := selectText(custom, path); got != want {
			t.Errorf("selectText custom separators %q = %q, want %q", path, got, want)
		}
	}
	escaped := "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||SIU^S15|A\\F\\B|P|2.5.1\r"
	if got := selectText(escaped, "MSH-10"); got != "A|B" {
		t.Errorf("selectText escaped control = %q, want %q", got, "A|B")
	}
	if got := selectText("not a message", "MSH-10"); got != "" {
		t.Errorf("selectText unreadable = %q, want absent", got)
	}
	if got := selectText(raw, "SCH-12"); got != "" {
		t.Errorf("selectText missing position = %q, want absent", got)
	}
}

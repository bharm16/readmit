// Package synth writes the versioned synthetic SIU fixture family. Its messages
// depend only on declared generator inputs, never the clock or machine state.
package synth

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

const (
	GeneratorVersion = "readmit-synth-v1"
	ProfileVersion   = "readmit-siu-v1"
)

type variant struct {
	name        string
	data        []byte
	knownDefect string
}

// Validate before creating output, including the latest appointment end, so
// HL7's four-digit year and whole-second precision never truncate an input.
func generate(inputs bundle.GeneratorInputs) ([]variant, error) {
	if inputs.GeneratorVersion != GeneratorVersion {
		return nil, errors.New("unsupported generator version; supported: readmit-synth-v1")
	}
	if inputs.ProfileVersion != ProfileVersion {
		return nil, errors.New("unsupported profile version; supported: readmit-siu-v1")
	}
	base := inputs.BaseTime.UTC()
	if base.IsZero() || base.Year() < 1 || base.Year() > 9999 || base.Nanosecond() != 0 || base.Add(48*time.Hour+30*time.Minute).Year() > 9999 {
		return nil, errors.New("base time must be a nonzero whole second with the complete scenario within years 0001 through 9999")
	}

	// The PCG stream and draw order belong to readmit-synth-v1. Changing either
	// requires a new implemented generator version, not a different label.
	random := rand.New(rand.NewPCG(inputs.Seed, 0x726561646d697431))
	ids := identifiers{
		patient: fmt.Sprintf("SYNTH-%016X", inputs.Seed),
		placer:  fmt.Sprintf("PLACER-%016X", random.Uint64()),
		filler:  fmt.Sprintf("FILLER-%016X", random.Uint64()),
	}
	booked := base.Add(24 * time.Hour)
	rescheduled := base.Add(48 * time.Hour)
	booking := frame("S12", 1, base, booked, ids, ids.filler)
	reschedule := frame("S13", 2, base.Add(time.Minute), rescheduled, ids, ids.filler)
	cancellation := frame("S15", 3, base.Add(2*time.Minute), rescheduled, ids, ids.filler)
	invalid := frame("S13", 2, base.Add(time.Minute), rescheduled, ids, ids.filler+"-UNBOOKED")

	return []variant{
		{name: "regression", data: append(append([]byte{}, booking...), reschedule...)},
		{name: "cancellation", data: append(append(append([]byte{}, booking...), reschedule...), cancellation...)},
		{name: "invalid", data: append(append([]byte{}, booking...), invalid...), knownDefect: "S13 SCH-2.1 references a filler identifier with no prior booking"},
	}, nil
}

type identifiers struct {
	patient string
	placer  string
	filler  string
}

func frame(trigger string, sequence int, declared, appointment time.Time, ids identifiers, filler string) []byte {
	// These are the positions in the readmit-siu-v1 fixture profile, not a
	// claim to implement every SIU field or validate general HL7 conformance.
	return mllp.Frame(hl7.Encode([][]string{
		{"MSH", "^~\\&", "READMIT", "SYNTHETIC", "RECEIVER", "READMIT", hl7.Time(declared), "", "SIU^" + trigger, fmt.Sprintf("SYNTH-%06d", sequence), "T", "2.5.1"},
		{"SCH", ids.placer + "^READMIT", filler + "^READMIT", "", "", "", "CHECKUP", "ROUTINE", "NORMAL", "30", "min", "^^^" + hl7.Time(appointment) + "^" + hl7.Time(appointment.Add(30*time.Minute))},
		{"PID", "1", "", ids.patient + "^^^READMIT", "", "SYNTHETIC^PATIENT"},
	}))
}

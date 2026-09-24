package customerrunner

import "github.com/bharm16/readmit/internal/durablerun"

// refusal is one named reason the runner refuses a job. It reads as
// ErrRefused, the sentence the command line prints; a caller that names the
// reason in its own words tells them apart with errors.Is. The name keeps
// each one a distinct value and says which it is when printed with %#v.
type refusal struct{ name string }

func (r *refusal) Error() string { return ErrRefused.Error() }
func (r *refusal) Unwrap() error { return ErrRefused }

var (
	// ErrUnprepared: the job's spec could not be prepared.
	ErrUnprepared error = &refusal{"unprepared"}
	// ErrNoIdentity: the prepared inputs have no identity to pin.
	ErrNoIdentity error = &refusal{"no identity"}
	// ErrChanged: the prepared inputs are not the ones the job was pinned to.
	ErrChanged error = &refusal{"changed"}
	// ErrOtherEnvironment: the prepared inputs bind an environment other
	// than the configured one, which Preflight.Environment names.
	ErrOtherEnvironment error = &refusal{"other environment"}
	// ErrUnbound: the prepared inputs bind no environment.
	ErrUnbound error = &refusal{"unbound"}
	// ErrRetained: the runner root already reserves the job's id.
	ErrRetained error = &refusal{"retained"}
)

// Preflight is what one job would run as on this runner, established without
// asking the hub, admitting anything or sending anything: the identity of its
// prepared inputs, the pin an execution binds to, and the environment they
// bind.
type Preflight struct {
	InputIdentity string
	Environment   string
	prepared      *durablerun.Prepared
}

// Inspect prepares the job's spec exactly as execution does and applies the
// runner's rules to it, in execution's order, answering the first that
// refuses: the inputs must have an identity, bind the configured environment,
// and name a job id the root does not reserve. The refusal still carries what
// was established before it. A root this machine cannot read decides nothing:
// the runner's own claim at execution stays the rule, and a configuration's
// root may be on another host.
func Inspect(c Config, job Job) (Preflight, error) {
	if ValidateJob(job) != nil {
		return Preflight{}, ErrRefused
	}
	p, err := prepare(job, nil)
	if err != nil {
		return p, err
	}
	return p, p.bind(c, job.ID)
}

// prepare prepares the job's spec and seals its inputs' identity, refusing
// inputs that are not the ones pin names when pin is not nil.
func prepare(job Job, pin *string) (Preflight, error) {
	prepared, err := durablerun.Prepare(job.Spec)
	if err != nil {
		return Preflight{}, ErrUnprepared
	}
	identity, err := prepared.InputIdentity()
	if err != nil {
		return Preflight{}, ErrNoIdentity
	}
	p := Preflight{InputIdentity: identity, prepared: prepared}
	if pin != nil && identity != *pin {
		return p, ErrChanged
	}
	for _, r := range prepared.Resources() {
		if r.Kind == durablerun.EnvironmentResource {
			p.Environment = r.Name
		}
	}
	return p, nil
}

// bind applies the rules of the runner c configures to prepared inputs: they
// must bind its environment, and id must be one its root does not reserve.
// Inputs that bind no environment are refused last, after the retained id,
// the order in which the window's preflight has always named them.
func (p Preflight) bind(c Config, id string) error {
	if p.Environment != "" && p.Environment != c.Environment {
		return ErrOtherEnvironment
	}
	if retained, err := Retained(c.Root, id); err == nil && retained {
		return ErrRetained
	}
	if p.Environment == "" {
		return ErrUnbound
	}
	return nil
}

package collection

import (
	"encoding/json/v2"
	"errors"
	"net/netip"
	"slices"
)

const FaultSchema = "readmit-collection/v3"
const MaxFaultSteps = 64
const MaxFaultDelayMS = 30000

// FaultPolicy approves exact test endpoints, never DNS names, wildcard binds,
// or executable hooks. Classification is the operator's declaration, not a
// verified property of the remote system. Production is never permitted.
type FaultPolicy struct {
	EnvironmentClass      string      `json:"environment_class"`
	ApprovedTestEndpoints []string    `json:"approved_test_endpoints"`
	Steps                 []FaultStep `json:"steps"`
}

type FaultStep struct {
	Message int    `json:"message"`
	Stage   string `json:"stage"`
	Action  string `json:"action"`
	DelayMS int    `json:"delay_ms"`
}

// FaultEvent records execution separately from the acknowledgement result.
// A malformed reply is wire evidence, never a successfully acknowledged stage.
type FaultEvent struct {
	Action string `json:"action"`
	Stage  string `json:"stage"`
	Status string `json:"status"`
}

func (f *FaultPolicy) UnmarshalJSON(data []byte) error {
	var required struct {
		EnvironmentClass      *string      `json:"environment_class"`
		ApprovedTestEndpoints *[]string    `json:"approved_test_endpoints"`
		Steps                 *[]FaultStep `json:"steps"`
	}
	if err := json.Unmarshal(data, &required, json.RejectUnknownMembers(true)); err != nil || required.EnvironmentClass == nil || required.ApprovedTestEndpoints == nil || required.Steps == nil {
		return errors.New("fault policy requires a nonproduction classification, approved endpoints, and steps")
	}
	*f = FaultPolicy{*required.EnvironmentClass, *required.ApprovedTestEndpoints, *required.Steps}
	return nil
}

func (f *FaultStep) UnmarshalJSON(data []byte) error {
	var required struct {
		Message *int    `json:"message"`
		Stage   *string `json:"stage"`
		Action  *string `json:"action"`
		DelayMS *int    `json:"delay_ms"`
	}
	if err := json.Unmarshal(data, &required, json.RejectUnknownMembers(true)); err != nil || required.Message == nil || required.Stage == nil || required.Action == nil || required.DelayMS == nil {
		return errors.New("fault step requires message, stage, action, and delay")
	}
	*f = FaultStep{*required.Message, *required.Stage, *required.Action, *required.DelayMS}
	return nil
}

func (f *FaultEvent) UnmarshalJSON(data []byte) error {
	var required struct {
		Action *string `json:"action"`
		Stage  *string `json:"stage"`
		Status *string `json:"status"`
	}
	if err := json.Unmarshal(data, &required, json.RejectUnknownMembers(true)); err != nil || required.Action == nil || required.Stage == nil || required.Status == nil {
		return errors.New("fault event requires action, stage, and status")
	}
	*f = FaultEvent{*required.Action, *required.Stage, *required.Status}
	return nil
}

// FaultAction is one controlled fault a step can declare, and whether it
// waits before it acts: one that waits declares a delay of one to thirty
// thousand milliseconds, and every other declares zero.
type FaultAction struct {
	Action string `json:"action"`
	Waits  bool   `json:"waits"`
}

// faultActions is every fault action a step can declare, in the order a
// person is offered them.
var faultActions = []FaultAction{
	{"delay", true},
	{"reject", false},
	{"malformed-ack", false},
	{"missing-response", true},
	{"disconnect", false},
}

// FaultActions is every fault action a step can declare.
func FaultActions() []FaultAction { return slices.Clone(faultActions) }

// FaultWaits reports whether a fault action waits before it acts.
func FaultWaits(action string) bool {
	return slices.Contains(faultActions, FaultAction{action, true})
}

func literalEndpoint(address string) (netip.AddrPort, error) {
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil {
		return netip.AddrPort{}, errors.New("fault endpoints must be literal unicast IP addresses and nonzero ports")
	}
	addressIP := endpoint.Addr().Unmap()
	if endpoint.Port() == 0 || addressIP.IsUnspecified() || addressIP.IsMulticast() || addressIP.Zone() != "" {
		return netip.AddrPort{}, errors.New("fault endpoints must be literal unicast IP addresses and nonzero ports")
	}
	return netip.AddrPortFrom(addressIP, endpoint.Port()), nil
}

func (f FaultPolicy) Validate() error {
	if f.EnvironmentClass != "nonproduction" {
		return errors.New("fault injection requires an explicitly declared nonproduction environment; production is refused")
	}
	if len(f.ApprovedTestEndpoints) < 1 || len(f.ApprovedTestEndpoints) > 16 {
		return errors.New("fault policy requires one to sixteen approved test endpoints")
	}
	seen := map[netip.AddrPort]bool{}
	for _, address := range f.ApprovedTestEndpoints {
		endpoint, err := literalEndpoint(address)
		if err != nil {
			return err
		}
		if seen[endpoint] {
			return errors.New("fault endpoints must be distinct")
		}
		seen[endpoint] = true
	}
	if len(f.Steps) < 1 || len(f.Steps) > MaxFaultSteps {
		return errors.New("fault policy requires one to sixty four steps")
	}
	previous := 0
	for _, step := range f.Steps {
		if step.Message <= previous || step.Message > 4000 {
			return errors.New("fault messages must be distinct ascending ordinals from one through four thousand")
		}
		previous = step.Message
		if step.Stage != AcceptStage && step.Stage != ApplicationStage {
			return errors.New("fault stage must be accept or application")
		}
		i := slices.IndexFunc(faultActions, func(known FaultAction) bool { return known.Action == step.Action })
		switch {
		case i < 0:
			return errors.New("unsupported fault action")
		case faultActions[i].Waits:
			if step.DelayMS < 1 || step.DelayMS > MaxFaultDelayMS {
				return errors.New("fault wait must be between one and thirty thousand milliseconds")
			}
		case step.DelayMS != 0:
			return errors.New("this fault action declares zero delay")
		}
	}
	return nil
}

// ApproveEndpoint is checked before binding and again against the listener's
// actual address. Separate application delivery is checked before dialing.
func (f FaultPolicy) ApproveEndpoint(address string) error {
	if err := f.Validate(); err != nil {
		return err
	}
	endpoint, err := literalEndpoint(address)
	if err != nil {
		return err
	}
	for _, declared := range f.ApprovedTestEndpoints {
		approved, _ := literalEndpoint(declared)
		if endpoint == approved {
			return nil
		}
	}
	return errors.New("fault endpoint is not an explicitly approved test endpoint")
}

func (f *FaultPolicy) Step(message int) *FaultStep {
	if f != nil {
		for _, step := range f.Steps {
			if step.Message == message {
				return &step
			}
		}
	}
	return nil
}

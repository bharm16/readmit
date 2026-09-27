package observeinterval

import (
	"context"
	"time"
)

// Observe samples continuously while stimulus is in flight and until the full
// post-stimulus horizon or independently observed barrier. It never examines an
// assertion or stops because a value/count looks successful. Acquisition runs
// through the caller's existing authorized source adapter.
func (s *Session) Observe(ctx context.Context, acquire func(context.Context) (Observation, error), stimulusDone <-chan struct{}) error {
	finished := false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !finished {
			select {
			case <-stimulusDone:
				if err := s.StimulusFinished(); err != nil {
					return err
				}
				finished = true
			default:
			}
		}
		s.mu.Lock()
		sampleCount := 0
		for _, record := range s.result.Records {
			if record.Kind == "baseline" || record.Kind == "sample" {
				sampleCount++
			}
		}
		limit := s.result.Definition.MaxSamples
		binding := s.result.Binding
		s.mu.Unlock()
		if sampleCount >= limit {
			if err := s.Append(ctx, Observation{Binding: binding, Status: "safety-limit"}); err != nil {
				return err
			}
			return invalid
		}
		sample, err := acquire(ctx)
		if err != nil {
			sample.Status = "collector-failed"
			sample.Binding = s.result.Binding
		}
		if appendErr := s.Append(ctx, sample); appendErr != nil {
			return appendErr
		}
		if err != nil || sample.Status != "healthy" {
			return invalid
		}
		s.mu.Lock()
		state := decide(s.result)
		sampleMS := s.result.Definition.SampleMS
		s.mu.Unlock()
		if state.Sufficient() {
			return nil
		}
		switch state.Reason {
		case "not-ready", "horizon-incomplete", "barrier-not-observed", "no-coverage":
			// These are the only nonterminal states that another sample can resolve.
		default:
			return invalid
		}
		if err := s.clock.Wait(ctx, time.Duration(sampleMS)*time.Millisecond); err != nil {
			return err
		}
	}
}

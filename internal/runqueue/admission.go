package runqueue

import "context"

type admissionKey struct{}

// WithAdmission installs an invocation adapter's check before each new job.
// The queue owns no commercial policy: pure engine callers retain their explicit
// execution authority. Production adapters supply this check while holding one
// bounded runner-instance lease for the whole invocation, not one slot per test.
func WithAdmission(ctx context.Context, check func() error) context.Context {
	return context.WithValue(ctx, admissionKey{}, check)
}
func admitted(ctx context.Context) error {
	if check, ok := ctx.Value(admissionKey{}).(func() error); ok {
		return check()
	}
	return nil
}

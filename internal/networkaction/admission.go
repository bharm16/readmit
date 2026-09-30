package networkaction

import "context"

type admissionKey struct{}
type admissionCheck func(context.Context) error
type authorityKey struct{}

// WithAuthority selects an independently approved runner authority for this
// execution only. It admits the current compiler-derived exact bindings under
// durable customer approval; imported plans and retained results cannot select
// it. FileAuthority keeps its ordinary on-disk grant semantics without this
// explicit context, and WithAdmission still runs before the selected authority.
func WithAuthority(ctx context.Context, authority Authority) context.Context {
	if authority == nil {
		return ctx
	}
	return context.WithValue(ctx, authorityKey{}, authority)
}

// WithAdmission adds a runtime runner admission check to the separately
// provisioned exact-action grant. The check is never retained or reconstructed
// from evidence; each effect must still pass both the admission and the grant.
// Nested admissions retain every enclosing lease/fence check.
func WithAdmission(ctx context.Context, check func(context.Context) error) context.Context {
	if check == nil {
		return ctx
	}
	prior, _ := ctx.Value(admissionKey{}).(admissionCheck)
	return context.WithValue(ctx, admissionKey{}, admissionCheck(func(current context.Context) error {
		if prior != nil {
			if err := prior(current); err != nil {
				return err
			}
		}
		return check(current)
	}))
}

func checkAdmission(ctx context.Context) error {
	if check, ok := ctx.Value(admissionKey{}).(admissionCheck); ok {
		return check(ctx)
	}
	return nil
}

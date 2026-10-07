package primitives

import "context"

type overviewWorkSummaryKey struct{}

// WithOverviewWorkSummary selects narrower canonical card columns for Overview.
// It changes projection format only; canonical visibility and derived sections
// still use the same scoped candidates and contributor checks.
func WithOverviewWorkSummary(ctx context.Context) context.Context {
	return context.WithValue(ctx, overviewWorkSummaryKey{}, true)
}

func overviewWorkSummary(ctx context.Context) bool {
	value, _ := ctx.Value(overviewWorkSummaryKey{}).(bool)
	return value
}

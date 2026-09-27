package commandcenter

import "context"

type Attribution struct {
	RunID   string `json:"run_id"`
	HostID  string `json:"host_id"`
	AgentID string `json:"agent_id"`
	Adapter string `json:"adapter"`
}
type attributionKey struct{}

func WithAttribution(ctx context.Context, a Attribution) context.Context {
	return context.WithValue(ctx, attributionKey{}, a)
}
func AttributionFrom(ctx context.Context) (Attribution, bool) {
	a, ok := ctx.Value(attributionKey{}).(Attribution)
	return a, ok
}

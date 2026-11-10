package pm

import "fmt"

// ActorMismatchError is disclosed only after agent authentication/authorization.
type ActorMismatchError struct {
	ConfiguredActorID string `json:"configured_actor_id"`
	ActingActorID     string `json:"acting_actor_id"`
}

func (e *ActorMismatchError) Error() string {
	return fmt.Sprintf("PM actor mismatch: ANX_PM_AGENT_ACTOR_ID is %q; acting actor is %q", e.ConfiguredActorID, e.ActingActorID)
}
func (e *ActorMismatchError) Unwrap() error { return ErrForbidden }
func (e *ActorMismatchError) Hint() string {
	return fmt.Sprintf("Ask the workspace operator to rebind ANX_PM_AGENT_ACTOR_ID to the acting actor %q and restart anx-core.", e.ActingActorID)
}

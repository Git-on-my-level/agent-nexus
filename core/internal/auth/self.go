package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"errors"
)

// SelfPrincipal is the common self-identification shape for every bearer.
// The agent envelope name is retained by the HTTP route for existing clients.
type SelfPrincipal struct {
	ID            string `json:"id"`
	AgentID       string `json:"agent_id"`
	ActorID       string `json:"actor_id"`
	Ref           string `json:"ref"`
	Username      string `json:"username"`
	Handle        string `json:"handle"`
	DisplayName   string `json:"display_name"`
	PrincipalKind string `json:"principal_kind"`
	AuthMethod    string `json:"auth_method"`
	IdentityKind  string `json:"identity_kind,omitempty"`
	HostID        string `json:"host_id,omitempty"`
	HostSlug      string `json:"host_slug,omitempty"`
	Name          string `json:"name,omitempty"`
	Adapter       string `json:"adapter,omitempty"`
	Persona       *bool  `json:"persona,omitempty"`
}

func (s *Store) GetSelfPrincipal(ctx context.Context, agentID string) (SelfPrincipal, error) {
	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return SelfPrincipal{}, err
	}
	if agent.Revoked {
		return SelfPrincipal{}, ErrAgentRevoked
	}
	var display string
	if err := resourceaccess.NewDB(s.db).QueryRowContext(ctx, `SELECT display_name FROM actors WHERE id=?`, agent.ActorID).Scan(&display); err != nil {
		return SelfPrincipal{}, err
	}
	kind, method := "", ""
	if agent.PrincipalKind != nil {
		kind = *agent.PrincipalKind
	}
	if agent.AuthMethod != nil {
		method = *agent.AuthMethod
	}
	out := SelfPrincipal{ID: agent.AgentID, AgentID: agent.AgentID, ActorID: agent.ActorID,
		Ref: "actor:" + agent.ActorID, Username: agent.Username, Handle: agent.Username,
		DisplayName: display, PrincipalKind: kind, AuthMethod: method}
	if kind != string(PrincipalKindAgent) {
		return out, nil
	}
	derived, err := s.GetDerivedAgent(ctx, agentID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrHostNotFound) {
		out.IdentityKind = "standalone"
		out.Name = agent.Username
		return out, nil
	}
	if err != nil {
		return SelfPrincipal{}, err
	}
	out.Handle = derived.Handle
	out.DisplayName = derived.DisplayName
	out.IdentityKind = derived.IdentityKind
	out.Name = derived.Name
	if derived.HostID != nil {
		out.HostID = *derived.HostID
	}
	if derived.HostSlug != nil {
		out.HostSlug = *derived.HostSlug
	}
	out.Adapter = defaultHostAdapter(derived.Name)
	persona := out.Adapter == "generic" && derived.Name != "generic"
	out.Persona = &persona
	return out, nil
}

func defaultHostAdapter(name string) string {
	switch name {
	case "claude", "codex", "cursor", "omp", "generic":
		return name
	default:
		return "generic"
	}
}

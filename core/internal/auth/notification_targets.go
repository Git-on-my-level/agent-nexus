package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"encoding/json"
)

// NotificationTargets returns only requested active agent identities, without
// last-seen/session enrichment or a directory read per inbox row.
func (s *Store) NotificationTargets(ctx context.Context, actorIDs, agentIDs []string) (map[string]AuthPrincipalSummary, error) {
	out := map[string]AuthPrincipalSummary{}
	actors, _ := json.Marshal(actorIDs)
	agents, _ := json.Marshal(agentIDs)
	rows, err := resourceaccess.NewDB(s.db).QueryContext(ctx, `SELECT 'actor:'||j.value,a.id,a.actor_id,a.username FROM json_each(?) j JOIN agents a ON a.id=(SELECT p.id FROM agents p WHERE p.actor_id=j.value AND p.revoked_at IS NULL AND `+principalKindExpr("p")+`='agent' ORDER BY p.created_at DESC,p.id DESC LIMIT 1)
 UNION ALL SELECT 'agent:'||a.id,a.id,a.actor_id,a.username FROM agents a WHERE a.id IN (SELECT value FROM json_each(?)) AND a.revoked_at IS NULL AND `+principalKindExpr("a")+`='agent'`, string(actors), string(agents))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var p AuthPrincipalSummary
		if err = rows.Scan(&key, &p.AgentID, &p.ActorID, &p.Username); err != nil {
			return nil, err
		}
		p.PrincipalKind = "agent"
		out[key] = p
	}
	return out, rows.Err()
}

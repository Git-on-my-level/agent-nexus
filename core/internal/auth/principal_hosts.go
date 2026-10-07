package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Read only the host fields used by the selected principal page. Authority is
// still reloaded by the PM lookup on every check; this batches presentation data.
func (s *Store) enrichPrincipalHosts(ctx context.Context, principals []AuthPrincipalSummary) error {
	ids := make([]string, 0, len(principals))
	positions := map[string]int{}
	for i, p := range principals {
		ids = append(ids, p.AgentID)
		positions[p.AgentID] = i
	}
	raw, _ := json.Marshal(ids)
	rows, err := resourceaccess.NewDB(s.db).QueryContext(ctx, `SELECT ha.agent_id,ha.host_id,h.id,h.slug,h.revoked_at,h.bridge_expires_at,
 COALESCE((SELECT json_group_array(name) FROM host_exclusions WHERE host_id=ha.host_id),'[]')
 FROM json_each(?) selected JOIN host_agents ha ON ha.agent_id=selected.value
 LEFT JOIN hosts h ON h.id=ha.host_id`, string(raw))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID, hostID, names string
		var visibleID, slug, revoked, expiry sql.NullString
		if err := rows.Scan(&agentID, &hostID, &visibleID, &slug, &revoked, &expiry, &names); err != nil {
			return err
		}
		if !visibleID.Valid {
			return ErrHostNotFound
		}
		p := &principals[positions[agentID]]
		p.HostID = hostID
		p.HostBridgeOnline = expiry.Valid && !expired(expiry.String) && !revoked.Valid
		var excluded []string
		if err := json.Unmarshal([]byte(names), &excluded); err != nil {
			return err
		}
		for _, name := range excluded {
			if strings.EqualFold(p.Username, name+"."+slug.String) {
				p.HostExcluded = true
			}
		}
	}
	return rows.Err()
}

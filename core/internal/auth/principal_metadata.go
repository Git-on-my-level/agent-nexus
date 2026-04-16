package auth

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	AuthMethodPublicKey = "public_key"
	AuthMethodPasskey   = "passkey"
)

func principalKindExpr(agentAlias string) string {
	return fmt.Sprintf(`COALESCE(
		NULLIF(json_extract(%s.metadata_json, '$.principal_kind'), ''),
		CASE
			WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id = %s.id LIMIT 1) THEN 'human'
			ELSE 'agent'
		END
	)`, agentAlias, agentAlias)
}

func authMethodExpr(agentAlias string) string {
	return fmt.Sprintf(`COALESCE(
		NULLIF(json_extract(%s.metadata_json, '$.auth_method'), ''),
		CASE
			WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id = %s.id LIMIT 1) THEN 'passkey'
			ELSE 'public_key'
		END
	)`, agentAlias, agentAlias)
}

func principalLastSeenExpr(agentAlias string) string {
	return fmt.Sprintf(`COALESCE(
		(SELECT MAX(t.created_at) FROM auth_access_tokens t WHERE t.agent_id = %s.id),
		%s.created_at
	)`, agentAlias, agentAlias)
}

func principalMetadataJSON(kind PrincipalKind, authMethod string, extra map[string]any) (string, error) {
	payload := map[string]any{
		"principal_kind": string(kind),
		"auth_method":    strings.TrimSpace(authMethod),
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func actorMetadataJSON(kind PrincipalKind, authMethod string, extra map[string]any) (string, error) {
	payload := map[string]any{
		"principal_kind": string(kind),
		"auth_method":    strings.TrimSpace(authMethod),
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

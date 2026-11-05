package resourceaccess

// HumanPrincipalSQL reports whether the agents row named by alias is a human
// principal, with the legacy passkey fallback auth uses for rows that predate
// principal_kind metadata. It reads durable authority only, never profiles.
func HumanPrincipalSQL(alias string) string {
	return `COALESCE(NULLIF(json_extract(` + alias + `.metadata_json,'$.principal_kind'),''),CASE WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id=` + alias + `.id LIMIT 1) THEN 'human' ELSE 'agent' END)='human'`
}

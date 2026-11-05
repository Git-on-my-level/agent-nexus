package resourceaccess

// HumanPrincipalSQL reports whether the agents row named by alias is a human
// principal, with the legacy passkey fallback auth uses for rows that predate
// principal_kind metadata. It reads durable authority only, never profiles.
func HumanPrincipalSQL(alias string) string {
	return `COALESCE(NULLIF(json_extract(` + alias + `.metadata_json,'$.principal_kind'),''),CASE WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id=` + alias + `.id LIMIT 1) THEN 'human' ELSE 'agent' END)='human'`
}

// SoleHumanSQL reports whether exactly one human principal was active at the
// timestamp expression at (RFC 3339 text), or now when at is empty. The count
// stops at two rows, so it stays bounded however many principals exist.
func SoleHumanSQL(at string) string {
	active := `h.revoked_at IS NULL`
	if at != "" {
		active = `julianday(h.created_at)<=julianday(` + at + `) AND (h.revoked_at IS NULL OR julianday(h.revoked_at)>julianday(` + at + `))`
	}
	return `(SELECT count(*) FROM (SELECT 1 FROM agents h WHERE ` + active + ` AND ` + HumanPrincipalSQL("h") + ` LIMIT 2))=1`
}

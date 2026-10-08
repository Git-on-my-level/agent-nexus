package workprojection

// JSON null is a present override in projectWork, distinct from a missing key.
func StringSQL(key, canonical string) string {
	value := func(body, path string) string {
		return `CASE WHEN json_type(` + body + `,'` + path + `')='text' THEN anx_unicode_trim(json_extract(` + body + `,'` + path + `')) ELSE '' END`
	}
	return `CASE WHEN anx_unicode_trim(COALESCE(m.authority,'nexus'))='nexus' THEN ` + canonical + ` WHEN json_type(o.body_json,'$.facts.` + key + `') IS NOT NULL THEN ` + value("o.body_json", "$.facts."+key) + ` WHEN json_type(m.metadata_json,'$.` + key + `') IS NOT NULL THEN ` + value("m.metadata_json", "$."+key) + ` ELSE ` + canonical + ` END`
}

// ClosedSQL evaluates effective work closure using indexed c/m/o joins.
func ClosedSQL() string {
	return "(" + StringSQL("phase", "c.column_key") + " IN ('done','cancelled') OR COALESCE(c.archived_at,'')<>'' OR COALESCE(c.trashed_at,'')<>'')"
}

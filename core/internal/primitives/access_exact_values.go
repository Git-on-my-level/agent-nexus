package primitives

import "agent-nexus-core/internal/resourceaccess"

// Exact submitted atoms resolve through point indexes before joining denials.
// Do not expand every historical spelling of every denied resource merely to
// validate a handful of submitted IDs. Prose keeps the complete matcher.
func exactValueCheckSQL() string {
	valid := "d.kind NOT LIKE 'filter/%' AND d.kind NOT IN ('work_evidence_record','work_evidence_alias')"
	kind := "CASE lower(substr(j.value,1,instr(j.value,':')-1)) WHEN 'doc' THEN 'document' ELSE lower(substr(j.value,1,instr(j.value,':')-1)) END"
	ref := "substr(j.value,instr(j.value,':')+1)"
	return `SELECT EXISTS (
 SELECT 1 FROM json_each(?) j CROSS JOIN _anx_denied d
 WHERE ` + valid + ` AND d.kind<>'plan' AND j.value=d.id COLLATE NOCASE
 UNION ALL
 SELECT 1 FROM json_each(?) j CROSS JOIN _anx_denied d
 WHERE ` + valid + ` AND d.kind<>'external_key' AND instr(j.value,':')>0
 AND d.kind=` + kind + ` COLLATE NOCASE AND d.id=` + ref + ` COLLATE NOCASE
 AND NOT (d.kind='card' AND (d.id LIKE 'http://%' OR d.id LIKE 'https://%'))
 UNION ALL
 SELECT 1 FROM json_each(?) j CROSS JOIN main.resource_access_identities i
 JOIN _anx_denied d ON d.kind=i.kind AND d.id=i.resource_id
 WHERE ` + valid + ` AND d.kind<>'external_key' AND instr(j.value,':')>0
 AND i.kind=` + kind + ` AND ` + resourceaccess.AtomKeySQL("i.ref") + `=` + resourceaccess.AtomKeySQL(ref) + `
 AND NOT (i.kind='card' AND (i.ref LIKE 'http://%' OR i.ref LIKE 'https://%'))
 UNION ALL
 SELECT 1 FROM json_each(?) j CROSS JOIN _anx_denied d CROSS JOIN main.resource_access_identities i
 WHERE ` + valid + ` AND d.kind<>lower(d.kind) AND d.kind<>'external_key'
 AND instr(j.value,':')>0 AND d.kind=` + kind + ` COLLATE NOCASE
 AND i.kind=d.kind AND i.resource_id=d.id
 AND ` + resourceaccess.AtomKeySQL("i.ref") + `=` + resourceaccess.AtomKeySQL(ref) + `
 AND NOT (i.kind='card' AND (i.ref LIKE 'http://%' OR i.ref LIKE 'https://%'))
 UNION ALL
 SELECT 1 FROM json_each(?) j CROSS JOIN main.resource_access_identities i
 JOIN _anx_denied d ON d.kind=i.kind AND d.id=i.resource_id
 WHERE i.kind IN ('card','external_key')
 AND (i.kind='external_key' OR i.ref LIKE 'http://%' OR i.ref LIKE 'https://%')
 AND ` + resourceaccess.AtomKeySQL("i.ref") + `=` + resourceaccess.AtomKeySQL("j.value") + `
 UNION ALL
 SELECT 1 FROM json_each(?) j CROSS JOIN main.work_metadata m
 JOIN _anx_denied d ON d.kind='card' AND d.id=m.card_id
 WHERE m.authority<>'nexus' AND j.value=CASE
 WHEN json_extract(m.metadata_json,'$.source.url') LIKE 'http://%' OR json_extract(m.metadata_json,'$.source.url') LIKE 'https://%'
 THEN json_extract(m.metadata_json,'$.source.url') ELSE 'card:'||json_extract(m.metadata_json,'$.source.url') END COLLATE NOCASE
 )`
}

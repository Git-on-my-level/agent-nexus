package resourceaccess

// ReferenceSQL mirrors the accepted typed-reference spelling: surrounding
// whitespace is insignificant on either side of the colon, as is kind casing.
// HTTP source URLs are opaque and must not be parsed as typed references.
// Callers supply only trusted SQL expressions, never interpolated user values.
func ReferenceSQL(value string) string {
	prefix := "lower(" + TrimSpaceSQL("substr("+value+",1,instr("+value+",':')-1)") + ")"
	return "CASE WHEN " + prefix + " IN ('thread','board','card','topic','document','doc','event','artifact','card_revision','document_revision','wakeup','plan','inbox','run') THEN (CASE " + prefix + " WHEN 'doc' THEN 'document' ELSE " + prefix + " END)||':'||" + TrimSpaceSQL("substr("+value+",instr("+value+",':')+1)") + " ELSE " + TrimSpaceSQL(value) + " END"
}

// SQLite's one-argument trim only removes ASCII space. Go's reference parser
// uses strings.TrimSpace, which removes the Unicode White_Space set.
func TrimSpaceSQL(value string) string {
	return "trim(" + value + ",char(9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288))"
}

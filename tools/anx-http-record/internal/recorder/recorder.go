package recorder

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const SchemaVersion = 1

type Entry struct {
	SchemaVersion         int                 `json:"schema_version"`
	Seq                   uint64              `json:"seq"`
	TS                    string              `json:"ts"`
	Method                string              `json:"method"`
	Path                  string              `json:"path"`
	Query                 string              `json:"query,omitempty"`
	RequestHeaders        map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders       map[string][]string `json:"response_headers,omitempty"`
	RequestBody           string              `json:"request_body,omitempty"`
	RequestBodyEncoding   string              `json:"request_body_encoding,omitempty"`
	ResponseBody          string              `json:"response_body,omitempty"`
	ResponseBodyEncoding  string              `json:"response_body_encoding,omitempty"`
	StatusCode            int                 `json:"status_code,omitempty"`
	TruncatedRequestBody  bool                `json:"truncated_request_body,omitempty"`
	TruncatedResponseBody bool                `json:"truncated_response_body,omitempty"`
	RequestBodyOmitted    bool                `json:"request_body_omitted,omitempty"`
	ResponseBodyOmitted   bool                `json:"response_body_omitted,omitempty"`
	RequestBodyRedacted   bool                `json:"request_body_redacted,omitempty"`
	ResponseBodyRedacted  bool                `json:"response_body_redacted,omitempty"`
	ClientLabel           string              `json:"client_label,omitempty"`
	DurationMS            int64               `json:"duration_ms,omitempty"`
	Error                 string              `json:"error,omitempty"`
}

type JSONLWriter struct {
	mu      sync.Mutex
	nextSeq uint64
	w       io.Writer
}

func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return &JSONLWriter{w: w}
}

// Write assigns seq while holding the serializer lock so file order is the replay order.
func (w *JSONLWriter) Write(entry Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextSeq++
	entry.SchemaVersion = SchemaVersion
	entry.Seq = w.nextSeq
	entry.TS = time.Now().UTC().Format(time.RFC3339Nano)

	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = w.w.Write(line)
	return err
}

type LimitedBuffer struct {
	limit     int64
	buf       bytes.Buffer
	truncated bool
}

func NewLimitedBuffer(limit int64) *LimitedBuffer {
	return &LimitedBuffer{limit: limit}
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		if len(p) > 0 {
			b.truncated = true
		}
		return len(p), nil
	}
	if int64(b.buf.Len()) >= b.limit {
		if len(p) > 0 {
			b.truncated = true
		}
		return len(p), nil
	}

	remaining := int(b.limit - int64(b.buf.Len()))
	if remaining <= 0 {
		b.truncated = b.truncated || len(p) > 0
		return len(p), nil
	}
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *LimitedBuffer) Bytes() []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b.buf.Bytes()...)
}

func (b *LimitedBuffer) Truncated() bool {
	if b == nil {
		return false
	}
	return b.truncated
}

type CaptureReadCloser struct {
	rc         io.ReadCloser
	sink       io.Writer
	finalize   func()
	finalizeMu sync.Once
}

func NewCaptureReadCloser(rc io.ReadCloser, sink io.Writer, finalize func()) *CaptureReadCloser {
	return &CaptureReadCloser{
		rc:       rc,
		sink:     sink,
		finalize: finalize,
	}
}

func (c *CaptureReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 && c.sink != nil {
		_, _ = c.sink.Write(p[:n])
	}
	if err == io.EOF {
		c.finish()
	}
	return n, err
}

func (c *CaptureReadCloser) Close() error {
	err := c.rc.Close()
	c.finish()
	return err
}

func (c *CaptureReadCloser) finish() {
	c.finalizeMu.Do(func() {
		if c.finalize != nil {
			c.finalize()
		}
	})
}

type BodyValue struct {
	Value    string
	Encoding string
	Omitted  bool
	Redacted bool
}

func MaterializeBody(headers http.Header, data []byte, truncated bool) BodyValue {
	if len(data) == 0 {
		return BodyValue{}
	}

	if isJSONContent(headers, data) {
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			if truncated && utf8.Valid(data) {
				fragment, redacted := redactJSONFragment(string(data))
				return BodyValue{
					Value:    fragment,
					Encoding: "json_fragment",
					Redacted: redacted,
				}
			}
			return BodyValue{Omitted: true, Redacted: true}
		}
		redacted := redactJSON(decoded)
		encoded, err := json.Marshal(redacted)
		if err != nil {
			return BodyValue{Omitted: true, Redacted: true}
		}
		return BodyValue{
			Value:    string(encoded),
			Encoding: "json",
			Redacted: bodyRedacted(decoded, redacted),
		}
	}

	if isText(headers, data) {
		return BodyValue{
			Value:    string(data),
			Encoding: "utf8",
		}
	}

	return BodyValue{
		Value:    base64.StdEncoding.EncodeToString(data),
		Encoding: "base64",
	}
}

var sensitiveJSONFragmentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)("token"\s*:\s*")([^"]*)("?)`),
	regexp.MustCompile(`(?i)("access_token"\s*:\s*")([^"]*)("?)`),
	regexp.MustCompile(`(?i)("refresh_token"\s*:\s*")([^"]*)("?)`),
	regexp.MustCompile(`(?i)("bootstrap_token"\s*:\s*")([^"]*)("?)`),
	regexp.MustCompile(`(?i)("invite_token"\s*:\s*")([^"]*)("?)`),
	regexp.MustCompile(`(?i)("password"\s*:\s*")([^"]*)("?)`),
}

func redactJSONFragment(raw string) (string, bool) {
	redacted := raw
	changed := false
	for _, pattern := range sensitiveJSONFragmentPatterns {
		next := pattern.ReplaceAllString(redacted, `${1}REDACTED${3}`)
		if next != redacted {
			changed = true
			redacted = next
		}
	}
	return redacted, changed
}

func RedactHeaders(header http.Header) map[string][]string {
	if len(header) == 0 {
		return nil
	}

	out := make(map[string][]string, len(header))
	for key, values := range header {
		canonicalKey := http.CanonicalHeaderKey(key)
		if isSensitiveHeader(canonicalKey) {
			out[canonicalKey] = []string{"REDACTED"}
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[canonicalKey] = copied
	}
	return out
}

func isSensitiveHeader(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "cookie", "set-cookie", "proxy-authorization", "x-api-key", "x-oar-bootstrap-token":
		return true
	default:
		return false
	}
}

func isJSONContent(headers http.Header, data []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(headers.Get("Content-Type"))
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		return true
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

func isText(headers http.Header, data []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(headers.Get("Content-Type"))
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	if mediaType == "application/x-ndjson" || strings.HasSuffix(mediaType, "+json") || mediaType == "application/json" {
		return true
	}
	if !utf8.Valid(data) {
		return false
	}
	for _, r := range string(data) {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 {
			return false
		}
	}
	return true
}

func bodyRedacted(before any, after any) bool {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return false
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return false
	}
	return !bytes.Equal(beforeJSON, afterJSON)
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveJSONKey(key) {
				out[key] = "REDACTED"
				continue
			}
			out[key] = redactJSON(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactJSON(child)
		}
		return out
	default:
		return value
	}
}

func isSensitiveJSONKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "token", "access_token", "refresh_token", "bootstrap_token", "invite_token", "password":
		return true
	default:
		return false
	}
}

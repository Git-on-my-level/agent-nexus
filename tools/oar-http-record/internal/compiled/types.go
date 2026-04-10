package compiled

type Run struct {
	SchemaVersion   int        `json:"schema_version"`
	GeneratedAt     string     `json:"generated_at"`
	SourceRecording string     `json:"source_recording,omitempty"`
	Exchanges       []Exchange `json:"exchanges"`
}

type Exchange struct {
	Seq                uint64              `json:"seq"`
	Method             string              `json:"method"`
	Path               string              `json:"path"`
	Query              string              `json:"query,omitempty"`
	RequestHeaders     map[string][]string `json:"request_headers,omitempty"`
	RequestBodyKind    string              `json:"request_body_kind,omitempty"`
	RequestBody        any                 `json:"request_body,omitempty"`
	ExpectedStatusCode int                 `json:"expected_status_code"`
	Captures           []Capture           `json:"captures,omitempty"`
}

type Capture struct {
	Alias           string `json:"alias"`
	ResponsePointer string `json:"response_pointer"`
}

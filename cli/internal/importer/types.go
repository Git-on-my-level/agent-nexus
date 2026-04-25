package importer

// InventoryRecord is one scanned source file in a normalized inventory.
type InventoryRecord struct {
	SourceID             string   `json:"source_id"`
	RelPath              string   `json:"relpath,omitempty"`
	AbsPath              string   `json:"abspath,omitempty"`
	Filename             string   `json:"filename,omitempty"`
	Extension            string   `json:"extension,omitempty"`
	SizeBytes            int64    `json:"size_bytes,omitempty"`
	MTime                string   `json:"mtime,omitempty"`
	SHA256               string   `json:"sha256,omitempty"`
	MIMEGuess            string   `json:"mime_guess,omitempty"`
	IsText               bool     `json:"is_text,omitempty"`
	Encoding             string   `json:"encoding,omitempty"`
	Category             string   `json:"category,omitempty"`
	RepoRoot             string   `json:"repo_root,omitempty"`
	ClusterHint          string   `json:"cluster_hint,omitempty"`
	TitleHint            string   `json:"title_hint,omitempty"`
	Preview              string   `json:"preview,omitempty"`
	TextCachePath        string   `json:"text_cache_path,omitempty"`
	NormalizedTextSHA256 string   `json:"normalized_text_sha256,omitempty"`
	LineCount            int      `json:"line_count,omitempty"`
	WordCount            int      `json:"word_count,omitempty"`
	SourceHints          []string `json:"source_hints,omitempty"`
	Error                string   `json:"error,omitempty"`
}

// ScanSummary captures scan output locations and aggregate counts.
type ScanSummary struct {
	CreatedAt           string         `json:"created_at"`
	Input               string         `json:"input"`
	ScanRoot            string         `json:"scan_root"`
	ExtractedRoot       string         `json:"extracted_root,omitempty"`
	FileCount           int            `json:"file_count"`
	RepoRoots           []string       `json:"repo_roots,omitempty"`
	CountsByCategory    map[string]int `json:"counts_by_category,omitempty"`
	CountsByClusterHint map[string]int `json:"counts_by_cluster_hint,omitempty"`
	Inventory           string         `json:"inventory"`
}

// ExactDuplicateGroup is one exact-duplicate cluster.
type ExactDuplicateGroup struct {
	Hash    string   `json:"hash"`
	Keep    string   `json:"keep"`
	Members []string `json:"members"`
	Drop    []string `json:"drop"`
	Reason  string   `json:"reason"`
}

// ProbableDuplicateGroup is one review-needed near-duplicate cluster.
type ProbableDuplicateGroup struct {
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	ClusterHint string   `json:"cluster_hint"`
	Members     []string `json:"members"`
	Reason      string   `json:"reason"`
	Action      string   `json:"action"`
}

// DedupeReport captures exact/probable duplicate analysis.
type DedupeReport struct {
	CreatedAt          string                   `json:"created_at"`
	Inventory          string                   `json:"inventory"`
	ExactDuplicates    []ExactDuplicateGroup    `json:"exact_duplicates,omitempty"`
	ProbableDuplicates []ProbableDuplicateGroup `json:"probable_duplicates,omitempty"`
	RecommendedSkipIDs []string                 `json:"recommended_skip_ids,omitempty"`
}

// SkippedItem records a source item intentionally skipped during planning.
type SkippedItem struct {
	SourceID string `json:"source_id"`
	RelPath  string `json:"relpath,omitempty"`
	Reason   string `json:"reason"`
}

// ReviewBundle records a source item deferred for operator or agent review.
type ReviewBundle struct {
	SourceID    string  `json:"source_id"`
	RelPath     string  `json:"relpath,omitempty"`
	Category    string  `json:"category,omitempty"`
	Reason      string  `json:"reason"`
	Confidence  float64 `json:"confidence"`
	ClusterHint string  `json:"cluster_hint,omitempty"`
}

// PlanObject is one proposed Agent Nexus write with refs that may include $REF:key placeholders.
type PlanObject struct {
	Key                 string         `json:"key"`
	Kind                string         `json:"kind"`
	SourceIDs           []string       `json:"source_ids,omitempty"`
	Confidence          float64        `json:"confidence,omitempty"`
	Reason              string         `json:"reason,omitempty"`
	Create              map[string]any `json:"create"`
	PendingBinaryUpload bool           `json:"pending_binary_upload,omitempty"`
}

// ImportPlan is the conservative proposed import graph.
type ImportPlan struct {
	CreatedAt     string         `json:"created_at"`
	SourceName    string         `json:"source_name"`
	Inventory     string         `json:"inventory"`
	Dedupe        string         `json:"dedupe"`
	Principles    map[string]any `json:"principles,omitempty"`
	Objects       []PlanObject   `json:"objects,omitempty"`
	Skipped       []SkippedItem  `json:"skipped,omitempty"`
	ReviewBundles []ReviewBundle `json:"review_bundles,omitempty"`
	Notes         []string       `json:"notes,omitempty"`
}

// ApplyResult is one preview or execution row from applying a plan.
type ApplyResult struct {
	Key      string         `json:"key"`
	Kind     string         `json:"kind"`
	Payload  string         `json:"payload"`
	Status   string         `json:"status"`
	Reason   string         `json:"reason,omitempty"`
	Note     string         `json:"note,omitempty"`
	Response map[string]any `json:"response,omitempty"`
}

// ApplyReport captures payload preview generation and optional execution results.
type ApplyReport struct {
	CreatedAt string            `json:"created_at"`
	Plan      string            `json:"plan"`
	Execute   bool              `json:"execute"`
	Results   []ApplyResult     `json:"results,omitempty"`
	Refs      map[string]string `json:"refs,omitempty"`
}

// ScanOptions configures Scan.
type ScanOptions struct {
	InputPath         string
	OutDir            string
	MaxPreviewBytes   int
	MaxTextCacheBytes int64
}

// DedupeOptions configures Dedupe.
type DedupeOptions struct {
	InventoryPath string
	OutDir        string
}

// PlanOptions configures Plan.
type PlanOptions struct {
	InventoryPath      string
	DedupePath         string
	OutDir             string
	SourceName         string
	CollectorThreshold int
}

// ApplyOptions configures Apply.
type ApplyOptions struct {
	PlanPath string
	OutDir   string
	Execute  bool
}

// CreateFunc creates one object from an already-resolved payload and returns the response body.
type CreateFunc func(kind string, payload map[string]any) (map[string]any, error)

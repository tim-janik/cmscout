package metrics

type MatchEvidence struct {
	Status           string   `json:"status"`
	Method           string   `json:"method"`
	Similarity       float64  `json:"similarity"`
	BeforeCandidates []string `json:"before_candidates,omitempty"`
	AfterCandidates  []string `json:"after_candidates,omitempty"`
}

type MetricDelta struct {
	Status             string `json:"status"`
	Cyclomatic         *int   `json:"cyclomatic"`
	Lines              *int   `json:"lines"`
	Chars              *int   `json:"chars"`
	PrefixChars        *int   `json:"prefix_chars"`
	PrefixLines        *int   `json:"prefix_lines"`
	InlineChars        *int   `json:"inline_chars"`
	InlineCommentCount *int   `json:"inline_comment_count"`
}

type Change struct {
	BeforeName        *string       `json:"before_name"`
	AfterName         *string       `json:"after_name"`
	BeforeKind        *string       `json:"before_kind"`
	AfterKind         *string       `json:"after_kind"`
	Match             MatchEvidence `json:"match"`
	Added             bool          `json:"added"`
	Removed           bool          `json:"removed"`
	Renamed           bool          `json:"renamed"`
	Moved             bool          `json:"moved"`
	KindChanged       bool          `json:"kind_changed"`
	NameChanged       bool          `json:"qualified_name_changed"`
	DirectChanged     bool          `json:"direct_changed"`
	DescendantChanged bool          `json:"descendant_changed"`
	CodeChanged       bool          `json:"code_changed"`
	SignatureChanged  bool          `json:"signature_changed"`
	PrefixChanged     bool          `json:"prefix_changed"`
	InlineChanged     bool          `json:"inline_changed"`
	FormattingChanged bool          `json:"formatting_changed"`
	BeforeRanges      []Span        `json:"before_ranges"`
	AfterRanges       []Span        `json:"after_ranges"`
	Delta             MetricDelta   `json:"delta"`
}

type RenderedDiff struct {
	Format string `json:"format"`
	Text   string `json:"text"`
}

type Comparison struct {
	SchemaVersion  string        `json:"schema_version"`
	Status         string        `json:"status"`
	RangePrecision string        `json:"range_precision"`
	Before         *Snapshot     `json:"before"`
	After          *Snapshot     `json:"after"`
	Changes        []Change      `json:"changes"`
	BeforeRanges   []Span        `json:"before_ranges"`
	AfterRanges    []Span        `json:"after_ranges"`
	Diagnostics    []Diagnostic  `json:"diagnostics"`
	Diff           *RenderedDiff `json:"diff,omitempty"`
}

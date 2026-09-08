package metrics

type Span struct {
	StartByte uint `json:"start_byte"`
	EndByte   uint `json:"end_byte"`
	StartLine uint `json:"start_line"`
	StartCol  uint `json:"start_col"`
	EndLine   uint `json:"end_line"`
	EndCol    uint `json:"end_col"`
}

type Size struct {
	Bytes uint `json:"bytes"`
	Chars *int `json:"chars"`
	Lines int  `json:"lines"`
}

type Comment struct {
	Size
	Span      *Span  `json:"span"`
	OwnerName string `json:"owner_name,omitempty"`
	Role      string `json:"role,omitempty"`
	Ownership string `json:"ownership,omitempty"`
}

type Decision struct {
	Rule         string `json:"rule"`
	Span         Span   `json:"span"`
	Contribution int    `json:"contribution"`
}

type Cyclomatic struct {
	Value     *int       `json:"value"`
	Status    string     `json:"status"`
	Decisions []Decision `json:"decisions,omitempty"`
}

type FunctionMetrics struct {
	Cyclomatic     Cyclomatic `json:"cyclomatic"`
	CommentStatus  string     `json:"comment_status"`
	PrefixComment  *Comment   `json:"prefix_comment"`
	InlineComments []Comment  `json:"inline_comments"`
}

type NamePart struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Ordinal   int    `json:"ordinal,omitempty"`
}

type Component struct {
	QualifiedName  string     `json:"qualified_name"`
	ParentName     string     `json:"parent_name"`
	Name           string     `json:"name"`
	NameParts      []NamePart `json:"name_parts"`
	NameOrigin     string     `json:"name_origin"`
	Kind           string     `json:"kind"`
	Span           *Span      `json:"span"`
	Body           *Span      `json:"body,omitempty"`
	Declaration    *Span      `json:"declaration"`
	Size           *Size      `json:"size"`
	InnerFunctions []string   `json:"inner_functions"`
	*FunctionMetrics
}

type Diagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Span    *Span  `json:"span"`
}

type Snapshot struct {
	SchemaVersion string       `json:"schema_version"`
	NamingVersion string       `json:"naming_version"`
	Profiles      []string     `json:"profiles"`
	Limitations   []string     `json:"limitations"`
	SnapshotID    string       `json:"snapshot_id"`
	ContentDigest string       `json:"content_digest"`
	Namespace     string       `json:"namespace"`
	Path          string       `json:"path"`
	Language      string       `json:"language"`
	Population    string       `json:"population"`
	Coordinates   string       `json:"coordinates"`
	Status        string       `json:"status"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	Comments      []Comment    `json:"comments"`
	Components    []Component  `json:"components"`
	source        []byte
	tokens        []source_token
}

type Options struct {
	Namespace string
	Path      string
	Explain   bool
}

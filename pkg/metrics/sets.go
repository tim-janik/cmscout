package metrics

import "cmscout/pkg/source"

type ScanReport struct {
	SchemaVersion string          `json:"schema_version"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	Root          string          `json:"root"`
	Population    string          `json:"population"`
	Files         []*Snapshot     `json:"files"`
	Skipped       []source.Notice `json:"skipped"`
	Diagnostics   []source.Notice `json:"diagnostics"`
}

type FileComparison struct {
	Change     string      `json:"change"`
	Comparison *Comparison `json:"comparison"`
}

type ChangeSet struct {
	SchemaVersion string           `json:"schema_version"`
	Kind          string           `json:"kind"`
	Status        string           `json:"status"`
	Root          string           `json:"root"`
	Population    string           `json:"population"`
	Input         source.Selection `json:"input"`
	Files         []FileComparison `json:"files"`
	Skipped       []source.Notice  `json:"skipped"`
	Diagnostics   []source.Notice  `json:"diagnostics"`
}

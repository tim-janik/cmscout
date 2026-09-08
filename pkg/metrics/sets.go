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

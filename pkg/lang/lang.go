// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package lang provides source language detection.
package lang

import (
	"path/filepath"
	"strings"
)

// Language represents a source language and its file extension.
type Language struct {
	Name string // "ts", "tsx", "js", "jsx"
	Ext  string // file extension, e.g. ".ts"
}

// languages maps file extensions to Language values.
var languages = []Language{
	{Name: "ts", Ext: ".ts"},
	{Name: "tsx", Ext: ".tsx"},
	{Name: "js", Ext: ".js"},
	{Name: "js", Ext: ".mjs"},
	{Name: "js", Ext: ".cjs"},
	{Name: "jsx", Ext: ".jsx"},
	{Name: "go", Ext: ".go"},
	{Name: "bash", Ext: ".sh"},
	{Name: "bash", Ext: ".bash"},
}

// Detect returns the language for the given file path based on extension.
// Returns (Language, true) if recognized, or (Language{}, false) otherwise.
func Detect(path string) (Language, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, l := range languages {
		if ext == l.Ext {
			return l, true
		}
	}
	return Language{}, false
}

// String returns the language name.
func (l Language) String() string {
	return l.Name
}

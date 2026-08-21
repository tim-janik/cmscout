// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package lang provides source language detection.
package lang

import (
	"path/filepath"
	"strings"
)

// Language represents a source language and its file extension.
type Language struct {
	Name string // "ts", "tsx", "js", "jsx", "go", "bash", "c", "cpp"
	Ext  string // file extension, e.g. ".ts"
}

// languages maps case-insensitive file extensions to Language values.
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
	{Name: "c", Ext: ".c"},
	{Name: "cpp", Ext: ".cc"},
	{Name: "cpp", Ext: ".cpp"},
	{Name: "cpp", Ext: ".cxx"},
	{Name: "cpp", Ext: ".c++"},
	{Name: "cpp", Ext: ".hh"},
	{Name: "cpp", Ext: ".hpp"},
	{Name: "cpp", Ext: ".hxx"},
	{Name: "cpp", Ext: ".h++"},
	{Name: "cpp", Ext: ".tcc"},
	{Name: "cpp", Ext: ".h"},
}

// Detect returns the language for the given file path based on extension.
// Returns (Language, true) if recognized, or (Language{}, false) otherwise.
func Detect(path string) (Language, bool) {
	// Preserve the legacy `.C` -> cpp convention before lowercasing.
	if raw := filepath.Ext(path); raw == ".C" {
		return Language{Name: "cpp", Ext: ".C"}, true
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, l := range languages {
		if ext == l.Ext {
			return l, true
		}
	}
	return Language{}, false
}

// HasMacroFunctions reports whether a language supports function-like macros.
func HasMacroFunctions(l Language) bool {
	return l.Name == "c" || l.Name == "cpp"
}

// String returns the language name.
func (l Language) String() string {
	return l.Name
}

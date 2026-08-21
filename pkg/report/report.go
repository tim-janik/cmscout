// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package report implements Phase 8: renders a CorrelationResult as a human-readable review report.
package report

// Options configures report output.
type Options struct {
	NoColor       bool // disable ANSI colors
	SummaryOnly   bool // show only the summary
	SkipUnchanged bool // suppress entirely unchanged blocks in the detailed listing

	// OldLanguage/NewLanguage select the whitespace-classification rules for each side
	// (blank = language-neutral scanner; C++ raw strings and Go backticks are atomic).
	OldLanguage string
	NewLanguage string
}

// color holds ANSI escape sequences for terminal output.
type color struct {
	reset   string
	cyan    string
	green   string
	red     string
	yellow  string
	magenta string
	gray    string
	bold    string
}

func newColor(noColor bool) color {
	if noColor {
		return color{} // all empty strings
	}
	return color{
		reset:   "\033[0m",
		cyan:    "\033[36m",
		green:   "\033[32m",
		red:     "\033[31m",
		yellow:  "\033[33m",
		magenta: "\033[35m",
		gray:    "\033[90m",
		bold:    "\033[1m",
	}
}

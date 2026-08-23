// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"flag"
	"fmt"

	"cmdiff/pkg/diff"
)

// compareFlags holds display names, -B/-A content redirection, and output options.
type compareFlags struct {
	oldName               string // display name (also content source if -B not given)
	newName               string // display name (also content source if -A not given)
	beforeFile            string // -B / --before-contents: read old content from this path
	afterFile             string // -A / --after-contents: read new content from this path
	noColor               bool
	wordDiff              bool
	wordDiffSpanThreshold float64
	ignoreSpace           bool
}

// register binds the shared flags onto a command FlagSet.
func (cf *compareFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&cf.oldName, "old", "", "old file display name (default: first argument)")
	fs.StringVar(&cf.newName, "new", "", "new file display name (default: second argument)")
	fs.StringVar(&cf.beforeFile, "B", "", "read old content from this file (default: same as old name)")
	fs.StringVar(&cf.beforeFile, "before-contents", "", "read old content from this file (default: same as old name)")
	fs.StringVar(&cf.afterFile, "A", "", "read new content from this file (default: same as new name)")
	fs.StringVar(&cf.afterFile, "after-contents", "", "read new content from this file (default: same as new name)")
	fs.BoolVar(&cf.noColor, "no-color", false, "disable ANSI colors")
	fs.BoolVar(&cf.wordDiff, "word-diff", false, "highlight intra-line word changes")
	fs.Float64Var(&cf.wordDiffSpanThreshold, "word-diff-span-threshold", diff.DefaultWordDiffSpanThreshold,
		"collapse word diff to one span when more than this fraction of a line's words changed (negative: never collapse)")
	fs.BoolVar(&cf.ignoreSpace, "ignore-all-space", false, "ignore whitespace when comparing lines")
}

// resolve maps positional display names and -B/-A content redirection to content paths.
func (cf *compareFlags) resolve(args []string) (oldContentPath, newContentPath string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("expected at most two positional names, got %d", len(args))
	}
	var positionalOld, positionalNew string
	if len(args) >= 1 {
		positionalOld = args[0]
		if cf.oldName == "" {
			cf.oldName = positionalOld
		}
	}
	if len(args) >= 2 {
		positionalNew = args[1]
		if cf.newName == "" {
			cf.newName = positionalNew
		}
	}

	if cf.oldName == "" || cf.newName == "" {
		return "", "", fmt.Errorf("usage: cmdiff [flags] <old_file> <new_file>")
	}

	oldContentPath = cf.beforeFile
	if oldContentPath == "" {
		oldContentPath = positionalOld
		if oldContentPath == "" {
			oldContentPath = cf.oldName
		}
	}
	newContentPath = cf.afterFile
	if newContentPath == "" {
		newContentPath = positionalNew
		if newContentPath == "" {
			newContentPath = cf.newName
		}
	}
	return oldContentPath, newContentPath, nil
}

// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Command cmdiff is a Code Motion Diff: it matches code blocks across languages
// (functions, classes, JSX, string templates) and diffs per block.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"cmdiff/pkg/correlate"
	"cmdiff/pkg/diff"
	"cmdiff/pkg/extract"
	"cmdiff/pkg/ir"
	"cmdiff/pkg/lang"
	"cmdiff/pkg/parser"
	"cmdiff/pkg/report"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// No input-size limit: the m×n table and LCS diff are correctness-first by design.

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var (
		cf            compareFlags
		summary       bool
		skipUnchanged bool
		simpleDiff    bool
		addedStyle    string
		removedStyle  string
	)

	fs := flag.NewFlagSet("cmdiff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cf.register(fs)
	fs.BoolVar(&summary, "summary", false, "show only summary statistics")
	fs.BoolVar(&skipUnchanged, "skip-unchanged", false, "suppress entirely unchanged components (blocks identical on both sides)")
	fs.BoolVar(&simpleDiff, "simple-diff", false, "skip semantic analysis: emit a plain whole-file line diff")
	fs.StringVar(&addedStyle, "added-style", "white", "how to color added blocks: 'white' (only '+' green, body white, readable) or 'green' (entire line green)")
	fs.StringVar(&removedStyle, "removed-style", "white", "how to color removed blocks: 'white' (only '-' red, body white) or 'red' (entire line red)")
	fs.Usage = func() { printUsage(stdout) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	oldContentPath, newContentPath, err := cf.resolve(fs.Args())
	if err != nil {
		return err
	}

	// Load content
	oldSrc, newSrc, err := loadPair(oldContentPath, newContentPath, stdin)
	if err != nil {
		return fmt.Errorf("loading inputs: %w", err)
	}

	if simpleDiff {
		return runSimpleDiff(cf, summary, skipUnchanged, addedStyle, removedStyle, oldSrc, newSrc, stdout)
	}
	return runSemanticReview(cf, summary, skipUnchanged, addedStyle, removedStyle, oldSrc, newSrc, stdout)
}

// runSemanticReview runs the full pipeline (detect → parse → extract → match → correlate → diff → report).
func runSemanticReview(cf compareFlags, summary, skipUnchanged bool, addedStyle, removedStyle string, oldSrc, newSrc string, stdout io.Writer) error {
	// Build pipeline with full context (the coverage supplement needs complete line representation).
	d := diff.NewWithOpts(diff.Options{
		WordDiff:              cf.wordDiff,
		WordDiffSpanThreshold: cf.wordDiffSpanThreshold,
		IgnoreSpace:           cf.ignoreSpace,
		FullContext:           true,
	})

	// Detect language support first: an unsupported side falls back without being parsed.
	oldLang, oldOK := lang.Detect(cf.oldName)
	newLang, newOK := lang.Detect(cf.newName)

	// Separate macro kinds only when both inputs are C/C++.
	separateMacros := oldOK && newOK &&
		lang.HasMacroFunctions(oldLang) && lang.HasMacroFunctions(newLang)

	var result *ir.CorrelationResult
	usedFallback := false
	if !oldOK || !newOK {
		// Unsupported-language fallback: one whole-file diff under "Other" (never a deceptive all-zero summary).
		usedFallback = true
		result = synthesizeFallbackDiff(oldSrc, newSrc, cf.oldName, cf.newName, d)
	} else {
		oldDoc, err := parseAndExtract(oldSrc, cf.oldName, separateMacros)
		if err != nil {
			return fmt.Errorf("parsing old file: %w", err)
		}
		newDoc, err := parseAndExtract(newSrc, cf.newName, separateMacros)
		if err != nil {
			return fmt.Errorf("parsing new file: %w", err)
		}

		if len(oldDoc.Blocks) == 0 && len(newDoc.Blocks) == 0 {
			// No semantic blocks on either side: fall back to a whole-file diff.
			usedFallback = true
			result = synthesizeFallbackDiff(oldSrc, newSrc, cf.oldName, cf.newName, d)
			result.OldParseErrors = oldDoc.ParseErrors
			result.NewParseErrors = newDoc.ParseErrors
		} else {
			// Staged hierarchical matching: exact identity → distance tables (see pkg/matching).
			result = correlate.Correlate(oldDoc, newDoc)

			// Collapse matched children into canonical reference comments. This and
			// AttachPrefixComments are the only stages mutating pair sources.
			correlate.CollapseMatchedSubBlocks(result)

			// Attach prefix doc-comments (e.g. `/// Do foo` before `void foo()`) to the following component
			// as one diff unit; must run before Diff so the InnerDiff includes the prefix.
			correlate.AttachPrefixComments(result)

			// Diff matched pairs last: the report's displayed similarity derives from this final text.
			for i := range result.Pairs {
				p := &result.Pairs[i]
				if p.Old != nil && p.New != nil {
					p.InnerDiff = d.Diff(p.Old.Source, p.New.Source)
				}
			}
		}
	}

	// Coverage supplement: append a whole-file diff so every non-empty input line appears in the report.
	// The unknown-language fallback already is a whole-file diff and needs no supplement.
	if !usedFallback {
		result.Pairs = append(result.Pairs, ir.CorrelatedPair{
			Old:          &ir.SemanticBlock{Source: oldSrc, Kind: ir.KindUnknown, Name: cf.oldName},
			New:          &ir.SemanticBlock{Source: newSrc, Kind: ir.KindUnknown, Name: cf.newName},
			InnerDiff:    d.DiffFull(oldSrc, newSrc),
			Confidence:   1.0,
			MatchType:    ir.MatchSimilarity,
			Supplemental: true,
		})
	}

	// Report: print the git-style header; the reporter draws no header of its own.
	return writeReport(stdout, report.Options{
		NoColor:       cf.noColor,
		SummaryOnly:   summary,
		SkipUnchanged: skipUnchanged,
		AddedStyle:    addedStyle,
		RemovedStyle:  removedStyle,
	}, result, cf.oldName, cf.newName)
}

// runSimpleDiff emits a plain whole-file line diff, skipping the semantic pipeline entirely.
func runSimpleDiff(cf compareFlags, summary, skipUnchanged bool, addedStyle, removedStyle string, oldSrc, newSrc string, stdout io.Writer) error {
	d := diff.NewWithOpts(diff.Options{
		WordDiff:              cf.wordDiff,
		WordDiffSpanThreshold: cf.wordDiffSpanThreshold,
		IgnoreSpace:           cf.ignoreSpace,
	})
	inner := d.DiffFull(oldSrc, newSrc)
	return writeReport(stdout, report.Options{
		NoColor:       cf.noColor,
		SummaryOnly:   summary,
		SkipUnchanged: skipUnchanged,
		AddedStyle:    addedStyle,
		RemovedStyle:  removedStyle,
	}, &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{{
			Old:        &ir.SemanticBlock{Source: oldSrc, Kind: ir.KindUnknown, Name: cf.oldName},
			New:        &ir.SemanticBlock{Source: newSrc, Kind: ir.KindUnknown, Name: cf.newName},
			InnerDiff:  inner,
			Confidence: 1.0,
			MatchType:  ir.MatchSimilarity,
		}},
	}, cf.oldName, cf.newName)
}

// synthesizeFallbackDiff builds a single whole-file diff for unsupported languages / no blocks (C2).
func synthesizeFallbackDiff(oldSrc, newSrc, oldName, newName string, d *diff.Differ) *ir.CorrelationResult {
	whole := d.DiffFull(oldSrc, newSrc)
	return &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{{
			Old:        &ir.SemanticBlock{Source: oldSrc, Kind: ir.KindUnknown, Name: oldName},
			New:        &ir.SemanticBlock{Source: newSrc, Kind: ir.KindUnknown, Name: newName},
			InnerDiff:  whole,
			Confidence: 1.0,
			MatchType:  ir.MatchSimilarity,
		}},
	}
}

// writeReport prints the git-style header and renders via the text reporter.
func writeReport(stdout io.Writer, opts report.Options, result *ir.CorrelationResult, oldName, newName string) error {
	// Pass source languages to the reporter so each lexer handles its literals correctly.
	if oldLang, ok := lang.Detect(oldName); ok {
		opts.OldLanguage = oldLang.Name
	}
	if newLang, ok := lang.Detect(newName); ok {
		opts.NewLanguage = newLang.Name
	}
	r := &report.TextReporter{Opts: opts}
	if _, err := fmt.Fprintf(stdout, "diff --cmdiff %s %s\n", oldName, newName); err != nil {
		return err
	}
	return r.Write(stdout, result, "", "")
}

func parseAndExtract(src, path string, separateMacros bool) (*ir.SemanticDocument, error) {
	// Reject undetected languages: parsing an unsupported file would fabricate parse errors.
	langCode, ok := lang.Detect(path)
	if !ok {
		return nil, fmt.Errorf("unsupported language for %s", path)
	}

	// Choose C or C++ for .h files by parse recovery quality.
	if langCode.Ext == ".h" {
		cppDoc, cppErr := parseAndExtractLanguage(src, path, langCode, separateMacros)
		cDoc, cErr := parseAndExtractLanguage(src, path, lang.Language{Name: "c", Ext: ".c"}, separateMacros)
		switch {
		case cppErr == nil && cErr == nil:
			if cDoc.ParseErrors < cppDoc.ParseErrors {
				return cDoc, nil
			}
			return cppDoc, nil // tie and C++ errors both prefer the documented default
		case cppErr == nil:
			return cppDoc, nil
		case cErr == nil:
			return cDoc, nil
		default:
			return nil, fmt.Errorf("C++ parse: %v; C parse: %v", cppErr, cErr)
		}
	}

	return parseAndExtractLanguage(src, path, langCode, separateMacros)
}

func parseAndExtractLanguage(src, path string, langCode lang.Language, separateMacros bool) (*ir.SemanticDocument, error) {
	// Create parser
	p, err := parser.New(langCode)
	if err != nil {
		return nil, err
	}
	defer p.Close()

	// Parse
	ast, err := p.Parse(context.Background(), []byte(src))
	if err != nil {
		return nil, err
	}
	defer ast.Close()

	// Extract
	ex := extract.NewWithOptions(path, extract.Options{SeparateMacroFunctions: separateMacros})
	doc, err := ex.Extract(ast)
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func loadFile(path string, stdin io.Reader) (string, error) {
	if path == "-" || path == "" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func loadPair(oldPath, newPath string, stdin io.Reader) (string, string, error) {
	if oldPath == "-" && newPath == "-" {
		return "", "", fmt.Errorf("stdin can only provide one side of a comparison")
	}
	oldSrc, err := loadFile(oldPath, stdin)
	if err != nil {
		return "", "", err
	}
	newSrc, err := loadFile(newPath, stdin)
	if err != nil {
		return "", "", err
	}
	return oldSrc, newSrc, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `cmdiff — code motion diff

Usage:
  cmdiff [flags] <old_file> <new_file>
  cmdiff --simple-diff [flags] <old_file> <new_file>

  The default mode parses both files and reports the change per semantic
  block (functions, classes, methods, constants, imports, JSX elements,
  ...). With --simple-diff the semantic pipeline is skipped entirely and
  the output is a plain whole-file line diff.

  <old_file>/<new_file> are file paths; "-" reads one side from stdin
  (only one side may be stdin).

Flags:
  --simple-diff           Skip semantic analysis: plain whole-file line diff
  --no-color              Disable ANSI colors
  --summary               Show only summary statistics
  --skip-unchanged        Suppress entirely unchanged components
  --word-diff             Highlight intra-line word changes
  --word-diff-span-threshold <frac>  Collapse word diff to one span when more than
                            this fraction of a line's words changed (default: 0.4;
                            negative: never collapse)
  --ignore-all-space      Ignore whitespace when comparing lines
  --added-style white|green  How to color added blocks: 'white' (only '+' green, body white, default, readable) or 'green' (entire line green)
  --removed-style white|red  How to color removed blocks: 'white' (only '-' red, body white, default) or 'red' (entire line red)
  -B, --before-contents <path>  Read old content from this file
  -A, --after-contents <path>   Read new content from this file
  --old <name>            Old display name (default: first argument)
  --new <name>            New display name (default: second argument)

  --skip-unchanged removes components whose name and source are identical
  on both sides from the detailed listing (they are still counted in the
  summary). Note: this intentionally trades away the coverage guarantee
  for those blocks — their lines are no longer echoed in the report.

  When -B/-A are given, the positional <old_file>/<new_file> are mere
  display names (used for language detection and report headers); the
  actual file contents are read from the -B/-A paths.

Git external diff usage:
  GIT_EXTERNAL_DIFF=/path/to/git-diff-wrapper.sh git diff -- <file>
`)
}

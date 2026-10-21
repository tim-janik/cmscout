// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Command cmscout is a Code Motion Scout: it matches code blocks across languages
// (functions, classes, JSX, string templates) and diffs per block.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"cmscout/pkg/correlate"
	"cmscout/pkg/diff"
	"cmscout/pkg/extract"
	"cmscout/pkg/ir"
	"cmscout/pkg/lang"
	"cmscout/pkg/parser"
	"cmscout/pkg/report"
)

var version = "unversioned"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exit_code(err))
	}
}

// No input-size limit: the m×n table and LCS diff are correctness-first by design.

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var (
		cf                compareFlags
		mf                metrics_flags
		stats_mode        bool
		max_comment_lines int
		summary           bool
		skipUnchanged     bool
		simpleDiff        bool
		addedStyle        string
		removedStyle      string
		show_version      bool
	)

	fs := flag.NewFlagSet("cmscout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cf.register(fs)
	mf.register(fs)
	fs.BoolVar(&stats_mode, "stats", false, "report single-file block statistics")
	fs.IntVar(&max_comment_lines, "max-comment-lines", 5, "flag long comments in --stats (0 disables)")
	fs.BoolVar(&show_version, "version", false, "show version")
	fs.BoolVar(&summary, "summary", false, "show only summary statistics")
	fs.BoolVar(&skipUnchanged, "skip-unchanged", false, "suppress entirely unchanged components (blocks identical on both sides)")
	fs.BoolVar(&simpleDiff, "simple-diff", false, "skip semantic analysis: emit a plain whole-file line diff")
	fs.StringVar(&addedStyle, "added-style", "white", "how to color added blocks: 'white' (only '+' green, body white, readable) or 'green' (entire line green)")
	fs.StringVar(&removedStyle, "removed-style", "white", "how to color removed blocks: 'white' (only '-' red, body white) or 'red' (entire line red)")
	fs.Usage = func() {}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printUsage(stdout)
			return nil
		}
		if mf.enabled || stats_mode {
			return exit2_error{err}
		}
		printUsage(stderr)
		return err
	}
	if stats_mode {
		if err := run_stats_flags(fs, cf, mf.scan_flags, max_comment_lines, stdin, stdout); err != nil {
			return exit2_error{err}
		}
		return nil
	}
	if mf.enabled {
		if err := mf.run(fs, cf, stdin, stdout); err != nil {
			return exit2_error{err}
		}
		return nil
	}
	stats_only := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "max-comment-lines" {
			stats_only = true
		}
	})
	if stats_only {
		return fmt.Errorf("--max-comment-lines requires --stats")
	}
	var metrics_only string
	fs.Visit(func(option *flag.Flag) {
		if option.Name != "metrics" && mf.flags.Lookup(option.Name) != nil {
			metrics_only = option.Name
		}
	})
	if metrics_only != "" {
		return fmt.Errorf("--%s requires --metrics", metrics_only)
	}

	if show_version {
		_, err := fmt.Fprintf(stdout, "cmscout %s\n", version)
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

	opts := report.Options{
		NoColor:       cf.noColor,
		SummaryOnly:   summary,
		SkipUnchanged: skipUnchanged,
		IgnoreSpace:   cf.ignoreSpace,
		AddedStyle:    addedStyle,
		RemovedStyle:  removedStyle,
	}
	if simpleDiff {
		return runSimpleDiff(cf, opts, oldSrc, newSrc, stdout)
	}
	return runSemanticReview(cf, opts, oldSrc, newSrc, stdout)
}

// runSemanticReview runs the full pipeline (detect → parse → extract → match → correlate → diff → report).
func runSemanticReview(cf compareFlags, opts report.Options, oldSrc, newSrc string, stdout io.Writer) error {
	return run_semantic_review_documents(cf, opts, oldSrc, newSrc, nil, nil, stdout)
}

func run_semantic_review_documents(cf compareFlags, opts report.Options,
	oldSrc, newSrc string, oldDoc, newDoc *ir.SemanticDocument, stdout io.Writer) error {
	// Build pipeline with shared word-diff and ignore-space options.
	d := diff.NewWithOpts(diff.Options{
		WordDiff:              cf.wordDiff,
		WordDiffSpanThreshold: cf.wordDiffSpanThreshold,
		IgnoreSpace:           cf.ignoreSpace,
	})

	// Detect language support first: an unsupported side falls back without being parsed.
	_, oldOK := lang.Detect(cf.oldName)
	_, newOK := lang.Detect(cf.newName)
	separateMacros := separate_macro_functions(cf.oldName, cf.newName)

	var result *ir.CorrelationResult
	usedFallback := false
	if !oldOK || !newOK {
		// Unsupported-language fallback: one whole-file diff under "Other" (never a deceptive all-zero summary).
		usedFallback = true
		result = synthesizeFallbackDiff(oldSrc, newSrc, cf.oldName, cf.newName, d)
	} else {
		if oldDoc == nil {
			var err error
			oldDoc, err = parseAndExtract(oldSrc, cf.oldName, separateMacros)
			if err != nil {
				return fmt.Errorf("parsing old file: %w", err)
			}
		}
		if newDoc == nil {
			var err error
			newDoc, err = parseAndExtract(newSrc, cf.newName, separateMacros)
			if err != nil {
				return fmt.Errorf("parsing new file: %w", err)
			}
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

			// Comments strictly inside a function-like container already render there; a
			// standalone entry would duplicate. See [doc/enclosed-comments.md](../../doc/enclosed-comments.md).
			correlate.SuppressEnclosedComments(result)

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
			Old:          &ir.SemanticBlock{Source: oldSrc, Kind: ir.KindUnknown, Name: report.DisplayPath(cf.oldName)},
			New:          &ir.SemanticBlock{Source: newSrc, Kind: ir.KindUnknown, Name: report.DisplayPath(cf.newName)},
			InnerDiff:    d.Diff(oldSrc, newSrc),
			Confidence:   1.0,
			MatchType:    ir.MatchSimilarity,
			Supplemental: true,
		})
	}

	// Report: print the git-style header; the reporter draws no header of its own.
	return writeReport(stdout, opts, result, cf.oldName, cf.newName)
}

// runSimpleDiff emits a plain whole-file line diff, skipping the semantic pipeline entirely.
func runSimpleDiff(cf compareFlags, opts report.Options, oldSrc, newSrc string, stdout io.Writer) error {
	d := diff.NewWithOpts(diff.Options{
		WordDiff:              cf.wordDiff,
		WordDiffSpanThreshold: cf.wordDiffSpanThreshold,
		IgnoreSpace:           cf.ignoreSpace,
	})
	return writeReport(stdout, opts, synthesizeFallbackDiff(oldSrc, newSrc, cf.oldName, cf.newName, d), cf.oldName, cf.newName)
}

// synthesizeFallbackDiff builds a single whole-file diff for unsupported languages / no blocks (C2).
func synthesizeFallbackDiff(oldSrc, newSrc, oldName, newName string, d *diff.Differ) *ir.CorrelationResult {
	whole := d.Diff(oldSrc, newSrc)
	return &ir.CorrelationResult{
		Pairs: []ir.CorrelatedPair{{
			Old:        &ir.SemanticBlock{Source: oldSrc, Kind: ir.KindUnknown, Name: report.DisplayPath(oldName)},
			New:        &ir.SemanticBlock{Source: newSrc, Kind: ir.KindUnknown, Name: report.DisplayPath(newName)},
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
	if _, err := fmt.Fprintf(stdout, "diff --cmscout %s %s\n", report.DisplayPath(oldName), report.DisplayPath(newName)); err != nil {
		return err
	}
	return r.Write(stdout, result)
}

// separate_macro_functions separates macro kinds only when all inputs are C/C++.
func separate_macro_functions(paths ...string) bool {
	for _, path := range paths {
		language, known := lang.Detect(path)
		if !known || !lang.HasMacroFunctions(language) {
			return false
		}
	}
	return len(paths) > 0
}

func parseAndExtract(src, path string, separateMacros bool) (*ir.SemanticDocument, error) {
	ast, err := parser.ParsePath([]byte(src), path)
	if err != nil {
		return nil, err
	}
	defer ast.Close()
	return extract.NewWithOptions(path, extract.Options{SeparateMacroFunctions: separateMacros}).Extract(ast)
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
	fmt.Fprint(w, `cmscout — code motion scout

Usage:
  cmscout [flags] <old_file> <new_file>
  cmscout --simple-diff [flags] <old_file> <new_file>
  cmscout --stats [flags] <file>
  cmscout --stats --scan [flags] [path ...]
  cmscout --metrics [flags] <file>
  cmscout --metrics [flags] <before_file> <after_file>
  cmscout --metrics --scan [flags] [path ...]
  cmscout --metrics --staged|--worktree [flags]
  cmscout --metrics --revision <commit> [--parent <number>|--base <commit>] [flags]

  The default mode parses both files and reports the change per semantic
  block (functions, classes, methods, constants, imports, JSX elements,
  ...). With --simple-diff the semantic pipeline is skipped entirely and
  the output is a plain whole-file line diff.

  <old_file>/<new_file> are file paths; "-" reads one side from stdin
  (only one side may be stdin).

Flags:
  --stats                 Report single-file block sizes, comments, methods and complexity
  --max-comment-lines N   In --stats, flag long comments (default 5; 0 disables)
  --metrics               Measure one file, or compare metrics and diff two files
  --format text|json      Metrics output format (default: text)
  --explain               Include each complexity decision and its location
  --root <dir>            Root for qualified names (default: infer from source path)
  --name <file>           Logical name for a single metrics input
  --stdin-name <file>     Logical filename for metrics read from '-'
  --scan                  Scan files and directories (default: current directory)
  --staged                Compare HEAD with the index
  --worktree              Compare the index with tracked working files
  --revision <commit>     Compare a commit with its parent
  --parent <number>       Choose a parent of --revision (required for merges)
  --base <commit>         Compare --revision against this commit instead
  --include <glob>        Include root-relative scan paths, repeatable; ** spans dirs
  --exclude <glob>        Exclude root-relative scan paths, repeatable
  --version               Show version
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

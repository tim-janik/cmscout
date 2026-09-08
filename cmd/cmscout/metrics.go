package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cmscout/pkg/analysis"
	"cmscout/pkg/extract"
	"cmscout/pkg/ir"
	"cmscout/pkg/lang"
	"cmscout/pkg/metrics"
	"cmscout/pkg/report"
	"cmscout/pkg/source"
)

type metrics_flags struct {
	enabled    bool
	format     string
	root       string
	name       string
	stdin_name string
	explain    bool
	scan       bool
	include    []string
	exclude    []string
}

func (mf *metrics_flags) register(fs *flag.FlagSet) {
	fs.BoolVar(&mf.enabled, "metrics", false, "measure one source file, or compare two files")
	fs.StringVar(&mf.format, "format", "text", "metrics output format: text or json")
	fs.StringVar(&mf.root, "root", "", "source root for qualified metric names")
	fs.StringVar(&mf.name, "name", "", "logical name for a single metrics input")
	fs.StringVar(&mf.stdin_name, "stdin-name", "", "logical filename and language for metrics read from stdin")
	fs.BoolVar(&mf.explain, "explain", false, "include the decisions contributing to complexity")
	fs.BoolVar(&mf.scan, "scan", false, "measure files and directories independently")
	fs.Func("include", "include root-relative scan glob, repeatable", func(value string) error {
		mf.include = append(mf.include, value)
		return nil
	})
	fs.Func("exclude", "exclude root-relative scan glob, repeatable", func(value string) error {
		mf.exclude = append(mf.exclude, value)
		return nil
	})
}

func metrics_requested(fs *flag.FlagSet, args []string) bool {
	requested := false
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if argument == "--" || argument == "-" || !strings.HasPrefix(argument, "-") {
			break
		}
		name, value, assigned := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		if name == "metrics" {
			requested = true
			if assigned {
				if parsed, err := strconv.ParseBool(value); err == nil {
					requested = parsed
				}
			}
		}
		if option := fs.Lookup(name); option != nil && !assigned {
			boolean, ok := option.Value.(interface{ IsBoolFlag() bool })
			if !ok || !boolean.IsBoolFlag() {
				i++
			}
		}
	}
	return requested
}

type metrics_error struct{ cause error }

func (err metrics_error) Error() string { return err.cause.Error() }
func (err metrics_error) Unwrap() error { return err.cause }

func exit_code(err error) int {
	if err == nil {
		return 0
	}
	var metric_error metrics_error
	if errors.As(err, &metric_error) {
		return 2
	}
	return 1
}

func (mf metrics_flags) run(fs *flag.FlagSet, cf compareFlags, stdin io.Reader, stdout io.Writer) error {
	paths := fs.Args()
	if !mf.scan && len(paths) == 1 && mf.name == "" && mf.stdin_name == "" {
		if info, err := os.Stat(paths[0]); err == nil && info.IsDir() {
			mf.scan = true
		}
	}
	pair := !mf.scan && (len(paths) == 2 || cf.oldName != "" || cf.newName != "" || cf.beforeFile != "" || cf.afterFile != "")
	var invalid string
	fs.Visit(func(option *flag.Flag) {
		switch option.Name {
		case "metrics", "format", "root", "name", "stdin-name", "explain", "no-color":
		case "scan", "include", "exclude":
			if !mf.scan && invalid == "" {
				invalid = option.Name
			}
		case "old", "new", "B", "A", "before-contents", "after-contents", "word-diff", "word-diff-span-threshold", "ignore-all-space":
			if !pair && invalid == "" {
				invalid = option.Name
			}
		default:
			if invalid == "" {
				invalid = option.Name
			}
		}
	})
	if invalid != "" {
		return fmt.Errorf("--%s cannot be used with this metrics input mode", invalid)
	}
	if mf.format != "text" && mf.format != "json" {
		return fmt.Errorf("unknown metrics format %q: expected text or json", mf.format)
	}
	if mf.scan {
		if mf.name != "" || mf.stdin_name != "" {
			return fmt.Errorf("scan inputs use their filesystem names; --name and --stdin-name are not supported")
		}
		return mf.scan_files(paths, stdout)
	}
	if pair {
		return mf.compare(cf, paths, stdin, stdout)
	}
	if len(paths) != 1 {
		return fmt.Errorf("usage: cmscout --metrics [flags] <file>")
	}
	path, logical_name := paths[0], paths[0]
	if mf.name != "" {
		logical_name = mf.name
	}
	if path == "-" {
		if mf.stdin_name == "" || mf.name != "" {
			return fmt.Errorf("stdin metrics require --stdin-name <file> and no --name")
		}
		logical_name = mf.stdin_name
	} else {
		if mf.stdin_name != "" {
			return fmt.Errorf("--stdin-name requires '-' as the input")
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("directory metrics are not implemented yet: %s", path)
		}
	}
	options, err := metric_context(logical_name, mf.root, mf.name != "" || path == "-")
	if err != nil {
		return err
	}
	options.Explain = mf.explain
	source, err := loadFile(path, stdin)
	if err != nil {
		return err
	}
	ast, err := analysis.Parse(context.Background(), []byte(source), options.Path)
	if err != nil {
		return err
	}
	defer ast.Close()
	snapshot, err := metrics.Measure(ast, options)
	if err != nil {
		return err
	}
	if mf.format == "json" {
		err = report.WriteMetricsJSON(stdout, snapshot)
	} else {
		err = report.WriteMetricsText(stdout, snapshot)
	}
	if err != nil {
		return err
	}
	if snapshot.Status != "complete" {
		return fmt.Errorf("metrics incomplete for %s; see report diagnostics", options.Path)
	}
	return nil
}

func (mf metrics_flags) compare(cf compareFlags, paths []string, stdin io.Reader, stdout io.Writer) error {
	if mf.name != "" || mf.stdin_name != "" {
		return fmt.Errorf("two-file metrics use --old/--new for logical names and -B/-A for content paths")
	}
	old_virtual, new_virtual := cf.oldName != "", cf.newName != ""
	old_path, new_path, err := cf.resolve(paths)
	if err != nil {
		return err
	}
	old_options, err := metric_context(cf.oldName, mf.root, old_virtual)
	if err != nil {
		return err
	}
	new_options, err := metric_context(cf.newName, mf.root, new_virtual)
	if err != nil {
		return err
	}
	old_options.Explain, new_options.Explain = mf.explain, mf.explain
	old_source, new_source, err := loadPair(old_path, new_path, stdin)
	if err != nil {
		return err
	}
	old_language, old_known := lang.Detect(old_options.Path)
	new_language, new_known := lang.Detect(new_options.Path)
	separate_macros := old_known && new_known && lang.HasMacroFunctions(old_language) && lang.HasMacroFunctions(new_language)
	before, old_document, err := measure_for_diff(old_source, old_options, separate_macros)
	if err != nil {
		return fmt.Errorf("before metrics: %w", err)
	}
	after, new_document, err := measure_for_diff(new_source, new_options, separate_macros)
	if err != nil {
		return fmt.Errorf("after metrics: %w", err)
	}
	comparison, err := metrics.Compare(before, after)
	if err != nil {
		return err
	}
	var rendered bytes.Buffer
	cf.oldName, cf.newName, cf.noColor = old_options.Path, new_options.Path, true
	err = runSemanticReviewDocuments(cf, false, false, "white", "white", old_source, new_source, old_document, new_document, &rendered)
	if err != nil {
		return err
	}
	comparison.Diff = &metrics.RenderedDiff{Format: "cmscout-text-v1", Text: rendered.String()}
	if mf.format == "json" {
		err = report.WriteComparisonJSON(stdout, comparison)
	} else {
		err = report.WriteComparisonText(stdout, comparison)
	}
	if err != nil {
		return err
	}
	if comparison.Status != "complete" {
		return fmt.Errorf("metrics comparison incomplete; see report diagnostics")
	}
	return nil
}

func measure_for_diff(source string, options metrics.Options, separate_macros bool) (*metrics.Snapshot, *ir.SemanticDocument, error) {
	ast, err := analysis.Parse(context.Background(), []byte(source), options.Path)
	if err != nil {
		return nil, nil, err
	}
	defer ast.Close()
	snapshot, err := metrics.Measure(ast, options)
	if err != nil {
		return nil, nil, err
	}
	document, err := extract.NewWithOptions(options.Path, extract.Options{SeparateMacroFunctions: separate_macros}).Extract(ast)
	return snapshot, document, err
}

func metric_context(name, root string, virtual bool) (metrics.Options, error) {
	if root != "" {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return metrics.Options{}, err
		}
		root = absolute
		info, err := os.Stat(root)
		if err != nil {
			return metrics.Options{}, err
		}
		if !info.IsDir() {
			return metrics.Options{}, fmt.Errorf("metrics root must be a directory: %s", root)
		}
		if virtual && !filepath.IsAbs(name) {
			name = filepath.Join(root, name)
		}
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return metrics.Options{}, err
	}
	if root == "" {
		root = source.InferRoot(filepath.Dir(absolute))
	}
	logical, err := filepath.Rel(root, absolute)
	if err != nil {
		return metrics.Options{}, err
	}
	if logical == ".." || strings.HasPrefix(logical, ".."+string(filepath.Separator)) || logical == "." {
		return metrics.Options{}, fmt.Errorf("logical metrics input %s is outside source root %s", name, root)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(root)))
	namespace := fmt.Sprintf("%s@%x", filepath.Base(root), digest[:8])
	return metrics.Options{Namespace: namespace, Path: filepath.ToSlash(logical)}, nil
}

// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import (
	"fmt"
	"io"

	"cmscout/pkg/lang"
	"cmscout/pkg/stats"
)

func runStats(src, path string, maxCommentLines int, stdout io.Writer) error {
	// Separate C/C++ macro functions into their own kind for finer statistics.
	separateMacros := false
	if langCode, ok := lang.Detect(path); ok {
		separateMacros = lang.HasMacroFunctions(langCode)
	}

	doc, ast, err := parseAndExtractWithAST(src, path, separateMacros)
	if ast != nil {
		defer ast.Close()
	}
	if err != nil {
		return err
	}

	report, err := stats.Analyze(doc, ast, stats.Options{MaxCommentLines: maxCommentLines})
	if err != nil {
		return fmt.Errorf("computing statistics: %w", err)
	}
	return stats.Render(stdout, report)
}

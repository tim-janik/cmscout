package analysis

import (
	"context"
	"fmt"

	"cmscout/pkg/lang"
	"cmscout/pkg/parser"
)

func Parse(ctx context.Context, source []byte, path string) (*parser.AST, error) {
	language, ok := lang.Detect(path)
	if !ok {
		return nil, fmt.Errorf("unsupported language for %s", path)
	}
	primary, primary_error := parse_language(ctx, source, language)
	if language.Ext != ".h" {
		return primary, primary_error
	}
	c_tree, c_error := parse_language(ctx, source, lang.Language{Name: "c", Ext: ".h"})
	switch {
	case primary_error == nil && c_error == nil:
		if c_tree.ErrorCount() < primary.ErrorCount() {
			primary.Close()
			return c_tree, nil
		}
		c_tree.Close()
		return primary, nil
	case primary_error == nil:
		return primary, nil
	case c_error == nil:
		return c_tree, nil
	default:
		return nil, fmt.Errorf("C++ parse: %w; C parse: %w", primary_error, c_error)
	}
}

func parse_language(ctx context.Context, source []byte, language lang.Language) (*parser.AST, error) {
	p, err := parser.New(language)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	return p.Parse(ctx, source)
}

// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package parser

import (
	"fmt"

	"cmscout/pkg/lang"
)

// ParsePath detects the language from path; .h files keep the C or C++ parse with fewer errors, ties keep C++.
func ParsePath(source []byte, path string) (*AST, error) {
	language, ok := lang.Detect(path)
	if !ok {
		return nil, fmt.Errorf("unsupported language for %s", path)
	}
	primary, primary_error := parse_language(source, language)
	if language.Ext != ".h" {
		return primary, primary_error
	}
	c_tree, c_error := parse_language(source, lang.Language{Name: "c", Ext: ".h"})
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

func parse_language(source []byte, language lang.Language) (*AST, error) {
	p, err := New(language)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	return p.Parse(source)
}

// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package parser wraps tree-sitter (Phase 1: source → AST) for TS/TSX/JS/JSX/Go/Bash/C/C++; CGO required.
package parser

import (
	"fmt"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	bashlang "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	clang "github.com/tree-sitter/tree-sitter-c/bindings/go"
	cpplang "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	jslang "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tslang "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"cmscout/pkg/lang"
)

// Parser wraps a tree-sitter parser for a specific language.
type Parser struct {
	ts      *tree_sitter.Parser
	srcLang lang.Language // carried onto the AST; see Parse
}

// New creates a parser for the given language.
func New(l lang.Language) (*Parser, error) {
	p := tree_sitter.NewParser()
	if p == nil {
		return nil, fmt.Errorf("failed to create tree-sitter parser")
	}

	var langPtr unsafe.Pointer
	switch l.Name {
	case "ts":
		langPtr = tslang.LanguageTypescript()
	case "tsx", "jsx":
		// The TSX grammar handles both TSX and JSX.
		langPtr = tslang.LanguageTSX()
	case "js":
		langPtr = jslang.Language()
	case "go":
		langPtr = golang.Language()
	case "bash":
		langPtr = bashlang.Language()
	case "c":
		langPtr = clang.Language()
	case "cpp":
		langPtr = cpplang.Language()
	default:
		p.Close()
		return nil, fmt.Errorf("unsupported language: %s", l.Name)
	}

	if err := p.SetLanguage(tree_sitter.NewLanguage(langPtr)); err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to set language %q: %w", l.Name, err)
	}

	return &Parser{ts: p, srcLang: l}, nil
}

// Close releases the parser resources.
func (p *Parser) Close() {
	if p == nil {
		return
	}
	if p.ts != nil {
		p.ts.Close()
		p.ts = nil
	}
}

// Parse parses source code and returns an AST.
func (p *Parser) Parse(source []byte) (*AST, error) {
	if p == nil {
		return nil, fmt.Errorf("parser is nil")
	}
	if p.ts == nil {
		return nil, fmt.Errorf("parser is closed")
	}
	tree := p.ts.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse source: parser returned nil tree")
	}
	return &AST{tree: tree, src: source, lang: p.srcLang}, nil
}

// AST wraps a tree-sitter tree and the source bytes (locations and text preserved).
type AST struct {
	tree *tree_sitter.Tree
	src  []byte
	lang lang.Language
}

// Close releases the underlying tree resources.
func (a *AST) Close() {
	if a != nil && a.tree != nil {
		a.tree.Close()
		a.tree = nil
	}
}

// RootNode returns the root node of the syntax tree.
func (a *AST) RootNode() *tree_sitter.Node {
	if a == nil || a.tree == nil {
		return nil
	}
	return a.tree.RootNode()
}

// Source returns the original source bytes.
func (a *AST) Source() []byte { return a.src }

// Language returns the language this AST was parsed with.
func (a *AST) Language() lang.Language { return a.lang }

// ErrorCount counts error and missing nodes: a non-zero count means extraction is
// incomplete and must be surfaced. One-time walk; well-formed files return 0 directly.
func (a *AST) ErrorCount() int {
	root := a.RootNode()
	if root == nil || !root.HasError() {
		return 0
	}
	return countErrors(root)
}

// countErrors recursively counts error and missing nodes, including
// anonymous/extra children so recovery nodes are not missed. Missing nodes
// carry the kind of the token they stand in for, so IsMissing is required.
func countErrors(n *tree_sitter.Node) int {
	if n == nil {
		return 0
	}
	count := 0
	if n.IsError() || n.IsMissing() {
		count++
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		count += countErrors(n.Child(i))
	}
	return count
}

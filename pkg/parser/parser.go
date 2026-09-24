// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package parser wraps tree-sitter (Phase 1: source → AST) for TS/TSX/JS/JSX/Go/Bash/C/C++; CGO required.
package parser

import (
	"context"
	"fmt"
	"sync"
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
	mu      sync.Mutex
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ts != nil {
		p.ts.Close()
		p.ts = nil
	}
}

// Parse parses source code and returns an AST.
func (p *Parser) Parse(ctx context.Context, source []byte) (*AST, error) {
	if p == nil {
		return nil, fmt.Errorf("parser is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Parse in a goroutine and close the tree if cancellation wins; otherwise native memory leaks.
	sourceCopy := append([]byte(nil), source...)
	type parseResult struct {
		tree *tree_sitter.Tree
		err  error
	}
	ch := make(chan parseResult)
	go func() {
		p.mu.Lock()
		if p.ts == nil {
			p.mu.Unlock()
			select {
			case ch <- parseResult{err: fmt.Errorf("parser is closed")}:
			case <-ctx.Done():
			}
			return
		}
		tree := p.ts.Parse(sourceCopy, nil)
		p.mu.Unlock()
		select {
		case ch <- parseResult{tree: tree}:
		case <-ctx.Done():
			if tree != nil {
				tree.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return nil, result.err
		}
		if result.tree == nil {
			return nil, fmt.Errorf("failed to parse source: parser returned nil tree")
		}
		tree := result.tree
		return &AST{
			tree: tree,
			src:  sourceCopy,
			lang: p.srcLang,
		}, nil
	}
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

// ErrorCount includes missing tokens inserted during error recovery.
func (a *AST) ErrorCount() int {
	root := a.RootNode()
	if root == nil || !root.HasError() {
		return 0
	}
	return len(a.Diagnostics())
}

type Diagnostic struct {
	Kind      string
	Token     string
	StartByte uint
	EndByte   uint
	StartLine uint
	StartCol  uint
	EndLine   uint
	EndCol    uint
}

func (a *AST) Diagnostics() []Diagnostic {
	diagnostics := []Diagnostic{}
	root := a.RootNode()
	if root == nil || !root.HasError() {
		return diagnostics
	}
	var visit func(*tree_sitter.Node)
	visit = func(node *tree_sitter.Node) {
		if node.IsError() || node.IsMissing() {
			kind := "error"
			if node.IsMissing() {
				kind = "missing"
			}
			start, end := node.StartPosition(), node.EndPosition()
			diagnostics = append(diagnostics, Diagnostic{
				Kind: kind, Token: node.Kind(), StartByte: node.StartByte(), EndByte: node.EndByte(),
				StartLine: start.Row, StartCol: start.Column, EndLine: end.Row, EndCol: end.Column,
			})
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			visit(node.Child(i))
		}
	}
	visit(root)
	return diagnostics
}

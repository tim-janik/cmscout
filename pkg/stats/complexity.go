// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package stats

import (
	"fmt"

	"cmscout/pkg/ir"
	"cmscout/pkg/metrics"
	"cmscout/pkg/parser"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type byte_span struct{ start, end uint }

func callable_wrapper(kind string) bool {
	switch kind {
	case "export_statement", "lexical_declaration", "variable_statement", "variable_declarator", "declaration", "init_declarator", "template_declaration", "friend_declaration":
		return true
	}
	return false
}

func function_stats(doc *ir.SemanticDocument, ast *parser.AST) (*ir.SemanticDocument, map[string]int, *metrics.Snapshot, error) {
	snapshot, err := metrics.Measure(ast, metrics.Options{Namespace: "stats", Path: doc.FilePath})
	if err != nil {
		return nil, nil, nil, err
	}
	copy_doc := *doc
	copy_doc.Blocks = append([]ir.SemanticBlock(nil), doc.Blocks...)
	blocks := make(map[byte_span]int)
	for i, b := range copy_doc.Blocks {
		blocks[byte_span{b.Span.StartByte, b.Span.EndByte}] = i
	}
	callables := make(map[byte_span]metrics.Component)
	owners := make(map[string]metrics.Component)
	for _, c := range snapshot.Components {
		owners[c.QualifiedName] = c
		if c.FunctionMetrics != nil {
			callables[byte_span{c.Span.StartByte, c.Span.EndByte}] = c
		}
	}
	types := make(map[string]string)
	for _, b := range copy_doc.Blocks {
		if b.Parent == "" && is_container_kind(b.Kind) {
			types[b.Name] = b.ID
		}
	}
	complexities := make(map[string]int)
	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if c, ok := callables[byte_span{node.StartByte(), node.EndByte()}]; ok {
			index := -1
			for current := node; current != nil; current = current.Parent() {
				if current.Id() != node.Id() && !callable_wrapper(current.Kind()) {
					break
				}
				if i, ok := blocks[byte_span{current.StartByte(), current.EndByte()}]; ok && is_function_like(copy_doc.Blocks[i].Kind) {
					index = i
					break
				}
			}
			if index < 0 {
				kind := ir.KindFunction
				switch c.Kind {
				case "method":
					kind = ir.KindMethod
				case "arrow_function":
					kind = ir.KindArrowFunc
				case "lambda":
					kind = ir.KindLambda
				}
				name := c.Name
				start, end := node.StartPosition(), node.EndPosition()
				if c.NameOrigin == "ordinal" {
					name = fmt.Sprintf("%s.%d.%d", kind, start.Row+1, start.Column+1)
				}
				parent := ""
				for current := node.Parent(); current != nil; current = current.Parent() {
					if i, ok := blocks[byte_span{current.StartByte(), current.EndByte()}]; ok {
						parent = copy_doc.Blocks[i].ID
						break
					}
				}
				index = len(copy_doc.Blocks)
				blocks[byte_span{node.StartByte(), node.EndByte()}] = index
				copy_doc.Blocks = append(copy_doc.Blocks, ir.SemanticBlock{
					ID: fmt.Sprintf("stats:%s:%d", kind, node.StartByte()), Kind: kind, Name: name, Parent: parent,
					Span:   ir.SourceSpan{StartByte: node.StartByte(), EndByte: node.EndByte(), StartLine: start.Row, StartCol: start.Column, EndLine: end.Row, EndCol: end.Column},
					Source: node.Utf8Text(ast.Source()),
				})
			}
			if owner := owners[c.ParentName]; copy_doc.Blocks[index].Parent == "" && owner.Kind == "type" {
				copy_doc.Blocks[index].Parent = types[owner.Name]
			}
			if c.Cyclomatic.Value != nil {
				complexities[copy_doc.Blocks[index].ID] = *c.Cyclomatic.Value
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(ast.RootNode())
	return &copy_doc, complexities, snapshot, nil
}

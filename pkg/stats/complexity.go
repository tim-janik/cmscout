package stats

import (
	"fmt"

	"cmscout/pkg/ir"
	"cmscout/pkg/parser"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type byte_span struct {
	start uint
	end   uint
}

func callable_kind(kind string) ir.BlockKind {
	switch kind {
	case "function_declaration", "function_definition", "function_expression", "generator_function", "generator_function_declaration":
		return ir.KindFunction
	case "method_definition", "method_declaration":
		return ir.KindMethod
	case "arrow_function":
		return ir.KindArrowFunc
	case "func_literal", "lambda_expression":
		return ir.KindLambda
	}
	return ""
}

func callable_wrapper(kind string) bool {
	switch kind {
	case "export_statement", "lexical_declaration", "variable_statement", "variable_declarator",
		"declaration", "init_declarator", "template_declaration", "friend_declaration":
		return true
	}
	return false
}

func function_stats(doc *ir.SemanticDocument, ast *parser.AST) (*ir.SemanticDocument, map[string]int) {
	copy_doc := *doc
	copy_doc.Blocks = append([]ir.SemanticBlock(nil), doc.Blocks...)
	blocks := make(map[byte_span]int)
	for i, b := range copy_doc.Blocks {
		blocks[byte_span{b.Span.StartByte, b.Span.EndByte}] = i
	}
	complexities := make(map[string]int)
	var walk func(*tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		if node == nil {
			return
		}
		kind := callable_kind(node.Kind())
		if kind != "" && node.ChildByFieldName("body") != nil {
			index := -1
			for current := node; current != nil; current = current.Parent() {
				if current.Id() != node.Id() && !callable_wrapper(current.Kind()) {
					break
				}
				if i, ok := blocks[byte_span{current.StartByte(), current.EndByte()}]; ok && isFunctionLike(copy_doc.Blocks[i].Kind) {
					index = i
					break
				}
			}
			if index < 0 {
				name := ""
				if n := node.ChildByFieldName("name"); n != nil {
					name = n.Utf8Text(ast.Source())
				}
				if name == "" {
					if parent := node.Parent(); parent != nil {
						switch parent.Kind() {
						case "variable_declarator", "init_declarator":
							for _, field := range []string{"name", "declarator"} {
								if n := parent.ChildByFieldName(field); n != nil {
									name = n.Utf8Text(ast.Source())
								}
							}
						}
					}
				}
				start, end := node.StartPosition(), node.EndPosition()
				if name == "" {
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
					Span: ir.SourceSpan{StartByte: node.StartByte(), EndByte: node.EndByte(),
						StartLine: start.Row, StartCol: start.Column, EndLine: end.Row, EndCol: end.Column},
					Source: node.Utf8Text(ast.Source()),
				})
			}
			if !node.HasError() {
				complexities[copy_doc.Blocks[index].ID] = 1 + function_decisions(node, ast.Language().Name, ast.Source())
			}
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(ast.RootNode())
	return &copy_doc, complexities
}

func function_decisions(node *tree_sitter.Node, language string, src []byte) int {
	count := 0
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		field := node.FieldNameForNamedChild(uint32(i))
		if language == "c" || language == "cpp" || language == "go" {
			if field != "body" && child.Kind() != "field_initializer_list" {
				continue
			}
		} else if field == "name" || child.Kind() == "computed_property_name" || child.Kind() == "decorator" {
			continue
		}
		count += count_decisions(child, language, src)
	}
	return count
}

func count_decisions(node *tree_sitter.Node, language string, src []byte) int {
	if callable_kind(node.Kind()) != "" {
		return definition_decisions(node, language, src)
	}
	switch node.Kind() {
	case "class_body":
		count := 0
		for i := uint(0); i < node.NamedChildCount(); i++ {
			count += definition_decisions(node.NamedChild(i), language, src)
		}
		return count
	case "field_declaration_list", "type_annotation", "type_alias_declaration", "type_alias_statement",
		"interface_declaration", "type_query", "type_descriptor", "preproc_function_def", "preproc_def",
		"preproc_include", "preproc_call", "static_assert_declaration", "requires_expression", "noexcept_expression",
		"decltype", "alignof_expression", "noexcept", "sizeof_expression":
		return 0
	case "call_expression":
		if language == "cpp" {
			if function := node.ChildByFieldName("function"); function != nil && function.Utf8Text(src) == "noexcept" {
				return 0
			}
		}
	}
	count := decision_weight(node, language, src)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if (node.Kind() == "preproc_if" || node.Kind() == "preproc_elif") && node.FieldNameForNamedChild(uint32(i)) == "condition" {
			continue
		}
		count += count_decisions(node.NamedChild(i), language, src)
	}
	return count
}

func definition_decisions(node *tree_sitter.Node, language string, src []byte) int {
	count := 0
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Kind() {
		case "computed_property_name", "decorator", "lambda_capture_specifier":
			count += count_decisions(child, language, src)
		}
	}
	return count
}

func decision_weight(node *tree_sitter.Node, language string, src []byte) int {
	switch node.Kind() {
	case "if_statement", "elif_clause", "conditional_expression", "ternary_expression",
		"for_statement", "for_in_statement", "for_range_loop", "c_style_for_statement",
		"while_statement", "do_statement", "switch_case", "catch_clause":
		return 1
	case "case_statement":
		if language == "c" || language == "cpp" {
			if node.ChildByFieldName("value") != nil {
				return 1
			}
		}
	case "expression_case":
		if value := node.ChildByFieldName("value"); value != nil {
			count := 0
			for i := uint(0); i < value.NamedChildCount(); i++ {
				if !value.NamedChild(i).IsExtra() {
					count++
				}
			}
			return count
		}
	case "type_case":
		count := 0
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if node.FieldNameForNamedChild(uint32(i)) == "type" {
				count++
			}
		}
		return count
	case "select_statement":
		count := 0
		for i := uint(0); i < node.NamedChildCount(); i++ {
			switch node.NamedChild(i).Kind() {
			case "communication_case", "default_case":
				count++
			}
		}
		return max(0, count-1)
	case "case_item":
		count := 0
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if node.FieldNameForNamedChild(uint32(i)) == "value" {
				if node.NamedChild(i).Utf8Text(src) == "*" {
					break
				}
				count++
			}
		}
		return count
	case "binary_expression", "list", "augmented_assignment_expression":
		count := 0
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Kind() {
			case "&&", "||", "??", "&&=", "||=", "??=":
				count++
			case "test_operator":
				if language == "bash" && (child.Utf8Text(src) == "-a" || child.Utf8Text(src) == "-o") {
					count++
				}
			case "and", "or":
				if language == "cpp" {
					count++
				}
			}
		}
		return count
	case "call_expression":
		for i := uint(0); i < node.ChildCount(); i++ {
			if node.Child(i).Kind() == "?." {
				return 1
			}
		}
	case "optional_chain", "assignment_pattern", "object_assignment_pattern":
		return 1
	case "required_parameter", "optional_parameter":
		if node.ChildByFieldName("value") != nil {
			return 1
		}
	case "expansion":
		if op := node.ChildByFieldName("operator"); op != nil {
			switch op.Utf8Text(src) {
			case "-", ":-", "+", ":+", "=", ":=", "?", ":?":
				return 1
			}
		}
	}
	return 0
}

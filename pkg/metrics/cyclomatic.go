// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package metrics

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func (inv *inventory) count_complexity(root *tree_sitter.Node) {
	for _, unit := range inv.units {
		value := 1
		inv.components[unit.index].Cyclomatic = Cyclomatic{Value: &value, Status: "complete"}
	}
	inv.count_node(root, -1)
}

func (inv *inventory) count_node(node *tree_sitter.Node, owner int) {
	if excluded_syntax(node.Kind()) || inv.unevaluated_call(node) {
		owner = -1
	}
	if index, exists := inv.nodes[node.Id()]; exists && inv.components[index].FunctionMetrics != nil {
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			field := node.FieldNameForChild(uint32(i))
			child_owner := -1
			if field == "body" || inv.language == "bash" && field == "redirect" ||
				inv.language == "c" && field == "declarator" || child.Kind() == "field_initializer_list" ||
				(inv.is_javascript() || inv.language == "c") && (field == "parameters" || field == "parameter") {
				child_owner = index
			} else if field == "captures" || node.Kind() == "method_definition" && (field == "name" || child.Kind() == "decorator") {
				child_owner = owner
			}
			inv.count_node(child, child_owner)
		}
		return
	}
	if node.Kind() == "class_static_block" || node.Kind() == "field_declaration" {
		owner = -1
	}
	if node.Kind() == "field_definition" || node.Kind() == "public_field_definition" {
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			child_owner := -1
			if child.Kind() == "computed_property_name" || child.Kind() == "decorator" {
				child_owner = owner
			}
			inv.count_node(child, child_owner)
		}
		return
	}
	if owner >= 0 {
		inv.count_decision(node, owner)
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if strings.HasPrefix(node.Kind(), "preproc_") && node.FieldNameForChild(uint32(i)) == "condition" {
			continue
		}
		child_owner := owner
		if inv.language == "c" && (node.Kind() == "array_declarator" || node.Kind() == "abstract_array_declarator") &&
			node.FieldNameForChild(uint32(i)) == "size" {
			child_owner = -1
		}
		inv.count_node(child, child_owner)
	}
}

func (inv *inventory) unevaluated_call(node *tree_sitter.Node) bool {
	if node.Kind() != "call_expression" {
		return false
	}
	name := node_text(node.ChildByFieldName("function"), inv.source)
	return inv.language == "cpp" && name == "noexcept" || inv.language == "c" && (name == "_Static_assert" || name == "static_assert")
}

func excluded_syntax(kind string) bool {
	switch kind {
	case "comment", "preproc_def", "preproc_function_def", "preproc_call", "preproc_include",
		"type_annotation", "type_parameters", "type_arguments", "type_query", "type_alias_declaration", "interface_declaration",
		"sizeof_expression", "alignof_expression", "decltype", "noexcept", "requires_expression", "requires_clause",
		"optional_parameter_declaration", "static_assert_declaration":
		return true
	}
	return false
}

func (inv *inventory) is_javascript() bool {
	return inv.language == "js" || inv.language == "jsx" || inv.language == "ts" || inv.language == "tsx"
}

func (inv *inventory) event(owner int, rule string, node *tree_sitter.Node) {
	metric := &inv.components[owner].Cyclomatic
	if metric.Value != nil {
		*metric.Value++
	}
	metric.Decisions = append(metric.Decisions, Decision{Rule: rule, Span: *node_span(node), Contribution: 1})
}

func token(node *tree_sitter.Node, kinds ...string) *tree_sitter.Node {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		for _, kind := range kinds {
			if child.Kind() == kind {
				return child
			}
		}
	}
	return node
}

func (inv *inventory) count_decision(node *tree_sitter.Node, owner int) {
	switch node.Kind() {
	case "if_statement", "elif_clause":
		inv.event(owner, "conditional", token(node, "if", "elif"))
	case "for_statement", "for_in_statement", "for_range_loop", "c_style_for_statement", "while_statement", "do_statement":
		inv.event(owner, "loop", token(node, "for", "while", "until", "select", "do"))
	case "conditional_expression", "ternary_expression":
		inv.event(owner, "ternary", token(node, "?"))
	case "binary_expression", "augmented_assignment_expression", "list":
		for i := uint(0); i < node.ChildCount(); i++ {
			operator := node.Child(i)
			switch operator.Kind() {
			case "&&", "||", "??", "&&=", "||=", "??=":
				inv.event(owner, "short_circuit", operator)
			case "and", "or":
				if inv.language == "cpp" {
					inv.event(owner, "short_circuit", operator)
				}
			case "test_operator":
				if inv.language == "bash" && (node_text(operator, inv.source) == "-a" || node_text(operator, inv.source) == "-o") {
					inv.event(owner, "short_circuit", operator)
				}
			}
		}
	case "optional_chain", "?.":
		if inv.is_javascript() && node.ChildCount() == 0 {
			inv.event(owner, "optional_chain", node)
		}
	case "assignment_pattern", "object_assignment_pattern":
		if inv.is_javascript() {
			inv.event(owner, "default", token(node, "="))
		}
	case "required_parameter", "optional_parameter":
		if inv.is_javascript() && node.ChildByFieldName("value") != nil {
			inv.event(owner, "default", token(node, "="))
		}
	case "switch_case":
		inv.event(owner, "case", token(node, "case"))
	case "expression_case":
		if values := node.ChildByFieldName("value"); values != nil {
			for i := uint(0); i < values.NamedChildCount(); i++ {
				value := values.NamedChild(i)
				if !value.IsExtra() {
					inv.event(owner, "case", value)
				}
			}
		}
	case "type_case":
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if node.FieldNameForNamedChild(uint32(i)) == "type" {
				inv.event(owner, "case", node.NamedChild(i))
			}
		}
	case "select_statement":
		first := true
		for i := uint(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Kind() != "communication_case" && child.Kind() != "default_case" {
				continue
			}
			if !first {
				inv.event(owner, "select", token(child, "case", "default"))
			}
			first = false
		}
	case "case_statement":
		if inv.language != "bash" && node.ChildByFieldName("value") != nil {
			inv.event(owner, "case", token(node, "case"))
		}
	case "case_item":
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if node.FieldNameForNamedChild(uint32(i)) != "value" {
				continue
			}
			value := node.NamedChild(i)
			if node_text(value, inv.source) == "*" {
				break
			}
			inv.event(owner, "case", value)
		}
	case "catch_clause":
		inv.event(owner, "catch", token(node, "catch"))
	case "expansion":
		if inv.language == "bash" {
			operator := token(node, "-", ":-", "=", ":=", "+", ":+", "?", ":?")
			if operator.Id() != node.Id() {
				inv.event(owner, "parameter_alternative", operator)
			}
		}
	}
}

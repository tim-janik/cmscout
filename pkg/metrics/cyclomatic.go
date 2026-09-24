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
	if owner >= 0 && inv.language == "c" && node.Kind() == "sizeof_expression" && dynamic_array(node) {
		inv.unsupported_array(owner)
	}
	if excluded_syntax(node.Kind()) || inv.language == "cpp" && node.Kind() == "call_expression" &&
		node_text(node.ChildByFieldName("function"), inv.source) == "noexcept" {
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
		inv.count_node(child, owner)
	}
}

func excluded_syntax(kind string) bool {
	switch kind {
	case "comment", "preproc_def", "preproc_function_def", "preproc_call", "preproc_include",
		"type_annotation", "type_parameters", "type_arguments", "type_query", "type_alias_declaration", "interface_declaration",
		"sizeof_expression", "alignof_expression", "decltype", "noexcept", "requires_expression", "requires_clause",
		"optional_parameter_declaration":
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
		operator := token(node, "&&", "||", "??", "&&=", "||=", "??=")
		if operator.Id() != node.Id() {
			inv.event(owner, "short_circuit", operator)
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
	case "switch_case", "expression_case", "type_case", "communication_case":
		inv.event(owner, "case", token(node, "case"))
	case "case_statement":
		if inv.language != "bash" && node.ChildByFieldName("value") != nil {
			inv.event(owner, "case", token(node, "case"))
		}
	case "case_item":
		if !inv.bash_default(node) {
			inv.event(owner, "case", node.ChildByFieldName("value"))
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
	case "array_declarator", "abstract_array_declarator":
		if inv.language == "c" {
			size := node.ChildByFieldName("size")
			if size != nil && size.Kind() != "number_literal" {
				inv.unsupported_array(owner)
			}
		}
	}
}

func (inv *inventory) unsupported_array(owner int) {
	inv.components[owner].Cyclomatic.Value = nil
	inv.components[owner].Cyclomatic.Status = "unsupported"
}

func dynamic_array(node *tree_sitter.Node) bool {
	if node.Kind() == "array_declarator" || node.Kind() == "abstract_array_declarator" {
		if size := node.ChildByFieldName("size"); size != nil && size.Kind() != "number_literal" {
			return true
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if dynamic_array(node.NamedChild(i)) {
			return true
		}
	}
	return false
}

func (inv *inventory) bash_default(node *tree_sitter.Node) bool {
	values := 0
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.FieldNameForChild(uint32(i)) == "value" {
			values++
		}
	}
	value := node.ChildByFieldName("value")
	if values != 1 || value == nil || node_text(value, inv.source) != "*" {
		return false
	}
	parent := node.Parent()
	for i := uint(0); i < parent.NamedChildCount(); i++ {
		child := parent.NamedChild(i)
		if child.Kind() == "case_item" && child.StartByte() > node.StartByte() {
			return false
		}
	}
	return true
}

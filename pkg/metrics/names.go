package metrics

import (
	"fmt"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func escape_name(value string) string {
	var result strings.Builder
	for _, b := range []byte(value) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-' {
			result.WriteByte(b)
		} else {
			fmt.Fprintf(&result, "%%%02X", b)
		}
	}
	return result.String()
}

func render_name(parts []NamePart) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := part.Kind + "=" + escape_name(part.Name)
		if part.Signature != "" {
			segment += "#" + escape_name(part.Signature)
		}
		if part.Ordinal > 0 {
			segment += fmt.Sprintf("~%d", part.Ordinal)
		}
		segments = append(segments, segment)
	}
	return strings.Join(segments, "::")
}

func node_text(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	return node.Utf8Text(source)
}

func declarator_name(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "operator_cast" {
		return "operator " + node_text(node.ChildByFieldName("type"), source)
	}
	if child := node.ChildByFieldName("declarator"); child != nil {
		return declarator_name(child, source)
	}
	switch node.Kind() {
	case "identifier", "field_identifier", "type_identifier", "qualified_identifier", "operator_name", "destructor_name":
		return node_text(node, source)
	case "parenthesized_declarator":
		return declarator_name(node.NamedChild(0), source)
	}
	return ""
}

func callable_binding(node *tree_sitter.Node, source []byte) (string, *tree_sitter.Node) {
	current := node
	for parent := current.Parent(); parent != nil; parent = current.Parent() {
		switch parent.Kind() {
		case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "expression_list", "literal_element":
			if parent.Kind() == "expression_list" && parent.NamedChildCount() != 1 {
				if name := parallel_binding(parent, current, source); name != "" {
					return name, declaration_anchor(parent.Parent())
				}
				return "", node
			}
			current = parent
			continue
		case "variable_declarator", "public_field_definition", "field_definition", "var_spec":
			name := parent.ChildByFieldName("name")
			if name == nil {
				name = parent.ChildByFieldName("property")
			}
			return node_text(name, source), declaration_anchor(parent)
		case "init_declarator":
			return declarator_name(parent.ChildByFieldName("declarator"), source), declaration_anchor(parent)
		case "pair", "keyed_element":
			return node_text(parent.ChildByFieldName("key"), source), parent
		case "assignment_expression", "short_var_declaration", "assignment_statement", "lambda_capture_initializer":
			left := parent.ChildByFieldName("left")
			if left != nil && (left.Kind() != "expression_list" || left.NamedChildCount() == 1) {
				return node_text(left, source), parent
			}
		}
		break
	}
	return "", node
}

func parallel_binding(list, value *tree_sitter.Node, source []byte) string {
	parent := list.Parent()
	if parent == nil {
		return ""
	}
	var names []*tree_sitter.Node
	if parent.Kind() == "var_spec" {
		for i := uint(0); i < parent.ChildCount(); i++ {
			if parent.FieldNameForChild(uint32(i)) == "name" {
				names = append(names, parent.Child(i))
			}
		}
	} else if parent.Kind() == "short_var_declaration" || parent.Kind() == "assignment_statement" {
		if left := parent.ChildByFieldName("left"); left != nil {
			for i := uint(0); i < left.NamedChildCount(); i++ {
				names = append(names, left.NamedChild(i))
			}
		}
	}
	if len(names) == int(list.NamedChildCount()) {
		for i := uint(0); i < list.NamedChildCount(); i++ {
			if list.NamedChild(i).Id() == value.Id() {
				return node_text(names[i], source)
			}
		}
	}
	return ""
}

func declaration_anchor(node *tree_sitter.Node) *tree_sitter.Node {
	for parent := node.Parent(); parent != nil; parent = node.Parent() {
		switch parent.Kind() {
		case "export_statement", "template_declaration", "lexical_declaration", "variable_declaration", "declaration", "var_declaration":
			node = parent
		default:
			return node
		}
	}
	return node
}

func source_signature(node *tree_sitter.Node, source []byte) string {
	declarator := node.ChildByFieldName("declarator")
	for declarator != nil && declarator.ChildByFieldName("parameters") == nil {
		declarator = declarator.ChildByFieldName("declarator")
	}
	if declarator == nil {
		return ""
	}
	var tokens []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current.Kind() == "comment" {
			return
		}
		if current.ChildCount() == 0 {
			tokens = append(tokens, node_text(current, source))
			return
		}
		for i := uint(0); i < current.ChildCount(); i++ {
			child := current.Child(i)
			field := current.FieldNameForChild(uint32(i))
			if current.Id() == declarator.Id() && field == "declarator" {
				continue
			}
			if (current.Kind() == "parameter_declaration" || current.Kind() == "optional_parameter_declaration") &&
				(field == "default_value" || child.Kind() == "=") {
				continue
			}
			if field == "declarator" && child.Kind() == "identifier" {
				continue
			}
			visit(child)
		}
	}
	visit(declarator)
	return strings.Join(tokens, " ")
}

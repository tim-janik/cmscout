package metrics

import (
	"fmt"
	"strings"

	"cmscout/pkg/parser"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type unit struct {
	node   *tree_sitter.Node
	anchor *tree_sitter.Node
	index  int
}

type inventory struct {
	source         []byte
	language       string
	components     []Component
	parents        []int
	units          []unit
	nodes          map[uintptr]int
	ordinals       map[string]int
	types          map[string]*tree_sitter.Node
	comments       []*tree_sitter.Node
	comment_owners []int
}

func node_span(node *tree_sitter.Node) *Span {
	if node == nil {
		return nil
	}
	start, end := node.StartPosition(), node.EndPosition()
	return &Span{node.StartByte(), node.EndByte(), start.Row, start.Column, end.Row, end.Column}
}

func discover(ast *parser.AST, options Options) *inventory {
	inv := &inventory{
		source: ast.Source(), language: ast.Language().Name,
		nodes: map[uintptr]int{}, ordinals: map[string]int{}, types: map[string]*tree_sitter.Node{},
	}
	parts := []NamePart{{Kind: "source", Name: options.Namespace}, {Kind: "file", Name: options.Path}}
	inv.components = []Component{{
		QualifiedName: render_name(parts), Name: options.Path, NameParts: parts, NameOrigin: "path", Kind: "file",
		Span: node_span(ast.RootNode()), InnerFunctions: []string{},
	}}
	inv.parents = []int{-1}
	inv.collect_types(ast.RootNode(), "")
	inv.walk(ast.RootNode(), 0)
	return inv
}

func (inv *inventory) collect_types(node *tree_sitter.Node, scope string) {
	if callable_kind(node) != "" {
		return
	}
	name := node_text(node.ChildByFieldName("name"), inv.source)
	switch node.Kind() {
	case "namespace_definition", "class_specifier", "struct_specifier", "union_specifier":
		if name != "" {
			scope += name + "::"
			inv.types[strings.TrimSuffix(scope, "::")] = node
		}
	case "type_spec":
		inv.types[name] = node
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		inv.collect_types(node.NamedChild(i), scope)
	}
}

func (inv *inventory) add(node, anchor *tree_sitter.Node, parent int, kind, name, origin, signature string) int {
	if node != nil {
		if index, exists := inv.nodes[node.Id()]; exists {
			return index
		}
	}
	part := NamePart{Kind: kind, Name: name, Signature: signature}
	key := inv.components[parent].QualifiedName + "::" + render_name([]NamePart{part})
	inv.ordinals[key]++
	if origin == "ordinal" || inv.ordinals[key] > 1 {
		part.Ordinal = inv.ordinals[key]
	}
	parts := append(append([]NamePart{}, inv.components[parent].NameParts...), part)
	index := len(inv.components)
	inv.components = append(inv.components, Component{
		QualifiedName: render_name(parts), ParentName: inv.components[parent].QualifiedName,
		Name: name, NameParts: parts, NameOrigin: origin, Kind: kind,
		Span: node_span(node), Declaration: node_span(anchor), InnerFunctions: []string{},
	})
	inv.parents = append(inv.parents, parent)
	if node != nil {
		inv.nodes[node.Id()] = index
	}
	return index
}

func (inv *inventory) walk(node *tree_sitter.Node, parent int) {
	if node.Kind() == "comment" || node.Kind() == "hash_bang_line" {
		inv.comments = append(inv.comments, node)
		for inv.components[parent].Kind == "object" {
			parent = inv.parents[parent]
		}
		inv.comment_owners = append(inv.comment_owners, parent)
		return
	}
	if node.Kind() == "preproc_def" || node.Kind() == "preproc_function_def" {
		return
	}
	if kind := container_kind(node); kind != "" {
		name := node_text(node.ChildByFieldName("name"), inv.source)
		origin := "symbol"
		if name == "" {
			name, _ = callable_binding(node, inv.source)
			origin = "binding"
		}
		if name == "" {
			name, origin = kind, "ordinal"
		}
		parent = inv.add(node, declaration_anchor(node), parent, kind, name, origin, "")
	} else if kind := callable_kind(node); kind != "" {
		name := node_text(node.ChildByFieldName("name"), inv.source)
		anchor, origin, signature := declaration_anchor(node), "symbol", ""
		if inv.language == "c" || inv.language == "cpp" {
			name = declarator_name(node.ChildByFieldName("declarator"), inv.source)
			if kind != "lambda" {
				signature = source_signature(node, inv.source)
			}
			if kind == "function" && inv.components[parent].Kind == "class" {
				kind = "method"
			}
			if strings.Contains(name, "::") {
				cut := strings.LastIndex(name, "::")
				parent = inv.resolve_owner(parent, name[:cut])
				name, kind = name[cut+2:], "method"
			}
		}
		if node.Kind() == "method_declaration" {
			receiver := node.ChildByFieldName("receiver")
			if receiver != nil {
				receiver = receiver.NamedChild(0)
			}
			if receiver != nil {
				receiver = receiver.ChildByFieldName("type")
			}
			for receiver != nil && (receiver.Kind() == "pointer_type" || receiver.Kind() == "generic_type") {
				if receiver.Kind() == "generic_type" {
					receiver = receiver.ChildByFieldName("type")
				} else {
					receiver = receiver.NamedChild(0)
				}
			}
			parent = inv.resolve_owner(0, node_text(receiver, inv.source))
		}
		binding, binding_anchor := callable_binding(node, inv.source)
		if binding != "" {
			anchor = binding_anchor
			if name == "" || node.Kind() == "function_expression" || node.Kind() == "generator_function" {
				name, origin = binding, "binding"
			}
		}
		if name == "" {
			name, origin = kind, "ordinal"
		}
		if node.Kind() == "method_definition" {
			for i := uint(0); i < node.ChildCount(); i++ {
				if child := node.Child(i); child.Kind() == "get" || child.Kind() == "set" {
					name = child.Kind() + " " + name
				}
			}
		}
		index := inv.add(node, anchor, parent, kind, name, origin, signature)
		inv.components[index].Body = node_span(node.ChildByFieldName("body"))
		inv.components[index].FunctionMetrics = &FunctionMetrics{
			Cyclomatic: Cyclomatic{Status: "unavailable"}, CommentStatus: "unavailable",
		}
		inv.components[parent].InnerFunctions = append(inv.components[parent].InnerFunctions, inv.components[index].QualifiedName)
		inv.units = append(inv.units, unit{node, anchor, index})
		parent = index
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		inv.walk(node.NamedChild(i), parent)
	}
}

func (inv *inventory) resolve_owner(parent int, name string) int {
	if name == "" {
		return parent
	}
	if strings.HasPrefix(name, "::") {
		parent = 0
		name = strings.TrimPrefix(name, "::")
	}
	var enclosing []string
	for _, part := range inv.components[parent].NameParts[2:] {
		enclosing = append(enclosing, part.Name)
	}
	key := strings.Join(append(enclosing, name), "::")
	if node := inv.types[key]; node != nil {
		return inv.ensure_type(node)
	}
	if node := inv.types[name]; node != nil {
		return inv.ensure_type(node)
	}
	for _, segment := range strings.Split(strings.TrimPrefix(name, "::"), "::") {
		var scope []string
		for _, part := range inv.components[parent].NameParts[2:] {
			scope = append(scope, part.Name)
		}
		key := strings.Join(append(scope, segment), "::")
		if node, exists := inv.types[key]; exists {
			parent = inv.add(node, declaration_anchor(node), parent, container_kind(node), segment, "symbol", "")
			continue
		}
		found := -1
		for i, component := range inv.components {
			if component.ParentName == inv.components[parent].QualifiedName && component.Name == segment &&
				component.NameOrigin == "unresolved_owner" {
				found = i
				break
			}
		}
		if found >= 0 {
			parent = found
		} else {
			parent = inv.add(nil, nil, parent, "scope", segment, "unresolved_owner", "")
		}
	}
	return parent
}

func (inv *inventory) ensure_type(node *tree_sitter.Node) int {
	if index, exists := inv.nodes[node.Id()]; exists {
		return index
	}
	parent := 0
	for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
		if container_kind(ancestor) != "" {
			parent = inv.ensure_type(ancestor)
			break
		}
	}
	name := node_text(node.ChildByFieldName("name"), inv.source)
	return inv.add(node, declaration_anchor(node), parent, container_kind(node), name, "symbol", "")
}

func container_kind(node *tree_sitter.Node) string {
	switch node.Kind() {
	case "class_declaration", "class", "class_specifier", "struct_specifier", "union_specifier":
		return "class"
	case "namespace_definition", "internal_module":
		return "namespace"
	case "object", "composite_literal":
		return "object"
	case "type_spec":
		return "type"
	}
	return ""
}

func callable_kind(node *tree_sitter.Node) string {
	if node.ChildByFieldName("body") == nil {
		return ""
	}
	switch node.Kind() {
	case "function_declaration", "function_definition", "function_expression", "generator_function_declaration", "generator_function":
		return "function"
	case "method_definition", "method_declaration":
		return "method"
	case "arrow_function":
		return "arrow_function"
	case "lambda_expression", "func_literal":
		return "lambda"
	}
	return ""
}

func validate_options(options Options) error {
	if options.Path == "" || options.Namespace == "" {
		return fmt.Errorf("metrics require a logical path and source namespace")
	}
	return nil
}

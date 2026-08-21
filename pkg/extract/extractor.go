// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package extract implements Phase 2: walks the AST and produces a SemanticDocument of semantic blocks.
package extract

import (
	"fmt"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"cmdiff/pkg/ir"
	"cmdiff/pkg/parser"
)

// Lifecycle hook names across React, Lit, Solid, Vue, detected during extraction.
var lifecycleHooks = map[string]bool{
	// React
	"componentDidMount":        true,
	"componentDidUpdate":       true,
	"componentWillUnmount":     true,
	"componentDidCatch":        true,
	"getDerivedStateFromProps": true,
	"getSnapshotBeforeUpdate":  true,
	// Lit
	"connectedCallback":    true,
	"disconnectedCallback": true,
	"updated":              true,
	"willUpdate":           true,
	"firstUpdated":         true,
	// Solid
	"createEffect":   true,
	"onCleanup":      true,
	"onMount":        true,
	"createSignal":   true,
	"createMemo":     true,
	"createComputed": true,
	// Vue
	"mounted":       true,
	"beforeMount":   true,
	"beforeUpdate":  true,
	"beforeUnmount": true,
	"unmounted":     true,
}

// Extractor walks a tree-sitter AST and produces a SemanticDocument.
type Extractor struct {
	filePath string
}

// New creates a new Extractor for the given file path.
func New(filePath string) *Extractor {
	return &Extractor{filePath: filePath}
}

// Extract walks the AST and produces a SemanticDocument with semantic blocks.
func (e *Extractor) Extract(ast *parser.AST) (*ir.SemanticDocument, error) {
	root := ast.RootNode()
	if root == nil {
		return nil, fmt.Errorf("AST root node is nil")
	}
	src := ast.Source()

	doc := &ir.SemanticDocument{
		Language:    ast.Language().Name,
		FilePath:    e.filePath,
		ParseErrors: ast.ErrorCount(),
	}

	e.walk(root, src, "", &doc.Blocks)

	return doc, nil
}

// walk recursively traverses the AST, extracting semantic blocks.
func (e *Extractor) walk(node *tree_sitter.Node, src []byte, parentID string, blocks *[]ir.SemanticBlock) {
	if node == nil {
		return
	}

	// Lit html templates: emit each top-level element as a parentless template block (like JSX);
	// the walk still descends into ${...} substitutions.
	if node.Kind() == "template_string" && isHtmlTaggedTemplate(node, src) {
		e.extractTemplateElements(node, src, blocks)
	}

	currentParent := parentID

	// Try to extract this node as a semantic block.
	if block, ok := e.tryExtract(node, src, parentID); ok {
		*blocks = append(*blocks, *block)
		currentParent = block.ID
		// An export-clause block owns its whole subtree; recursing would duplicate specifiers.
		if node.Kind() == "export_statement" && e.firstChild(node, "export_clause", src) != nil {
			return
		}
	}

	// Recurse so nested blocks (methods, JSX, arrow functions) are found.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		e.walk(child, src, currentParent, blocks)
	}
}

// exportedSource widens an `export`-wrapped declaration to the whole export statement
// (keeps the semantic report and the coverage supplement in sync).
func (e *Extractor) exportedSource(node *tree_sitter.Node, src []byte, text string, span ir.SourceSpan) (string, ir.SourceSpan) {
	p := node.Parent()
	if p != nil && p.Kind() == "export_statement" {
		return p.Utf8Text(src), e.makeSpan(p)
	}
	return text, span
}

// tryExtract attempts to create a SemanticBlock from a syntax node.
// Returns (block, true) if the node type is recognized, (nil, false) otherwise.
func (e *Extractor) tryExtract(node *tree_sitter.Node, src []byte, parentID string) (*ir.SemanticBlock, bool) {
	kind := node.Kind()
	text := node.Utf8Text(src)
	span := e.makeSpan(node)

	switch kind {
	case "import_statement":
		return e.extractImport(node, src, span), true

	case "function_declaration":
		name := e.childText(node, "identifier", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		text, span = e.exportedSource(node, src, text, span)
		return e.newBlock(ir.KindFunction, name, text, span, parentID), true

	case "method_definition":
		name := e.childText(node, "property_identifier", src)
		if name == "" {
			name = e.childText(node, "identifier", src)
		}
		kind := ir.KindMethod
		if parent := node.Parent(); parent != nil && parent.Kind() == "object" {
			kind = ir.KindObjectMethod
		}
		if lifecycleHooks[name] {
			kind = ir.KindLifecycle
		}
		return e.newBlock(kind, name, text, span, parentID), true

	case "class_declaration":
		name := e.childText(node, "type_identifier", src)
		if name == "" {
			name = e.childText(node, "identifier", src)
		}
		text, span = e.exportedSource(node, src, text, span)
		return e.newBlock(ir.KindClass, name, text, span, parentID), true

	case "interface_declaration":
		name := e.childText(node, "type_identifier", src)
		text, span = e.exportedSource(node, src, text, span)
		return e.newBlock(ir.KindInterface, name, text, span, parentID), true

	case "type_alias_statement":
		name := e.childText(node, "type_identifier", src)
		text, span = e.exportedSource(node, src, text, span)
		return e.newBlock(ir.KindTypeAlias, name, text, span, parentID), true

	case "enum_statement":
		name := e.childText(node, "type_identifier", src)
		text, span = e.exportedSource(node, src, text, span)
		return e.newBlock(ir.KindEnum, name, text, span, parentID), true

	case "lexical_declaration":
		// Don't create a block; the walk finds variable_declarator children.
		return nil, false

	case "variable_declarator":
		// Only top-level declarators are blocks; locals belong to their enclosing function.
		if e.isInsideFunction(node) {
			return nil, false
		}
		return e.extractVariableDeclarator(node, src, span, parentID), true

	case "arrow_function":
		// Skipped here; handled by extractVariableDeclarator.
		if parent := node.Parent(); parent != nil && parent.Kind() == "variable_declarator" {
			return nil, false
		}
		name := e.arrowFuncName(node, src)
		return e.newBlock(ir.KindArrowFunc, name, text, span, parentID), true

	case "function_expression":
		// Skip: the declarator block already represents the callable.
		if parent := node.Parent(); parent != nil && parent.Kind() == "variable_declarator" {
			return nil, false
		}
		name := e.childText(node, "identifier", src)
		if name == "" {
			name = "<anonymous>"
		}
		return e.newBlock(ir.KindFunction, name, text, span, parentID), true

	case "export_statement":
		return e.extractExportStatement(node, src, span, parentID)

	case "export_clause":
		// Don't create a block; let the walk find export_specifier children.
		return nil, false

	case "export_specifier":
		return e.extractExportSpecifier(node, src, span), true

	case "jsx_element", "jsx_self_closing_element", "jsx_fragment":
		return e.extractJSX(node, src, span), true

	case "comment":
		return e.extractComment(node, src, span), true

	// --- Go ---

	case "method_declaration":
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "field_identifier", src)
		}
		return e.newBlock(ir.KindMethod, name, text, span, parentID), true

	case "type_declaration":
		// Contains one or more type_spec children; recurse.
		return nil, false

	case "type_spec":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		kind := ir.KindTypeAlias
		if e.hasChildKind(node, "struct_type") {
			kind = ir.KindClass
		} else if e.hasChildKind(node, "interface_type") {
			kind = ir.KindInterface
		}
		// A sole spec covers the whole declaration (keyword included); grouped ones keep per-spec spans.
		text, span = e.typeDeclarationSource(node, src, span)
		return e.newBlock(kind, name, text, span, parentID), true

	case "type_alias":
		// tree-sitter-go models `type Alias = string` as its own node, not a type_spec.
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		text, span = e.typeDeclarationSource(node, src, span)
		return e.newBlock(ir.KindTypeAlias, name, text, span, parentID), true

	case "var_declaration", "const_declaration":
		// Contains var_spec/const_spec children; recurse.
		return nil, false

	case "var_spec":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.firstIdentifier(node, src)
		// A sole spec covers the complete declaration (keyword included).
		text, span = e.declarationSource(node, src, span)
		return e.newBlock(ir.KindVariable, name, text, span, parentID), true

	case "const_spec":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.firstIdentifier(node, src)
		// A sole spec covers the complete declaration (keyword included).
		text, span = e.declarationSource(node, src, span)
		return e.newBlock(ir.KindConstant, name, text, span, parentID), true

	case "short_var_declaration":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.firstIdentifier(node, src)
		return e.newBlock(ir.KindVariable, name, text, span, parentID), true

	case "import_declaration":
		var paths []string
		for i := uint(0); i < node.NamedChildCount(); i++ {
			c := node.NamedChild(i)
			if c != nil && c.Kind() == "import_spec" {
				p := e.fieldText(c, "path", src)
				if p == "" {
					p = c.Utf8Text(src)
				}
				paths = append(paths, p)
			}
		}
		name := strings.Join(paths, ", ")
		return e.newBlock(ir.KindImport, name, text, span, parentID), true

	case "func_literal":
		// Anonymous function literal; skip (handled by enclosing declaration).
		return nil, false

	// --- Bash ---

	case "function_definition":
		name := e.fieldText(node, "name", src)
		if name == "" {
			// Fallback: first named child that is a "word".
			for i := uint(0); i < node.NamedChildCount(); i++ {
				c := node.NamedChild(i)
				if c != nil && c.Kind() == "word" {
					name = c.Utf8Text(src)
					break
				}
			}
		}
		return e.newBlock(ir.KindFunction, name, text, span, parentID), true

	case "variable_assignment":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.firstIdentifier(node, src)
		}
		return e.newBlock(ir.KindVariable, name, text, span, parentID), true

	default:
		return nil, false
	}
}

// extractVariableDeclarator extracts a const/let/var declaration as a block.
func (e *Extractor) extractVariableDeclarator(node *tree_sitter.Node, src []byte, span ir.SourceSpan, parentID string) *ir.SemanticBlock {
	name := e.firstIdentifier(node, src)

	// Determine the value node (everything after =).
	var valueNode *tree_sitter.Node
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "=" {
			if i+1 < node.ChildCount() {
				valueNode = node.Child(i + 1)
			}
			break
		}
	}

	// Determine the declaration kind (const vs let vs var).
	declKind := "variable"
	if parent := node.Parent(); parent != nil {
		switch parent.Kind() {
		case "lexical_declaration":
			keyword := ""
			for i := uint(0); i < parent.ChildCount(); i++ {
				c := parent.Child(i)
				if c != nil && (c.Kind() == "const" || c.Kind() == "let") {
					keyword = c.Kind()
					break
				}
			}
			if keyword == "const" {
				declKind = "constant"
			}
		case "variable_statement":
			declKind = "variable"
		}
	}

	// An arrow/function value switches the block kind to match the value.
	blockKind := ir.KindConstant
	if declKind == "variable" {
		blockKind = ir.KindVariable
	}
	if valueNode != nil {
		switch valueNode.Kind() {
		case "arrow_function":
			blockKind = ir.KindArrowFunc
		case "function_expression":
			blockKind = ir.KindFunction
		}
	}

	// A sole declarator covers the complete declaration statement (keyword + terminator).
	source, span := e.declarationSource(node, src, span)

	return e.newBlock(blockKind, name, source, span, parentID)
}

// declarationSource: sole declarators cover the whole declaration (keyword, export
// prefix, terminator); multiple declarators keep per-declarator spans.
func (e *Extractor) declarationSource(node *tree_sitter.Node, src []byte, span ir.SourceSpan) (string, ir.SourceSpan) {
	parent := node.Parent()
	if parent == nil {
		return node.Utf8Text(src), span
	}
	switch parent.Kind() {
	case "lexical_declaration", "variable_statement":
		if !e.hasExactlyOneNamedChildOfKind(parent, "variable_declarator") {
			return node.Utf8Text(src), span
		}
	case "const_declaration", "var_declaration":
		if !e.hasExactlyOneNamedChildOfKind(parent, "const_spec") &&
			!e.hasExactlyOneNamedChildOfKind(parent, "var_spec") {
			return node.Utf8Text(src), span
		}
	default:
		return node.Utf8Text(src), span
	}

	// An `export`-wrapped declaration belongs to the export statement.
	base := parent
	if gp := parent.Parent(); gp != nil && gp.Kind() == "export_statement" {
		base = gp
	}
	return e.extendToTerminator(base, src, e.makeSpan(base))
}

// typeDeclarationSource: sole specs cover the whole declaration ("type" keyword
// included); grouped declarations keep per-spec spans.
func (e *Extractor) typeDeclarationSource(node *tree_sitter.Node, src []byte, span ir.SourceSpan) (string, ir.SourceSpan) {
	parent := node.Parent()
	if parent == nil || parent.Kind() != "type_declaration" {
		return node.Utf8Text(src), span
	}
	memberCount := 0
	for i := uint(0); i < parent.NamedChildCount(); i++ {
		child := parent.NamedChild(i)
		if child != nil && (child.Kind() == "type_spec" || child.Kind() == "type_alias") {
			memberCount++
		}
	}
	if memberCount == 1 {
		return e.extendToTerminator(parent, src, e.makeSpan(parent))
	}
	return node.Utf8Text(src), span
}

// extendToTerminator includes a following ";" and keeps line/column consistent with the byte span.
func (e *Extractor) extendToTerminator(base *tree_sitter.Node, src []byte, span ir.SourceSpan) (string, ir.SourceSpan) {
	if next := base.NextSibling(); next != nil && next.Kind() == ";" {
		span.EndByte = next.EndByte()
		end := next.EndPosition()
		span.EndLine = end.Row
		span.EndCol = end.Column
		return string(src[span.StartByte:span.EndByte]), span
	}
	return base.Utf8Text(src), span
}

// hasExactlyOneNamedChildOfKind reports whether the node has exactly one
// named child of the given kind.
func (e *Extractor) hasExactlyOneNamedChildOfKind(node *tree_sitter.Node, kind string) bool {
	count := 0
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		if c != nil && c.Kind() == kind {
			count++
		}
	}
	return count == 1
}

// extractImport extracts an import statement as a block.
func (e *Extractor) extractImport(node *tree_sitter.Node, src []byte, span ir.SourceSpan) *ir.SemanticBlock {
	name := e.extractImportName(node, src)
	source := node.Utf8Text(src)

	return e.newBlock(ir.KindImport, name, source, span, "")
}

// extractModulePath returns the module path string from an import statement.
func (e *Extractor) extractModulePath(node *tree_sitter.Node, src []byte) string {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Kind() == "string" {
			return e.stringValue(child, src)
		}
	}
	return ""
}

// extractImportName returns a best-effort name for an import.
func (e *Extractor) extractImportName(node *tree_sitter.Node, src []byte) string {
	// Check for default import: import Foo from 'mod'
	if clause := e.firstChild(node, "import_clause", src); clause != nil {
		// Named imports: import { foo, bar } from 'mod'
		if namedImports := e.firstChild(clause, "named_imports", src); namedImports != nil {
			var names []string
			for i := uint(0); i < namedImports.ChildCount(); i++ {
				c := namedImports.Child(i)
				if c == nil {
					continue
				}
				if c.Kind() == "import_specifier" {
					if ident := e.childText(c, "identifier", src); ident != "" {
						names = append(names, ident)
					}
				}
			}
			if len(names) > 0 {
				return strings.Join(names, ", ")
			}
		}

		// Namespace import: import * as Foo from 'mod'
		for i := uint(0); i < clause.ChildCount(); i++ {
			c := clause.Child(i)
			if c == nil {
				continue
			}
			if c.Kind() == "namespace_import" {
				if ident := e.childText(c, "identifier", src); ident != "" {
					return ident
				}
			}
		}

		// Default import: import Foo from 'mod'
		for i := uint(0); i < clause.ChildCount(); i++ {
			c := clause.Child(i)
			if c == nil {
				continue
			}
			if c.Kind() == "identifier" {
				return c.Utf8Text(src)
			}
		}
	}

	// Fallback: use module path as name
	return e.extractModulePath(node, src)
}

// extractExportStatement handles export declarations.
func (e *Extractor) extractExportStatement(node *tree_sitter.Node, src []byte, span ir.SourceSpan, parentID string) (*ir.SemanticBlock, bool) {
	// Check if this is an export_clause (re-export: export { foo, bar })
	if e.firstChild(node, "export_clause", src) != nil {
		name := strings.Join(e.exportSpecifierNames(node, src), ", ")
		source := node.Utf8Text(src)
		return e.newBlock(ir.KindExport, name, source, span, parentID), true
	}

	// Check if this is a re-export: export { foo } from 'mod'
	if e.firstChild(node, "from", src) != nil && e.firstChild(node, "string", src) != nil {
		name := e.extractModulePath(node, src)
		source := node.Utf8Text(src)
		return e.newBlock(ir.KindExport, name, source, span, parentID), true
	}

	// Wraps a declaration; don't create a block, let the walk find it.
	return nil, false
}

// exportSpecifierNames gathers names from export_specifiers nested under the export_clause.
func (e *Extractor) exportSpecifierNames(node *tree_sitter.Node, src []byte) []string {
	var names []string
	var visit func(*tree_sitter.Node)
	visit = func(current *tree_sitter.Node) {
		if current == nil {
			return
		}
		if current.Kind() == "export_specifier" {
			name := e.firstIdentifier(current, src)
			if name == "" {
				name = e.childText(current, "identifier", src)
			}
			if name != "" {
				names = append(names, name)
			}
			return
		}
		for i := uint(0); i < current.NamedChildCount(); i++ {
			visit(current.NamedChild(i))
		}
	}
	visit(node)
	return names
}

// extractExportSpecifier: individual specifiers (for grammars without an enclosing export block).
func (e *Extractor) extractExportSpecifier(node *tree_sitter.Node, src []byte, span ir.SourceSpan) *ir.SemanticBlock {
	name := e.childText(node, "identifier", src)
	source := node.Utf8Text(src)
	return e.newBlock(ir.KindExport, name, source, span, "")
}

// extractJSX extracts a JSX node as a block.
func (e *Extractor) extractJSX(node *tree_sitter.Node, src []byte, span ir.SourceSpan) *ir.SemanticBlock {
	name := e.jsxName(node, src)
	source := node.Utf8Text(src)
	return e.newBlock(ir.KindJSX, name, source, span, "")
}

// extractComment extracts a comment node.
func (e *Extractor) extractComment(node *tree_sitter.Node, src []byte, span ir.SourceSpan) *ir.SemanticBlock {
	text := node.Utf8Text(src)
	return e.newBlock(ir.KindComment, "", text, span, "")
}

// --- Helper functions ---

// newBlock creates a new SemanticBlock with a unique ID.
func (e *Extractor) newBlock(kind ir.BlockKind, name, source string, span ir.SourceSpan, parentID string) *ir.SemanticBlock {
	id := fmt.Sprintf("%s:%s:%d", kind, name, span.StartByte)
	return &ir.SemanticBlock{
		ID:     id,
		Kind:   kind,
		Name:   name,
		Parent: parentID,
		Span:   span,
		Source: source,
	}
}

// makeSpan creates a SourceSpan from a tree-sitter node.
func (e *Extractor) makeSpan(node *tree_sitter.Node) ir.SourceSpan {
	start := node.StartPosition()
	end := node.EndPosition()
	return ir.SourceSpan{
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		StartLine: start.Row,
		StartCol:  start.Column,
		EndLine:   end.Row,
		EndCol:    end.Column,
	}
}

// childText returns the source text of the first named child with the given kind.
func (e *Extractor) childText(node *tree_sitter.Node, kind string, src []byte) string {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == kind {
			return child.Utf8Text(src)
		}
	}
	return ""
}

// firstChild returns the first child with the given kind, or nil.
func (e *Extractor) firstChild(node *tree_sitter.Node, kind string, src []byte) *tree_sitter.Node {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

// fieldText returns the source text of the child selected by a tree-sitter
// field name (e.g. "name", "value"), or "" if absent.
func (e *Extractor) fieldText(node *tree_sitter.Node, fieldName string, src []byte) string {
	child := node.ChildByFieldName(fieldName)
	if child == nil {
		return ""
	}
	return child.Utf8Text(src)
}

// hasChildKind reports whether the node has a named child of the given kind.
func (e *Extractor) hasChildKind(node *tree_sitter.Node, kind string) bool {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		c := node.NamedChild(i)
		if c != nil && c.Kind() == kind {
			return true
		}
	}
	return false
}

// isInsideFunction: locals and inner declarations are not extracted (no duplication of the enclosing block).
func (e *Extractor) isInsideFunction(node *tree_sitter.Node) bool {
	p := node.Parent()
	for p != nil {
		switch p.Kind() {
		case "function_declaration", "method_declaration", "method_definition",
			"func_literal", "function_expression", "function_definition",
			"arrow_function":
			return true
		}
		p = p.Parent()
	}
	return false
}

// firstIdentifier returns the first identifier child's text.
func (e *Extractor) firstIdentifier(node *tree_sitter.Node, src []byte) string {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "identifier" || k == "property_identifier" || k == "type_identifier" {
			return child.Utf8Text(src)
		}
	}
	return ""
}

// stringValue extracts the value from a string literal node.
func (e *Extractor) stringValue(strNode *tree_sitter.Node, src []byte) string {
	for i := uint(0); i < strNode.ChildCount(); i++ {
		child := strNode.Child(i)
		if child != nil && child.Kind() == "string_fragment" {
			return child.Utf8Text(src)
		}
	}
	return strings.Trim(strNode.Utf8Text(src), "\"'")
}

// arrowFuncName takes the arrow function's name from its parent variable_declarator.
func (e *Extractor) arrowFuncName(node *tree_sitter.Node, src []byte) string {
	parent := node.Parent()
	if parent == nil {
		return ""
	}
	if parent.Kind() != "variable_declarator" {
		// Inside a return statement or assignment: no name.
		return ""
	}
	// The variable declarator's first identifier is the name.
	return e.firstIdentifier(parent, src)
}

// jsxName extracts the tag name from a JSX node.
func (e *Extractor) jsxName(node *tree_sitter.Node, src []byte) string {
	kind := node.Kind()
	switch kind {
	case "jsx_element":
		if opening := e.firstChild(node, "jsx_opening_element", src); opening != nil {
			return e.jsxTagName(opening, src)
		}
	case "jsx_self_closing_element":
		return e.jsxTagName(node, src)
	case "jsx_fragment":
		return "Fragment"
	}
	return "jsx"
}

// jsxTagName extracts the tag name from a jsx_opening_element or
// jsx_self_closing_element.
func (e *Extractor) jsxTagName(node *tree_sitter.Node, src []byte) string {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if k == "identifier" || k == "type_identifier" || k == "property_identifier" {
			return child.Utf8Text(src)
		}
		if k == "jsx_namespaced_name" {
			return child.Utf8Text(src)
		}
	}
	return "unknown"
}

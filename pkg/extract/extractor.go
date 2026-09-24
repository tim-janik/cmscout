// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Package extract implements Phase 2: walks the AST and produces a SemanticDocument of semantic blocks.
package extract

import (
	"fmt"
	"sort"
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

// Options configures language-dependent extraction behavior.
type Options struct {
	// SeparateMacroFunctions selects the C/C++ macro-function kind.
	SeparateMacroFunctions bool
}

// Extractor walks a tree-sitter AST and produces a SemanticDocument.
type Extractor struct {
	filePath string
	// SeparateMacroFunctions selects the macro-function kind when set.
	SeparateMacroFunctions bool

	// Class indexes resolve out-of-class C++ method parents.
	classIDs        map[string]string
	classIDsByName  map[string]string
	containerScopes map[string]string
	containerParent map[string]string
}

// New creates an Extractor with default options.
func New(filePath string) *Extractor {
	return &Extractor{filePath: filePath}
}

// NewWithOptions creates an Extractor with the given options.
func NewWithOptions(filePath string, opts Options) *Extractor {
	return &Extractor{filePath: filePath, SeparateMacroFunctions: opts.SeparateMacroFunctions}
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

	e.classIDs = make(map[string]string)
	e.classIDsByName = make(map[string]string)
	e.containerScopes = make(map[string]string)
	e.containerParent = make(map[string]string)
	e.indexCxxClasses(root, src, "", "")
	e.walk(root, src, "", &doc.Blocks)

	// Merge single-line "//" prefix runs right after the AST walk so they are never
	// double-booked as function prefix and standalone comment (see mergeSingleLinePrefixRuns).
	e.mergeSingleLinePrefixRuns(&doc.Blocks)

	// C/C++ components carry their enclosing namespace/class path in reports.
	if ast.Language().Name == "c" || ast.Language().Name == "cpp" {
		stampScope(doc.Blocks)
	}

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

	// Extract all direct C/C++ declarators before walking their children.
	special := false
	switch node.Kind() {
	case "declaration":
		special = true
		for _, block := range e.extractCDeclarationBlocks(node, src, parentID) {
			*blocks = append(*blocks, block)
		}
	case "type_definition":
		special = true
		aliases := e.extractCTypeDefinitionBlocks(node, src, parentID)
		for _, block := range aliases {
			*blocks = append(*blocks, block)
		}
		if len(aliases) > 0 {
			// Preserve the historical ownership of nested members in a typedef.
			currentParent = aliases[0].ID
		}
	}

	// Try to extract this node as a semantic block.
	if !special {
		if block, ok := e.tryExtract(node, src, parentID); ok {
			*blocks = append(*blocks, *block)
			currentParent = block.ID
			// An export-clause block owns its whole subtree; recursing would duplicate specifiers.
			if node.Kind() == "export_statement" && e.firstChild(node, "export_clause", src) != nil {
				return
			}
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
		// Function-local arrows stay inside their enclosing function.
		if e.isInsideFunction(node) {
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

	// --- Bash / C / C++ ---

	case "function_definition":
		name := e.fieldText(node, "name", src) // Bash: "name" field
		if name == "" {
			// C/C++ names come from the declarator chain.
			if d := node.ChildByFieldName("declarator"); d != nil {
				name = e.cDeclaratorName(d, src)
			}
		}
		if name == "" {
			// Bash fallback: first named child that is a "word".
			for i := uint(0); i < node.NamedChildCount(); i++ {
				c := node.NamedChild(i)
				if c != nil && c.Kind() == "word" {
					name = c.Utf8Text(src)
					break
				}
			}
		}
		// Local C++ types stay owned by their enclosing function.
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		parentID = e.parentOutsideFriend(node, parentID)
		blockKind := ir.KindFunction
		// Class methods may be inline, templated, or qualified.
		if e.isCxxClassMember(node) {
			blockKind = ir.KindMethod
		} else if scope, member, ok := e.cQualifiedMember(node, src); ok {
			if classID := e.classIDForScope(scope, parentID); classID != "" {
				blockKind = ir.KindMethod
				name = member
				parentID = classID
			}
		}
		text, span = e.templateWrappedSource(node, src, text, span)
		text, span = e.friendWrappedSource(node, src, text, span)
		return e.newBlock(blockKind, name, text, span, parentID), true

	case "variable_assignment":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.firstIdentifier(node, src)
		}
		return e.newBlock(ir.KindVariable, name, text, span, parentID), true

	// --- C ---

	case "struct_specifier", "union_specifier":
		// Local types stay inside their enclosing function.
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		// Bodyless specifiers are not semantic type components.
		if !e.cxxSpecifierHasBody(node) {
			return nil, false
		}
		// Typedefs own their nested type specifiers.
		if parent := node.Parent(); parent != nil && parent.Kind() == "type_definition" {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		text, span = e.templateWrappedSource(node, src, text, span)
		text, span = e.extendToTerminator(node, src, span)
		return e.newBlock(ir.KindClass, name, text, span, parentID), true

	case "enum_specifier":
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		if parent := node.Parent(); parent != nil && parent.Kind() == "type_definition" {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		text, span = e.templateWrappedSource(node, src, text, span)
		text, span = e.extendToTerminator(node, src, span)
		return e.newBlock(ir.KindEnum, name, text, span, parentID), true

	case "declaration":
		// Recurse into C/C++ declarations to extract their declarators.
		return nil, false

	case "init_declarator":
		if e.isInsideFunction(node) {
			return nil, false
		}
		value := node.ChildByFieldName("value")
		if node.Parent() != nil && node.Parent().Kind() == "field_declaration" &&
			(value == nil || value.Kind() != "lambda_expression") {
			// Keep ordinary data members inside their class.
			return nil, false
		}
		name := ""
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			name = e.cDeclaratorName(declarator, src)
		}
		if name == "" {
			name = e.firstIdentifier(node, src)
		}
		blockParentID := parentID
		if scope, member, ok := e.cQualifiedMember(declarator, src); ok {
			if classID := e.classIDForScope(scope, parentID); classID != "" {
				name = member
				blockParentID = classID
			}
		}
		blockKind := ir.KindVariable
		if e.declarationIsConst(node.Parent(), declarator, src) {
			blockKind = ir.KindConstant
		}
		// C++: auto f = [](int x){...}; switches to a lambda block.
		if value != nil && value.Kind() == "lambda_expression" {
			blockKind = ir.KindLambda
		}
		// Sole declarators own the complete declaration.
		source, span := e.declarationSource(node, src, span)
		return e.newBlock(blockKind, name, source, span, blockParentID), true

	case "preproc_include":
		name := e.fieldText(node, "path", src)
		if name == "" {
			name = text
		}
		text, span = e.trimPreprocessorTerminator(text, span, src)
		return e.newBlock(ir.KindImport, name, text, span, parentID), true

	case "preproc_def":
		name := e.fieldText(node, "name", src)
		text, span = e.trimPreprocessorTerminator(text, span, src)
		return e.newBlock(ir.KindConstant, name, text, span, parentID), true

	case "preproc_function_def":
		name := e.fieldText(node, "name", src)
		kind := ir.KindFunction
		if e.SeparateMacroFunctions {
			kind = ir.KindMacroFunction
		}
		text, span = e.trimPreprocessorTerminator(text, span, src)
		return e.newBlock(kind, name, text, span, parentID), true

	// --- C++ ---

	case "field_declaration":
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		if d := node.ChildByFieldName("declarator"); d != nil && e.cDeclaratorIsFunction(d) {
			name := e.cDeclaratorName(d, src)
			text, span = e.templateWrappedSource(node, src, text, span)
			return e.newBlock(ir.KindMethod, name, text, span, parentID), true
		}
		return nil, false

	case "class_specifier":
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		// Only body-bearing class specifiers become components.
		if !e.cxxSpecifierHasBody(node) {
			return nil, false
		}
		if parent := node.Parent(); parent != nil && parent.Kind() == "type_definition" {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		text, span = e.templateWrappedSource(node, src, text, span)
		text, span = e.extendToTerminator(node, src, span)
		return e.newBlock(ir.KindClass, name, text, span, parentID), true

	case "namespace_definition":
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		// Anonymous namespace has no name field → "".
		name := e.fieldText(node, "name", src)
		return e.newBlock(ir.KindNamespace, name, text, span, parentID), true

	case "concept_definition":
		if e.isInsideFunction(node) {
			return nil, false
		}
		name := e.fieldText(node, "name", src)
		text, span = e.templateWrappedSource(node, src, text, span)
		return e.newBlock(ir.KindConcept, name, text, span, parentID), true

	case "alias_declaration":
		if e.isInsideFunction(node) {
			return nil, false
		}
		// using X = ...;
		name := e.fieldText(node, "name", src)
		text, span = e.templateWrappedSource(node, src, text, span)
		return e.newBlock(ir.KindTypeAlias, name, text, span, parentID), true

	case "template_declaration":
		// Recurse through the template wrapper without duplicating it.
		return nil, false

	case "lambda_expression":
		if e.isInsideFunction(node) {
			// Function-local lambdas stay inside their enclosing function.
			return nil, false
		}
		if e.isInsideLocalCxxType(node) {
			return nil, false
		}
		// A lambda under an init_declarator is owned by the declarator block.
		if parent := node.Parent(); parent != nil && parent.Kind() == "init_declarator" {
			return nil, false
		} else if parent != nil && parent.Kind() == "field_declaration" {
			// C++ class fields put the lambda directly under field_declaration.
			name := e.firstIdentifier(parent, src)
			return e.newBlock(ir.KindLambda, name, parent.Utf8Text(src), e.makeSpan(parent), parentID), true
		}
		// Bare lambdas get anonymous ordinals during correlation.
		return e.newBlock(ir.KindLambda, "", text, span, parentID), true

	default:
		return nil, false
	}
}

// extractCDeclarationBlocks extracts direct file-scope declarators.
func (e *Extractor) extractCDeclarationBlocks(node *tree_sitter.Node, src []byte, parentID string) []ir.SemanticBlock {
	if e.isInsideFunction(node) {
		return nil
	}

	var blocks []ir.SemanticBlock
	declarators := e.cDeclarationDeclarators(node)
	declaratorCount := len(declarators)
	for _, declarator := range declarators {
		if declarator.Kind() == "init_declarator" {
			continue // the normal walk extracts and classifies this child
		}
		name := e.cDeclaratorName(declarator, src)
		if name == "" {
			name = e.firstIdentifier(declarator, src)
		}
		kind := ir.KindVariable
		blockParentID := e.parentOutsideFriend(node, parentID)
		if e.cDeclaratorIsFunction(declarator) {
			kind = ir.KindFunction
			if e.isCxxClassMember(node) {
				kind = ir.KindMethod
			}
			if scope, member, ok := e.cQualifiedMember(declarator, src); ok {
				if classID := e.classIDForScope(scope, blockParentID); classID != "" {
					kind = ir.KindMethod
					name = member
					blockParentID = classID
				}
			}
		} else {
			if scope, member, ok := e.cQualifiedMember(declarator, src); ok {
				if classID := e.classIDForScope(scope, blockParentID); classID != "" {
					name = member
					blockParentID = classID
				}
			}
			if e.declarationIsConst(node, declarator, src) {
				kind = ir.KindConstant
			}
		}
		text, span := e.declarationSource(declarator, src, e.makeSpan(declarator))
		// Include a template header only for a sole declarator.
		if declaratorCount == 1 {
			text, span = e.templateWrappedSource(node, src, text, span)
		}
		text, span = e.friendWrappedSource(node, src, text, span)
		blocks = append(blocks, *e.newBlock(kind, name, text, span, blockParentID))
	}
	return blocks
}

// extractCTypeDefinitionBlocks extracts each typedef declarator.
func (e *Extractor) extractCTypeDefinitionBlocks(node *tree_sitter.Node, src []byte, parentID string) []ir.SemanticBlock {
	if e.isInsideFunction(node) {
		return nil
	}

	declarators := e.cDeclarationDeclarators(node)
	if len(declarators) == 0 {
		// Keep malformed typedefs visible with a best-effort name.
		name := e.childText(node, "type_identifier", src)
		if name == "" && node.NamedChildCount() > 0 {
			name = node.NamedChild(node.NamedChildCount() - 1).Utf8Text(src)
		}
		return []ir.SemanticBlock{*e.newBlock(ir.KindTypeAlias, name, node.Utf8Text(src), e.makeSpan(node), parentID)}
	}

	blocks := make([]ir.SemanticBlock, 0, len(declarators))
	for _, declarator := range declarators {
		name := e.cDeclaratorName(declarator, src)
		if name == "" {
			// Primitive typedef names need a text fallback.
			name = declarator.Utf8Text(src)
		}
		text, span := declarator.Utf8Text(src), e.makeSpan(declarator)
		if len(declarators) == 1 {
			text, span = node.Utf8Text(src), e.makeSpan(node)
		}
		blocks = append(blocks, *e.newBlock(ir.KindTypeAlias, name, text, span, parentID))
	}
	return blocks
}

// cDeclarationDeclarators returns direct declarators in source order.
func (e *Extractor) cDeclarationDeclarators(node *tree_sitter.Node) []*tree_sitter.Node {
	first := node.ChildByFieldName("declarator")
	if first == nil {
		return nil
	}

	var declarators []*tree_sitter.Node
	foundFirst := false
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil || child.StartByte() < first.StartByte() {
			continue
		}
		if child.StartByte() == first.StartByte() && child.EndByte() == first.EndByte() {
			declarators = append(declarators, child)
			foundFirst = true
			continue
		}
		if e.isCDeclaratorNode(child) {
			declarators = append(declarators, child)
		}
	}
	if !foundFirst {
		declarators = append([]*tree_sitter.Node{first}, declarators...)
	}
	return declarators
}

// isCDeclaratorNode identifies direct C/C++ declarator shapes.
func (e *Extractor) isCDeclaratorNode(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "type_identifier", "field_identifier", "namespace_identifier",
		"qualified_identifier", "operator_name", "operator_cast", "template_function",
		"pointer_declarator", "array_declarator", "function_declarator",
		"parenthesized_declarator", "attributed_declarator", "reference_declarator",
		"structured_binding_declarator", "init_declarator":
		return true
	default:
		return false
	}
}

// cDeclaratorIsFunction distinguishes function prototypes from pointer variables.
func (e *Extractor) cDeclaratorIsFunction(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "operator_cast":
		return true
	case "function_declarator":
		inner := node.ChildByFieldName("declarator")
		return e.cFunctionNameRoot(inner)
	case "pointer_declarator", "reference_declarator", "attributed_declarator":
		return e.cDeclaratorIsFunction(e.cUnderlyingDeclarator(node))
	default:
		return false
	}
}

// cFunctionNameRoot distinguishes functions from pointer objects.
func (e *Extractor) cFunctionNameRoot(node *tree_sitter.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind() {
	case "identifier", "type_identifier", "field_identifier", "namespace_identifier",
		"qualified_identifier", "operator_name", "operator_cast", "template_function":
		return true
	case "attributed_declarator":
		return e.cFunctionNameRoot(e.cUnderlyingDeclarator(node))
	default:
		return false
	}
}

// cUnderlyingDeclarator finds a wrapper's nested declarator.
func (e *Extractor) cUnderlyingDeclarator(node *tree_sitter.Node) *tree_sitter.Node {
	if node == nil {
		return nil
	}
	if d := node.ChildByFieldName("declarator"); d != nil {
		return d
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if e.isCDeclaratorNode(child) {
			return child
		}
	}
	return nil
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
	case "field_declaration":
		if e.hasExactlyOneNamedChildOfKind(parent, "init_declarator") {
			return parent.Utf8Text(src), e.makeSpan(parent)
		}
		return node.Utf8Text(src), span
	case "declaration":
		// Keep an object after an inline type specifier non-overlapping.
		if len(e.cDeclarationDeclarators(parent)) != 1 {
			return node.Utf8Text(src), span
		}
		if e.hasCInlineTypeSpecifier(parent) {
			return e.cDeclaratorWithTerminator(node, src, span)
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

// hasCInlineTypeSpecifier reports inline type definitions.
func (e *Extractor) hasCInlineTypeSpecifier(node *tree_sitter.Node) bool {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "struct_specifier", "union_specifier", "enum_specifier", "class_specifier":
			return true
		}
	}
	return false
}

// cDeclaratorWithTerminator retains a declarator's semicolon.
func (e *Extractor) cDeclaratorWithTerminator(node *tree_sitter.Node, src []byte, span ir.SourceSpan) (string, ir.SourceSpan) {
	parent := node.Parent()
	if parent == nil {
		return node.Utf8Text(src), span
	}
	for i := uint(0); i < parent.ChildCount(); i++ {
		child := parent.Child(i)
		if child == nil || child.Kind() != ";" || child.StartByte() < node.EndByte() {
			continue
		}
		span.EndByte = child.EndByte()
		end := child.EndPosition()
		span.EndLine = end.Row
		span.EndCol = end.Column
		return string(src[span.StartByte:span.EndByte]), span
	}
	return node.Utf8Text(src), span
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

// trimPreprocessorTerminator removes a directive's trailing line ending.
func (e *Extractor) trimPreprocessorTerminator(text string, span ir.SourceSpan, src []byte) (string, ir.SourceSpan) {
	start, end := int(span.StartByte), int(span.EndByte)
	trimmedEnd := end
	for trimmedEnd > start && (src[trimmedEnd-1] == '\n' || src[trimmedEnd-1] == '\r') {
		trimmedEnd--
	}
	if trimmedEnd == end {
		return text, span
	}

	line, col := span.StartLine, span.StartCol
	for i := start; i < trimmedEnd; i++ {
		if src[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	span.EndByte = uint(trimmedEnd)
	span.EndLine = line
	span.EndCol = col
	return string(src[start:trimmedEnd]), span
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

// stampScope records enclosing C/C++ namespace and class names.
func stampScope(blocks []ir.SemanticBlock) {
	byID := make(map[string]*ir.SemanticBlock, len(blocks))
	for i := range blocks {
		byID[blocks[i].ID] = &blocks[i]
	}
	for i := range blocks {
		blocks[i].Scope = scopePath(&blocks[i], byID)
	}
}

// scopePath returns enclosing namespace and class names.
func scopePath(b *ir.SemanticBlock, byID map[string]*ir.SemanticBlock) string {
	var parts []string
	for cur := b; ; {
		if cur.Parent == "" {
			break
		}
		parent, ok := byID[cur.Parent]
		if !ok || parent == cur {
			break
		}
		if (parent.Kind == ir.KindNamespace || parent.Kind == ir.KindClass) && parent.Name != "" {
			parts = append([]string{parent.Name}, parts...)
		}
		cur = parent
	}
	return strings.Join(parts, "::")
}

// indexCxxClasses records qualified class paths before extraction.
func (e *Extractor) indexCxxClasses(node *tree_sitter.Node, src []byte, scope, parentID string) {
	if node == nil {
		return
	}

	nextScope, nextParentID := scope, parentID
	switch node.Kind() {
	case "namespace_definition":
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "namespace_identifier", src)
		}
		nextScope = joinCxxScope(scope, name)
		span := e.makeSpan(node)
		nextParentID = fmt.Sprintf("%s:%s:%d", ir.KindNamespace, name, span.StartByte)
		e.containerScopes[nextParentID] = nextScope
		e.containerParent[nextParentID] = parentID

	case "class_specifier", "struct_specifier", "union_specifier":
		name := e.fieldText(node, "name", src)
		if name == "" {
			name = e.childText(node, "type_identifier", src)
		}
		if name != "" && e.cxxSpecifierHasBody(node) {
			if parent := node.Parent(); parent == nil || parent.Kind() != "type_definition" {
				nextScope = joinCxxScope(scope, name)
				_, span := e.templateWrappedSource(node, src, node.Utf8Text(src), e.makeSpan(node))
				nextParentID = fmt.Sprintf("%s:%s:%d", ir.KindClass, name, span.StartByte)
				e.containerScopes[nextParentID] = nextScope
				e.containerParent[nextParentID] = parentID
				if _, exists := e.classIDs[nextScope]; !exists {
					e.classIDs[nextScope] = nextParentID
				}
				if previous, exists := e.classIDsByName[name]; exists && previous != nextParentID {
					e.classIDsByName[name] = ""
				} else {
					e.classIDsByName[name] = nextParentID
				}
			}
		}
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		e.indexCxxClasses(node.Child(i), src, nextScope, nextParentID)
	}
}

func joinCxxScope(parent, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return parent
	}
	if parent == "" {
		return name
	}
	return parent + "::" + name
}

// templateWrappedSource includes a declaration's template header.
func (e *Extractor) templateWrappedSource(node *tree_sitter.Node, src []byte, text string, span ir.SourceSpan) (string, ir.SourceSpan) {
	if parent := node.Parent(); parent != nil && parent.Kind() == "template_declaration" {
		return parent.Utf8Text(src), e.makeSpan(parent)
	}
	return text, span
}

// isCxxTypeContainerKind identifies C++ scope containers.
func isCxxTypeContainerKind(kind string) bool {
	switch kind {
	case "class_specifier", "struct_specifier", "union_specifier", "enum_specifier", "namespace_definition":
		return true
	default:
		return false
	}
}

// isInsideLocalCxxType reports whether a node belongs to a local type.
func (e *Extractor) isInsideLocalCxxType(node *tree_sitter.Node) bool {
	for p := node; p != nil; p = p.Parent() {
		if !isCxxTypeContainerKind(p.Kind()) {
			continue
		}
		for ancestor := p.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
			switch ancestor.Kind() {
			case "function_declaration", "method_declaration", "method_definition",
				"func_literal", "function_expression", "function_definition",
				"arrow_function", "lambda_expression":
				return true
			}
		}
	}
	return false
}

// cxxSpecifierHasBody reports whether a type specifier has a body.
func (e *Extractor) cxxSpecifierHasBody(node *tree_sitter.Node) bool {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if c := node.NamedChild(i); c != nil && c.Kind() == "field_declaration_list" {
			return true
		}
	}
	return false
}

// isCxxClassMember recognizes inline members and excludes friends.
func (e *Extractor) isCxxClassMember(node *tree_sitter.Node) bool {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "friend_declaration" {
			return false
		}
		switch p.Kind() {
		case "class_specifier", "struct_specifier", "union_specifier":
			return true
		case "function_definition", "lambda_expression":
			return false
		}
	}
	return false
}

func (e *Extractor) parentOutsideFriend(node *tree_sitter.Node, parentID string) string {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != "friend_declaration" {
			continue
		}
		if outer, ok := e.containerParent[parentID]; ok {
			return outer
		}
		return ""
	}
	return parentID
}

func (e *Extractor) friendWrappedSource(node *tree_sitter.Node, src []byte, text string, span ir.SourceSpan) (string, ir.SourceSpan) {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "friend_declaration" {
			return p.Utf8Text(src), e.makeSpan(p)
		}
	}
	return text, span
}

// cQualifiedMember splits a qualified member declarator.
func (e *Extractor) cQualifiedMember(node *tree_sitter.Node, src []byte) (string, string, bool) {
	if node == nil {
		return "", "", false
	}
	if node.Kind() == "qualified_identifier" {
		qualified := strings.TrimSpace(node.Utf8Text(src))
		cut := strings.LastIndex(qualified, "::")
		if cut <= 0 || cut+2 >= len(qualified) {
			return "", "", false
		}
		return qualified[:cut], normalizeCxxMemberName(qualified[cut+2:]), true
	}
	var child *tree_sitter.Node
	if d := node.ChildByFieldName("declarator"); d != nil {
		child = d
	} else {
		child = e.cUnderlyingDeclarator(node)
	}
	return e.cQualifiedMember(child, src)
}

// classIDForScope resolves a qualified class scope to an extracted class.
func (e *Extractor) classIDForScope(scope, parentID string) string {
	scope = normalizeCxxScope(scope)
	if scope == "" {
		return ""
	}
	if !strings.Contains(scope, "::") {
		if parentScope := e.containerScopes[parentID]; parentScope != "" {
			if id := e.classIDs[joinCxxScope(parentScope, scope)]; id != "" {
				return id
			}
		}
	}
	if id := e.classIDs[scope]; id != "" {
		return id
	}
	if i := strings.LastIndex(scope, "::"); i >= 0 {
		scope = scope[i+2:]
	}
	return e.classIDsByName[scope]
}

func normalizeCxxMemberName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "operator ") {
		if i := strings.IndexByte(name, '('); i >= 0 {
			return strings.TrimSpace(name[:i])
		}
	}
	return name
}

func normalizeCxxScope(scope string) string {
	var parts []string
	for _, part := range splitCxxScope(scope) {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, '<'); i >= 0 {
			part = part[:i]
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "::")
}

func splitCxxScope(scope string) []string {
	scope = strings.TrimSpace(scope)
	var parts []string
	start, depth := 0, 0
	for i := 0; i < len(scope); i++ {
		switch scope[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 && i+1 < len(scope) && scope[i+1] == ':' {
				parts = append(parts, scope[start:i])
				i++
				start = i + 1
			}
		}
	}
	parts = append(parts, scope[start:])
	return parts
}

// isInsideFunction: locals and inner declarations are not extracted (no duplication of the enclosing block).
func (e *Extractor) isInsideFunction(node *tree_sitter.Node) bool {
	p := node.Parent()
	for p != nil {
		switch p.Kind() {
		case "function_declaration", "method_declaration", "method_definition",
			"func_literal", "function_expression", "function_definition",
			"arrow_function", "lambda_expression":
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
		if k == "identifier" || k == "property_identifier" || k == "type_identifier" || k == "field_identifier" {
			return child.Utf8Text(src)
		}
	}
	return ""
}

// cDeclaratorName resolves names through C/C++ declarator wrappers.
func (e *Extractor) cDeclaratorName(node *tree_sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	switch node.Kind() {
	case "identifier", "type_identifier", "field_identifier", "namespace_identifier":
		return node.Utf8Text(src)
	case "destructor_name", "qualified_identifier", "operator_name", "template_function":
		// Keep the full token ("~Foo", "Foo::bar", "operator=", "f<int>") as the name.
		return node.Utf8Text(src)
	case "operator_cast":
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			return "operator " + typeNode.Utf8Text(src)
		}
		return node.Utf8Text(src)
	case "function_declarator", "pointer_declarator", "array_declarator":
		if d := node.ChildByFieldName("declarator"); d != nil {
			return e.cDeclaratorName(d, src)
		}
		return ""
	case "parenthesized_declarator", "attributed_declarator", "reference_declarator":
		// Recurse through named declarator children and skip qualifiers.
		for i := uint(0); i < node.NamedChildCount(); i++ {
			c := node.NamedChild(i)
			if c == nil {
				continue
			}
			if cDeclaratorContainerKinds[c.Kind()] || cDeclaratorTerminalKinds[c.Kind()] {
				return e.cDeclaratorName(c, src)
			}
		}
		return ""
	}
	return ""
}

// cDeclaratorContainerKinds are the recursive declarator wrappers descended by cDeclaratorName.
var cDeclaratorContainerKinds = map[string]bool{
	"function_declarator":      true,
	"pointer_declarator":       true,
	"array_declarator":         true,
	"parenthesized_declarator": true,
	"attributed_declarator":    true,
	"reference_declarator":     true,
}

// cDeclaratorTerminalKinds are the name-bearing terminals reached by cDeclaratorName.
var cDeclaratorTerminalKinds = map[string]bool{
	"identifier":           true,
	"type_identifier":      true,
	"field_identifier":     true,
	"namespace_identifier": true,
	"destructor_name":      true,
	"qualified_identifier": true,
	"operator_name":        true,
	"template_function":    true,
	"operator_cast":        true,
}

// declarationIsConst distinguishes base-type and object qualifiers.
func (e *Extractor) declarationIsConst(declaration, declarator *tree_sitter.Node, src []byte) bool {
	if declaration == nil || declaration.Kind() != "declaration" {
		return false
	}
	baseConst := false
	for i := uint(0); i < declaration.NamedChildCount(); i++ {
		child := declaration.NamedChild(i)
		if child == nil || child.Kind() != "type_qualifier" {
			continue
		}
		qualifier := child.Utf8Text(src)
		if qualifier == "const" || qualifier == "constexpr" {
			baseConst = true
			break
		}
	}
	return e.cObjectIsConst(declarator, baseConst, src)
}

// cObjectIsConst applies qualifiers to the declared object.
func (e *Extractor) cObjectIsConst(declarator *tree_sitter.Node, baseConst bool, src []byte) bool {
	if declarator == nil {
		return baseConst
	}
	switch declarator.Kind() {
	case "pointer_declarator", "reference_declarator":
		return e.declaratorHasConstQualifier(declarator, src)
	case "parenthesized_declarator", "attributed_declarator":
		return e.cObjectIsConst(e.cUnderlyingDeclarator(declarator), baseConst, src)
	case "function_declarator":
		// A parenthesized pointer under a function declarator is an object.
		inner := declarator.ChildByFieldName("declarator")
		if inner != nil && (inner.Kind() == "parenthesized_declarator" || inner.Kind() == "attributed_declarator") {
			return e.cObjectIsConst(inner, baseConst, src)
		}
		return false
	case "array_declarator":
		return baseConst || e.declaratorHasConstQualifier(declarator, src)
	default:
		return baseConst
	}
}

// declaratorHasConstQualifier checks direct declarator qualifiers.
func (e *Extractor) declaratorHasConstQualifier(declarator *tree_sitter.Node, src []byte) bool {
	for i := uint(0); i < declarator.NamedChildCount(); i++ {
		child := declarator.NamedChild(i)
		if child == nil || child.Kind() != "type_qualifier" {
			continue
		}
		qualifier := child.Utf8Text(src)
		if qualifier == "const" || qualifier == "constexpr" {
			return true
		}
	}
	return false
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

// mergeSingleLinePrefixRuns merges consecutive "//" single-line comments that directly prefix a
// component into one block, right after the AST walk. Why: [../../doc/prefix-comments.md](prefix-comments.md).
func (e *Extractor) mergeSingleLinePrefixRuns(blocks *[]ir.SemanticBlock) {
	if blocks == nil || len(*blocks) == 0 {
		return
	}
	// Sort pointers by source offset for prefix detection.
	sorted := make([]*ir.SemanticBlock, len(*blocks))
	for i := range *blocks {
		sorted[i] = &(*blocks)[i]
	}
	sort.Slice(sorted, func(a, b int) bool {
		if sorted[a].Span.StartByte != sorted[b].Span.StartByte {
			return sorted[a].Span.StartByte < sorted[b].Span.StartByte
		}
		return sorted[a].ID < sorted[b].ID
	})
	isSingleLineComment := func(b *ir.SemanticBlock) bool {
		if b == nil || b.Kind != ir.KindComment {
			return false
		}
		if b.Span.StartLine != b.Span.EndLine {
			return false
		}
		trimmed := strings.TrimLeft(b.Source, " \t")
		if !strings.HasPrefix(trimmed, "//") {
			return false
		}
		// Ensure single-line source (tree-sitter "//" comments are single line,
		// but guard against any multi-line "/*" that happens to be single line).
		if strings.Contains(b.Source, "\n") {
			return false
		}
		return true
	}
	// Identify prefix candidates: comment directly before a non-comment with no blank line.
	var candidateIdxs []int
	for i := 0; i+1 < len(sorted); i++ {
		cur := sorted[i]
		next := sorted[i+1]
		if cur.Kind != ir.KindComment || next.Kind == ir.KindComment {
			continue
		}
		if cur.Span.EndLine+1 != next.Span.StartLine {
			continue
		}
		if !isSingleLineComment(cur) {
			continue
		}
		candidateIdxs = append(candidateIdxs, i)
	}
	toRemove := make(map[*ir.SemanticBlock]bool)
	for _, idx := range candidateIdxs {
		target := sorted[idx]
		if toRemove[target] {
			continue
		}
		curIdx := idx
		curStartLine := target.Span.StartLine
		for curIdx > 0 {
			prevIdx := curIdx - 1
			prev := sorted[prevIdx]
			if toRemove[prev] {
				break
			}
			if prev.Kind != ir.KindComment {
				break
			}
			if !isSingleLineComment(prev) {
				break
			}
			if prev.Span.EndLine+1 != curStartLine {
				break
			}
			// Prepend prev into target.
			target.Source = prev.Source + "\n" + target.Source
			target.Span.StartByte = prev.Span.StartByte
			target.Span.StartLine = prev.Span.StartLine
			target.Span.StartCol = prev.Span.StartCol
			// ID intentionally not updated (matches correlate.AttachPrefixComments behavior).
			toRemove[prev] = true
			curIdx = prevIdx
			curStartLine = prev.Span.StartLine
		}
	}
	if len(toRemove) == 0 {
		return
	}
	filtered := (*blocks)[:0]
	for i := range *blocks {
		if toRemove[&(*blocks)[i]] {
			continue
		}
		filtered = append(filtered, (*blocks)[i])
	}
	*blocks = filtered
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

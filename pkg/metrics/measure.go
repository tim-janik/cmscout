package metrics

import (
	"crypto/sha256"
	"fmt"

	"cmscout/pkg/parser"
)

func Measure(ast *parser.AST, options Options) (*Snapshot, error) {
	if err := validate_options(options); err != nil {
		return nil, err
	}
	if ast == nil || ast.RootNode() == nil {
		return nil, fmt.Errorf("metrics require an open syntax tree")
	}
	index := index_source(ast.Source())
	inv := discover(ast, options)
	inv.components[0].Span = index.span(0, uint(len(ast.Source())))
	for i := range inv.components {
		inv.components[i].Size = index.size(inv.components[i].Span)
	}
	digest := sha256.Sum256(ast.Source())
	identity := sha256.New()
	fmt.Fprintf(identity, "%s\x00%s\x00%s\x00", options.Namespace, options.Path, ast.Language().Name)
	identity.Write(ast.Source())
	snapshot := &Snapshot{
		SchemaVersion: "1", NamingVersion: "source-v1", Profiles: []string{"cyclomatic/source-v1", "size-comments/source-v1"},
		SnapshotID: fmt.Sprintf("%x", identity.Sum(nil)), ContentDigest: fmt.Sprintf("%x", digest),
		Namespace: options.Namespace, Path: options.Path, Language: ast.Language().Name,
		Population: "functions", Coordinates: "zero-based lines and byte columns; half-open byte spans",
		Status: "complete", Diagnostics: []Diagnostic{}, Components: inv.components,
		source: append([]byte{}, ast.Source()...), tokens: source_tokens(ast.RootNode(), ast.Source()),
		Limitations: []string{
			"Source decisions, without reachability analysis or compiler type checking.",
			"Function population only; file and class initialization totals are not measured.",
		},
	}
	if inv.language == "c" || inv.language == "cpp" {
		snapshot.Limitations = append(snapshot.Limitations,
			"All written preprocessor alternatives are scanned; macros are not expanded.",
			"Ordinary unevaluated operands are excluded; dynamic C array bounds are unsupported.")
	}
	if inv.language == "bash" {
		snapshot.Limitations = append(snapshot.Limitations,
			"Legacy test -a/-o arguments, implicit set -e exits, traps and eval text are not modeled.",
			"Case terminators add no decisions; a final unquoted wildcard case adds none.")
	}
	for _, diagnostic := range ast.Diagnostics() {
		snapshot.Diagnostics = append(snapshot.Diagnostics, Diagnostic{
			Kind: "parse_" + diagnostic.Kind, Message: diagnostic.Token,
			Span: index.span(diagnostic.StartByte, diagnostic.EndByte),
		})
	}
	if len(index.invalid) > 0 {
		offset := index.invalid[0]
		snapshot.Diagnostics = append(snapshot.Diagnostics, Diagnostic{"invalid_utf8", "source is not valid UTF-8", index.span(offset, offset+1)})
	}
	if len(snapshot.Diagnostics) > 0 {
		snapshot.Status = "partial"
		return snapshot, nil
	}
	inv.count_complexity(ast.RootNode())
	comments, diagnostics := inv.measure_comments(index)
	snapshot.Comments = comments
	snapshot.Diagnostics = append(snapshot.Diagnostics, diagnostics...)
	for _, unit := range inv.units {
		component := &inv.components[unit.index]
		if component.Cyclomatic.Status != "complete" {
			snapshot.Diagnostics = append(snapshot.Diagnostics, Diagnostic{
				"unsupported", "C array bounds need type information to rule out variable-length evaluation", component.Span,
			})
		}
		if !options.Explain {
			component.Cyclomatic.Decisions = nil
		}
	}
	if len(snapshot.Diagnostics) > 0 {
		snapshot.Status = "partial"
	}
	return snapshot, nil
}

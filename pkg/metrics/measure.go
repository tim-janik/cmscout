// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

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
		SchemaVersion: "1", NamingVersion: "source-v1", Profiles: []string{"cyclomatic-v2", "size-comments-v1"},
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
			"Unevaluated operands and C array length expressions contribute zero decisions, including VLA bounds.")
	}
	if inv.language == "bash" {
		snapshot.Limitations = append(snapshot.Limitations,
			"Implicit set -e exits, traps and eval text are not modeled.",
			"Case alternatives count up to an unquoted wildcard; case terminators add no decisions.")
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
	snapshot.Comments = inv.measure_comments(index)
	for _, unit := range inv.units {
		component := &inv.components[unit.index]
		if !options.Explain {
			component.Cyclomatic.Decisions = nil
		}
	}
	return snapshot, nil
}

<!-- This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0 -->

# cmscout

## Where code goes

- `cmd/cmscout/` owns flags, inputs and pipeline wiring.
- `pkg/lang/` detects languages. `pkg/parser/` owns tree-sitter; close each parser and AST.
- `pkg/extract/` extracts blocks and original source spans. `pkg/ir/` holds shared internal types.
- `pkg/matching/` chooses pairs. Keep staged, one-to-one, scope-aware matching and deterministic maximum-weight assignment.
- `pkg/correlate/` collapses children and attaches comments. Finish source rewrites before computing InnerDiff.
- `pkg/diff/` computes line and word diffs. `pkg/report/` renders them and counts the summary.

## Output guarantees

- Read `doc/pipeline.md` and `doc/test-invariants.md` before changing the pipeline.
- Full line-mode output retains every non-empty input line, per side and occurrence. Repeated lines survive.
- Word diffs retain enough information to reconstruct both sides. Summary and skip-unchanged modes intentionally omit content.
- The Other supplement fills gaps without repeating semantic output. Added and removed blocks retain their real source.
- Canonical references identify matched children only. Header percentages describe the final shown text.
- The same inputs produce the same output. Keep scope ownership when pairing duplicate names or rescuing moved callables.
- Use whole-file fallback if either language is unsupported or both sides have no blocks. Keep parse-error reporting.
- Inputs have no size cap. Preserve the complete matching calculation when improving speed.

## Changes

- Use gofmt and nearby Go naming. New source files carry the project's MPL-2.0 header.
- Keep changes focused. Add a comment of at most one line when the intent needs explaining.
- Every behavior change needs a regression test. Use package tests; add a CLI case for changes across stages.
- Preserve tests for duplicate names, nesting, comments, malformed input and whitespace. Fixtures may contain deliberate oddities.
- Before removing or rewriting unusual code, read its history and reproduce the problem it handles.
- Logic removals need a nonempty `Evidence:` commit paragraph explaining the original purpose and why it is no longer needed, with commit ids or links.
- Never delete behavior or tests to satisfy a check. Document and test a narrow exception when a rule conflicts with required behavior.
- Commit changes with a file-path subject and a body explaining why and how. Omit Co-Authored-By and Signed-off-by trailers.

## Checks

- Use the Go version declared in `go.mod`, a C compiler and CGO_ENABLED=1.
- `make` builds. `make check` runs Go tests, lint, guidance checks, offline tooling tests and the committed-removal check.
- `make lint` checks formatting, vet, shell errors, Python syntax and workflows. ShellCheck warnings need review before becoming mandatory.
- `make check-agents-md` verifies the paths and make targets named in backticks here. It does not validate source declarations.
- `make check-removals` checks non-merge commits against origin/trunk, or trunk if the remote ref is unavailable.
- Set CHECK_BASE and CHECK_HEAD to select a committed range. At ten net code removals, counted per file, the message needs evidence.
- The removal count excludes tests, fixtures and generated source. A line count cannot judge whether the evidence is sound.
- `make distcheck` builds and tests committed HEAD through its source archive. Run working-tree checks before committing too.
- Keep the shared release and IRC scripts in sync with the upstream named in `doc/releases.md`.
- Project-specific release behavior belongs in `Makefile` and workflows. Tests mock external services and run independently of user configuration.

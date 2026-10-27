<!-- This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0 -->

# cmscout

## Output guarantees

- Read `doc/pipeline.md` and `doc/test-invariants.md` before changing the pipeline.
- Full line-mode output retains every non-empty input line, per side and occurrence. Repeated lines survive.
- Word diffs retain enough information to reconstruct both sides. Summary and skip-unchanged modes intentionally omit content.
- The Other supplement fills gaps without repeating semantic output. Added and removed blocks retain their real source.
- Canonical references identify matched children only. Header percentages describe the final shown text.
- The same inputs produce the same output. Keep scope ownership when pairing duplicate names or rescuing moved callables.
- Keep stdout for requested reports. Send errors and diagnostics to stderr.
- Use whole-file fallback if either language is unsupported or both sides have no blocks. Keep parse-error reporting.
- Inputs have no size cap. Preserve the complete matching calculation when improving speed.

## Changes

- Use gofmt and nearby Go naming. Prefer snake_case where possible and the Go tools don't warn about it. New source files carry the project's MPL-2.0 header.
- Files with `.g.` in their names are generated. Edit their generator, not the generated file.
- Keep changes focused. Add a comment of at most one line when the intent needs explaining.
- Every behavior change needs a regression test. Use package tests; add a CLI case for changes across stages. Test behavior and invariants, not properties the Go type system already guarantees.
- Preserve tests for duplicate names, nesting, comments, malformed input and whitespace. Fixtures may contain deliberate oddities.
- Remove internal code that has no callers, subject to the history and removal-evidence rules below. Check exported APIs before treating them as caller-less.
- Before removing or rewriting unusual code, read its history and reproduce the problem it handles. If its purpose remains unclear, ask before changing it.
- Logic removals need a nonempty `Evidence:` commit paragraph explaining the original purpose and why it is no longer needed, with commit ids or links.
- Never delete behavior or tests to satisfy a check. Document and test a narrow exception when a rule conflicts with required behavior.
- Commit changes with a file-path subject and a body explaining why and how. Omit Co-Authored-By and Signed-off-by trailers.

## Checks

- Use the Go version declared in `go.mod`, a C compiler and CGO_ENABLED=1.
- `make` builds. `make check` runs Go tests, lint, guidance checks, offline tooling tests and the committed-removal check. It also runs from the source archive, where history and workflow checks are skipped.
- `make lint` checks formatting, vet, shell errors and Python syntax. ShellCheck warnings need review before becoming mandatory.
- `make check-agents-md` verifies the paths and make targets named in backticks here. It does not validate source declarations.
- `make check-removals` checks non-merge commits against origin/trunk, or trunk if the remote ref is unavailable.
- Set CHECK_BASE and CHECK_HEAD to select a committed range. A commit that deletes a block of ten or more code lines with nothing added in its place needs an `Evidence:` paragraph.
- The removal count excludes tests, fixtures and generated source. A line count cannot judge whether the evidence is sound.
- `make distcheck` builds and tests committed HEAD through its source archive. Run working-tree checks before committing too.
- Keep the shared release and IRC scripts in sync with their canonical upstream copies.
- Project-specific release behavior belongs in `Makefile` and workflows. Tests mock external services and run independently of user configuration.

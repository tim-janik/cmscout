# Statistics mode

`cmscout --stats <file>` parses a single source file and prints per-semantic-block
statistics. The mode is a first step toward complexity linting and patch-level
complexity assessment: every record carries the file name and line span, and
block-owned comments carry their owner name. Owners may be functions or containers;
file-level comments have no owner. For JSON, decision explanations, directory scans,
and Git comparisons, use [metrics mode](metrics.md). Both modes use the same
`cyclomatic-v2` counting engine.

## Record format

Line-oriented, one record per data point:

```
# cmscout stats: <file>  (<language>, <N> parse errors)
block <kind> <qualified>  at <file>:<start>-<end>  lines=<n> chars=<n>
  prefix_lines=<n> prefix_chars=<n> [branches=<n> complexity=<n>|complexity=unknown] [methods=<n>]
  comment prefix_of=<qualified>  at <file>:<line>[-<end>]  lines=<n> chars=<n> [exceeds=<lines>]
  comment inside=<qualified>  at <file>:<line>[-<end>]  lines=<n> chars=<n> [exceeds=<lines>]
comment  at <file>:<line>[-<end>]  lines=<n> chars=<n> [exceeds=<lines>]
```

- `block` records list every extracted semantic block in source order; indented
  `comment` records directly below belong to that block.
- `prefix_lines`/`prefix_chars` measure the doc-comment run directly before the
  block (the "prefix command"), with one `comment prefix_of=` record per
  comment in the run.
- `comment inside=` records are comments inside the block that no nested block
  owns; the owner is the deepest containing block, named by its qualified name.
- File-level comments render unindented after the blocks they precede (before
  the next block, in source order).
- `complexity` is a function's cyclomatic complexity. `branches` is its decision
  count, equal to `complexity - 1`. See the rules below.
- `complexity=unknown` means no score is available. It has no `branches` field.
  This applies to declarations without bodies, macro definitions, and functions
  with unsupported C array bounds. Parse errors or invalid UTF-8 make all function
  scores unavailable. Partial reports include diagnostics and the CLI exits 2.
  Declarations and macros alone do not make a report partial.

- `exceeds=<lines>` flags comments longer than `--max-comment-lines` (default
  5; `0` disables the check).

C array length expressions contribute zero decisions. VLA bounds may hide
executable code (`int a[n ? 4 : 8]`), but tree-sitter sees unexpanded source,
so those expressions are skipped rather than guessed at. The rest of the
function is still scored, because an unmeasured function cannot be linted at
all, while a slightly low score still catches most regressions.

Qualified names use the C/C++ scope path (`Ase::LoopImpl::run`) or the nested
container path joined with `.` for TS/JS/Go/Bash (`Knob.updateDelta`).

## Prefix and inline comment rules

The prefix rule mirrors `AttachPrefixComments` in the diff pipeline: a comment
run attaches only when it ends on the line directly above the block
(`EndLine+1 == StartLine`, no blank line in between) and every member is an
own-line comment (only whitespace before it on its line), so a trailing comment
on the line above never counts as a prefix. When several blocks start on that
line, only the first one owns the run. Extraction-time run merging applies
(see [prefix-comments.md](prefix-comments.md)), so `//` runs come back as one
comment block.

Inline comments are owned by the deepest containing block: a comment inside a
method is owned by the method, a comment between methods by the class. Comments
consumed by a prefix run are excluded from inline ownership.

## Cyclomatic complexity

The rule is `complexity = 1 + branches`, for every function body in TS, TSX,
JS, JSX, Go, Bash, C, and C++. Empty functions score 1. The count uses syntax,
without folding constants or removing unreachable code. Each loop counts once,
including an unconditional loop. Nesting adds no extra penalty.

| Construct | Added decisions |
| --- | --- |
| `if`, `else if`, Bash `elif` | 1 each |
| `for`, range loops, `for-in`, `for-of`, `while`, `do`, Bash `until` and `select` | 1 each |
| Ternary `?:`, including Bash arithmetic | 1 |
| `&&`, `\|\|`, C++ `and` and `or`, Bash test `-a` and `-o` | 1 per operator |
| JS/TS `??`, `&&=`, `\|\|=`, `??=` | 1 per operator |
| JS/TS optional access or call `?.` | 1 per optional step |
| JS/TS parameter and destructuring defaults | 1 per default |
| Non-default switch labels | 1 per case value, including each value in a Go comma-separated list |
| Go `select` | Number of communication and default clauses minus 1, with a minimum of 0 |
| Bash `case` | 1 per pattern alternative before an unquoted `*` in that item |
| Bash parameter defaults, alternatives, assignments and errors, such as `${x:-y}` | 1 per expansion |
| `catch` | 1 per handler |

Plain `else`, switch `default`, and Bash's unquoted `*` pattern add nothing.
A quoted `"*"` is a literal match and adds 1. `return`, `break`, `continue`,
`goto`, `throw`, `finally`, and ordinary calls add nothing by themselves.
Comments, strings, regex text, and bitwise operators add nothing. Executable
expressions embedded in JSX, templates, strings, and shell substitutions still
count.

Each callable owns its body and its runtime parameter defaults. C++ constructor
initializers belong to the constructor. Nested functions, methods, generators,
and lambdas get separate records, even when the diff extractor keeps them inside
a larger block. Stats adds those records to a private document copy. It does not
change the blocks used by the diff pipeline. Anonymous names include their kind,
line, and column. Named enclosing functions participate in qualified names.
C++ lambda capture initializers and JS/TS computed method names belong to the
surrounding function, where they execute.

Class field initializers and static blocks do not contribute to an enclosing
function's score. They are not reported as implicit functions. Type annotations,
C++ constraints, `sizeof`, `alignof`, `decltype`, `noexcept`, and static assertions
are excluded. C/C++ parameter defaults are excluded because they execute at call
sites; their implicit execution is not added to callers. C array length
expressions contribute zero decisions; see the VLA note above.

### Research and limits

[NIST SP 500-235, sections 2 and 4](https://www.govinfo.gov/content/pkg/GOVPUB-C13-38411790de6e3963e8d208a4b0ae4403/pdf/GOVPUB-C13-38411790de6e3963e8d208a4b0ae4403.pdf)
defines cyclomatic complexity from a control-flow graph as `E - N + 2` for one
connected function. Counting decisions plus 1 is its source-level shortcut.
Short-circuit operators introduce decisions; default paths are already accounted
for. cmscout uses the case-counting variant, rather than treating a whole switch
as one decision.

[ESLint's complexity rule](https://eslint.org/docs/latest/rules/complexity)
provides the JS/TS conventions for defaults, optional chains, logical assignments,
and separate function scopes. A Go `select` needs a different rule from a switch.
Without a default it blocks until a communication can proceed, so there is no
implicit path past the select. See the
[Go specification](https://go.dev/ref/spec#Select_statements).

Node names alone are not a language-independent definition. For example,
`case_statement` is a label in C/C++ and a whole switch in Bash. TypeScript
optional calls use an unnamed token where JavaScript uses a named node.
Tests exercise the grammars pinned in `go.mod` through parsing, extraction, and
stats, with explicit expected scores for all eight language modes.

This is a static source metric, not a compiler control-flow graph. C/C++ macros
are not expanded, preprocessor conditions do not add decisions, and statements
in all preprocessor arms count. Overloaded operators use the same syntax rule as
built-in operators. Template expansion, implicit exception paths, dynamic shell
code such as `eval`, and called functions are not expanded. Scores therefore
can differ from a compiler's graph for a particular build. New language support
needs grammar-specific rules and tests; unsupported languages are rejected by
the CLI.

## CLI

```
cmscout --stats [--max-comment-lines N] [--old <name>] [-B <path>] <file>
cmscout --stats --scan [--root <dir>] [--include <glob>] [--exclude <glob>] [path ...]
```

The file may be `-` (stdin) when `--old <name>` supplies the display name for
language detection; `-B <path>` reads the content from another path, mirroring
the diff-mode redirection.

`--scan` walks the given files and directories (default: current directory)
with the same selection machinery as `--metrics --scan`: `--include`/`--exclude`
globs are root-relative, `*` and `?` stay within one path segment, `**` spans
directories, exclusions win, and `--root` sets the naming root. Each selected
file is rendered as one `# cmscout stats:` section. Skipped files and errors are
reported as `# skipped`/`# diagnostic` records; any error makes the scan partial
and the CLI exits 2.
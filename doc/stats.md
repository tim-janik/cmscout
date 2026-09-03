# Statistics mode

`cmscout --stats <file>` parses a single source file and prints per-semantic-block
statistics. The mode is a first step toward complexity linting and patch-level
complexity assessment: every record carries the file name and line span, and
comments carry the fully qualified function name, so later stages can map each
data point back to the source (e.g. for lint output) or to a diff hunk (e.g. to
measure whether a patch increases or decreases block complexity).

## Record format

Line-oriented, one record per data point:

```
# cmscout stats: <file>  (<language>, <N> parse errors)
block <kind> <qualified>  at <file>:<start>-<end>  lines=<n> chars=<n>
  prefix_lines=<n> prefix_chars=<n> [branches=<n>] [methods=<n>]
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
- `branches` is a best-effort decision-point count for function-like blocks
  (cyclomatic complexity precursor), see below.
- `methods` is the number of direct method-like children for classes,
  interfaces, and namespaces.
- `exceeds=<lines>` flags comments longer than `--max-comment-lines` (default
  5; `0` disables the check).

Qualified names use the C/C++ scope path (`Ase::LoopImpl::run`) or the nested
container path joined with `.` for TS/JS/Go/Bash (`Knob.updateDelta`).

## Prefix and inline comment rules

The prefix rule mirrors `AttachPrefixComments` in the diff pipeline: a comment
run attaches only when it ends on the line directly above the block
(`EndLine+1 == StartLine`, no blank line in between) and every member is an
own-line comment (only whitespace before it on its line), so a trailing comment
on the line above never counts as a prefix. Extraction-time run merging applies
(see [prefix-comments.md](prefix-comments.md)), so `//` runs come back as one
comment block.

Inline comments are owned by the deepest containing block: a comment inside a
method is owned by the method, a comment between methods by the class. Comments
consumed by a prefix run are excluded from inline ownership.

## Branch counts

`branches` counts decision points inside a function-like block's byte span,
each adding 1:

- `if` (each `else if`/`elif` is its own decision point), `for`, `for-in`/`for-of`,
  `while` (incl. bash `until`), `do`
- ternary `?:` (`conditional_expression` in C/C++, `ternary_expression` in
  TS/JS)
- case/default labels: `case_statement` (C/C++, each label incl. default),
  `switch_case`/`switch_default` (TS/JS), `expression_case`/`default_case`/
  `type_case`/`communication_case` (Go), `case_item` (bash)
- `catch` clauses
- short-circuit `&&`/`||` (inside `binary_expression` for TS/JS/Go/C/C++ and
  inside `list` for bash)

Plain `else` clauses add 0. The count is language-approximate and documented
best-effort; it is the input for a future cyclomatic-complexity report and for
patch-complexity deltas across correlated block pairs.

## CLI

```
cmscout --stats [--max-comment-lines N] [--old <name>] [-B <path>] <file>
```

The file may be `-` (stdin) when `--old <name>` supplies the display name for
language detection; `-B <path>` reads the content from another path, mirroring
the diff-mode redirection.
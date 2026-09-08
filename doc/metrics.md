# Function metrics

`--metrics` measures one source file or compares two files in TS, TSX, JS, JSX,
Go, Bash, C, or C++. It includes methods, generators, nested functions, arrows,
and lambdas. It needs neither Git nor `.cmscout.json`.

```sh
cmscout --metrics somefile.cc
cmscout --metrics --format json somefile.cc
cmscout --metrics --explain somefile.cc
cmscout --metrics --stdin-name scratch.go -
cmscout --metrics --root project --name src/main.cc saved-copy
cmscout --metrics before.cc after.cc
cmscout --metrics --format json before.cc after.cc
```

Put flags before the input. `--name` gives a saved file its logical filename,
including its language extension. `--stdin-name` does the same for stdin.
With an explicit `--root`, relative logical-name overrides are relative to that
root. Ordinary input paths remain relative to the current directory.

The existing two-file diff command still works without `--metrics`.

## Scan files and directories

```sh
cmscout --metrics src/
cmscout --metrics --scan --format json src/ lib/ helper.cc
cmscout --metrics --scan --include '**/*.go' --exclude 'vendor/**'
```

A single directory selects a recursive scan. `--scan` treats every path as an
independent input, including when there are two paths. With no paths it scans
the current directory. Overlapping inputs are deduplicated by logical path and
files are sorted. The default naming root is inferred from the common directory
of the inputs. Use `--root` to keep names consistent with other scans.

Repeat `--include` to select any matching pattern and `--exclude` to reject any
matching pattern. Patterns match root-relative paths with `/` separators.
`*` and `?` stay within one path segment; `**` as a complete segment spans zero
or more directories. Exclusions take precedence. Quote patterns in the shell.

Scans skip `.git`, symlinks, special files, and unsupported source extensions.
They include hidden, generated, and dependency source files unless filtered.
JSON has `kind: "scan"`, `population: "selected_files"`, a `files` array of
ordinary snapshots, and explicit `skipped` and `diagnostics` arrays. A complete
report covers the selected regular source files, not an application's full
membership. Read errors, binary source, or incomplete file analysis produce a
partial report and exit 2. An empty selection is a complete report with no files.

Staged and revision inputs are not available yet.

## Measurements

Every callable has these fields in the JSON `components` array:

| Field | Meaning |
| --- | --- |
| `qualified_name`, `parent_name` | Names for joining records and finding their owners. |
| `name_parts`, `name_origin`, `kind` | Structured name, how it was found, and component kind. |
| `span` | Original callable definition, without a prefix comment. |
| `body` | The callable body within that definition. |
| `declaration` | Source anchor for documentation, including an export or variable declaration when needed. |
| `size` | Bytes, Unicode characters, and physical lines of the definition. |
| `cyclomatic` | Integer `value`, `status`, and optional `decisions`. |
| `comment_status` | Whether prefix and inline ownership are complete. |
| `prefix_comment` | Bytes, characters, lines, and span of the preceding comment run. |
| `inline_comments` | Sizes and spans of comments owned inside this callable. |
| `inner_functions` | Names of directly nested callables. Each has its own full record. |

File, namespace, class, object, and Go type records provide parents. They have
no `cyclomatic` field. A declaration without a body has no function metric.
A function with an authored empty body has complexity 1.

Complexity and comment ownership exclude nested definitions. Source size includes
their text. Adding the sizes of nested functions would therefore count some text
twice. This release measures the function population. It does not claim complete
file execution, class totals, or application totals.

The top-level `comments` array retains comments outside functions, directives,
and ambiguous prefixes. It contains each owned comment range once; function
records also expose their corresponding entries for convenience.

## Counting profile

JSON schema `1` records `cyclomatic/source-v1` and `size-comments/source-v1`.
These profiles describe source syntax. They do not build a control-flow graph,
resolve types, fold constants, or establish how many tests prove correctness.

Complexity starts at 1. Each owned decision adds 1:

| Syntax | Rule |
| --- | --- |
| `if`, `else if`, Bash `elif` | One per condition; plain `else` adds none. |
| Ordinary, range, iterator, and shell loops | One per loop, including an unconditional loop. |
| `?:`, `&&`, `\|\|` | One per conditional or short-circuit operator, wherever evaluated. |
| JS/TS `??`, `&&=`, `\|\|=`, `??=` | One per operator. |
| JS/TS optional access or call | One per `?.` segment. |
| JS/TS parameter and destructuring defaults | One per default, plus decisions in its expression. |
| C/C++/JS/TS switch | One per non-default label, including stacked labels. |
| Go switch and select | One per non-default clause; several case values count once. |
| JS/TS/C++ catch | One per handler. |
| Bash case | One per item; a final unquoted `*` item adds none. |
| Bash parameter alternatives | One per `-`, `:-`, `=`, `:=`, `+`, `:+`, `?`, or `:?` operator. |

Returns, breaks, ordinary calls, arithmetic, comparisons, and bitwise operators
add no decisions. Expressions inside them can still contain decisions.
Literal text and type-only expressions add none. Runtime expressions inside
templates, JSX, or shell substitutions keep their nearest callable owner.

Nested function bodies and JavaScript parameter defaults belong to the nested
function. C++ lambda capture initializers and JavaScript computed method names
belong to the enclosing execution context. Constructor initializer lists belong
to the constructor. Bash function redirects belong to the function.

The C/C++ profile scans ordinary source in every written preprocessor branch.
It skips directive conditions and macro bodies. It counts `if constexpr` once
and visits both branches. Ordinary `sizeof`, `alignof`, `decltype`, `noexcept`,
and `requires` operands add no outer decisions. Separately defined callable
bodies still have their own source scores. Dynamic C array bounds are currently
unsupported and produce an incomplete report, including bounds that might be
constant after macro expansion or type checking.

The Bash profile excludes legacy test arguments `-a` and `-o`, implicit `set -e`
exits, traps, and text passed to `eval`. Case terminators add no decisions.
These limits are also included in each relevant report.

## Sizes and comments

Characters are Unicode code points, including spaces, delimiters, and line
endings. CRLF counts as two characters and one line break. Lines count the
physical lines touched by a nonempty range. An empty range is zero lines;
ending at the start of the next line does not add that next line.

The callable span is the syntax node itself. For `const f = x => x`, it starts
at `x => x`. The declaration anchor starts at `const`, allowing a comment above
the assignment to document `f`. The original source is never rewritten for
measurement.

A prefix is an own-line comment run immediately above the declaration, with
only whitespace and no blank line in the gap. Its size includes gaps between
comments, but excludes the gap to the declaration. A same-line or trailing
comment does not become a prefix. Adjacent own-line single-line comments merge
into inline runs. Blank lines, trailing comments, and multiline blocks end a run.

A child function owns its prefix even when that prefix sits inside its parent's
body. Its internal comments also belong to the child. Shebangs and recognized
Go, TypeScript, and shellcheck directives are retained as directives. They do not
count as function documentation.

A prefix above several callable declarators is ambiguous. The report retains
its range with `ownership: "ambiguous"`; the affected functions have a null
prefix and `comment_status: "ambiguous"`.

## Names and source context

`naming_version` is `source-v1`. Names contain the source namespace, logical file,
enclosing components, and local name. `name_parts` retains each part separately.
Rendered segments use `kind=name`, joined by `::`. Name bytes outside ASCII
letters, digits, `_`, `.`, and `-` use uppercase percent encoding.

C/C++ callables include `#` followed by an escaped source signature. Parameter
variable names and defaults are removed; written types and qualifiers remain.
This is a syntax signature, not a compiler symbol. Bound arrows, lambdas, and
function expressions use their binding name. Anonymous callables use their kind
and `~1`, `~2`, and so on,
counted within their immediate owner. Duplicate named siblings receive a local
suffix starting at `~2`. Names of identical anonymous bodies remain distinct.

The root is the nearest ancestor containing `go.mod`, `go.work`,
`compile_commands.json`, or `.git`, falling back to the input directory.
`--root` overrides it. The namespace contains the root directory's basename and
the first eight bytes of its SHA-256 path digest. Logical file paths use `/`.
This selects a naming root; it does not yet infer module or application membership.

Names are unique within a snapshot. They stay the same for the same root,
logical path, source, and naming version, including when using stdin or a saved
copy. Different checkout roots have different namespaces. Inserting an anonymous
sibling or renaming a parent can change names; a name is not permanent identity
across revisions. Unresolved C++ owners have `name_origin: "unresolved_owner"`.

`snapshot_id` hashes the namespace, logical path, selected language, and source.
`content_digest` hashes only the original source bytes. No timestamp is required.
For `.h`, cmscout chooses the C or C++ tree with fewer parse errors, preferring
C++ on a tie.

## Unknown values and exit status

JSON spans use zero-based lines and byte columns, with exclusive end offsets.
Text locations use one-based lines and byte columns. Each decision from `--explain`
has a rule, span, and contribution. A complete score equals 1 plus their sum.

Known absence gives a prefix of zero characters and lines with a null span,
or an empty inline-comment array. Unknown measurements use null values and an
explicit status. Any parse recovery error or invalid UTF-8 makes the file report
partial and its semantic measurements unavailable. Known source sizes remain
available; an invalid UTF-8 range has a null character count.

Exit 0 means complete analysis. Exit 2 means an input error, incomplete required
analysis, or unresolved matching ambiguity. A partial report is still emitted
in the requested format, with
diagnostics. JSON stdout contains only that document. Existing diff errors keep
exit 1. Reports are informational; no complexity threshold is imposed by default.

## Compare two files

The two-input form reports both snapshots, touched component names, metric
deltas, and the existing semantic diff. Both snapshots use the single-file
contract above. Diff rendering does not change their source spans or comments.

Use logical names when content comes from temporary copies:

```sh
cmscout --metrics --format json --root "$PWD" \
  --old src/main.cc --new src/main.cc \
  -B before.cc -A src/main.cc
```

`--old` and `--new` names are relative to an explicit root. Ordinary positional
paths remain relative to the current directory. One content path may be `-` for
stdin. In this mode use `--old`/`--new` for its language, rather than `--stdin-name`.

Comparison JSON has these fields:

| Field | Meaning |
| --- | --- |
| `schema_version`, `status` | Comparison contract version and completeness. |
| `before`, `after` | Complete snapshot records, including unchanged functions. |
| `changes` | Touched components, with old and new qualified-name references. |
| `before_ranges`, `after_ranges` | Changed original source lines, including edits outside measured functions. |
| `diagnostics` | Comparison problems, in addition to diagnostics in each snapshot. |
| `diff` | `{format: "cmscout-text-v1", text: ...}` containing the semantic diff without ANSI colors. |

Each change has `before_name`, `after_name`, both component kinds, matching
evidence, flags, source ranges, and `delta`. Look up each name in its side's
`components` array; the snapshot ID supplies the other part of its identity.
The diff text is for display. Rules should read the structured fields.

The current inventory names files, callables, and their namespace, class,
object, and Go type containers. Edits to other source are retained in the file
change and diff. This includes imports, macros, and top-level statements that
have no separate metric record yet.

Change flags distinguish additions, removals, local renames, kind changes,
qualified-name changes within the file, movement, code, signatures, prefix
comments, inline comments, and formatting. Different input filenames are labels
for the two versions; they do not alone count as a rename. A moved component
changes its containing source region or its order relative to another retained
component. An inserted line alone does not mark every later function as moved.

`direct_changed` describes changes to a component's source outside its nested
components, its own comments or declaration, or its exclusive complexity.
`descendant_changed` marks changes below it. Both can be true. Formatting checks
preserve text inside literals, so changing spaces in a string is a code edit.
Ranges are whole source lines; a line can contain text from several components.

The matcher uses names, bodies, and enclosing components. Unchanged source lines
can establish correspondence even when several anonymous bodies are identical.
Tied matches remain `ambiguous`, with candidate names and no trusted delta.
Children of an ambiguous match also have no trusted delta. Match similarity is
a matching score, not a probability or a proof of identity.

A matched, comparable pair has numeric `delta` fields for complexity, source
lines and characters, prefix size, total inline-comment characters, and inline
comment count. Inline arrays stay intact in both snapshots; entries are never
subtracted by their array position. A missing side is null, with no invented
zero score. Cross-language or different-profile deltas are `incomparable`.
Incomplete analysis makes deltas `unavailable` and avoids claiming additions or
removals from a damaged inventory.

`--word-diff`, `--word-diff-span-threshold`, and `--ignore-all-space` affect only
the rendered diff in comparison mode. They do not filter touched names or change
measurements. JSON remains one document in every case.

## Pre-commit feedback

[examples/lint-metrics.py](../examples/lint-metrics.py) reads comparison JSON and
joins touched names to the matching snapshot records. It can limit complexity,
complexity growth, or each prefix and inline comment's character count:

```sh
cmscout --metrics --format json --root "$PWD" \
  --old src/main.cc --new src/main.cc \
  -B before.cc -A src/main.cc > comparison.json &&
python3 examples/lint-metrics.py \
  --max-complexity 10 --max-increase 0 --max-comment-chars 300 < comparison.json
```

Choose limits for your project. The script checks only touched callables that
exist after the edit. Growth limits apply to matched functions; use an absolute
limit to constrain new functions too. Equality passes. A violation prints the
qualified function name, source location, observed value, and chosen limit.

The script exits 0 for a pass, 1 for a rule violation, and 2 for missing or
incomplete facts. A growth rule also rejects non-comparable deltas. It never
treats an absent function record as zero. Use `--explain` on the comparison to
locate the decisions behind a high score. This is a file-pair example; it does
not yet read staged content or choose commit parents.

## A small lint rule

Save a complete report, then check a chosen function limit with `jq`:

```sh
cmscout --metrics --format json somefile.cc > metrics.json &&
jq -e --argjson limit 10 '
  .schema_version == "1" and .status == "complete" and
  all(.components[] | select(has("cyclomatic"));
      .cyclomatic.status == "complete" and
      (.cyclomatic.value | type) == "number" and
      .cyclomatic.value <= $limit)
' metrics.json
```

Print the functions that exceed the same limit:

```sh
jq -r --argjson limit 10 '
  .components[] | select(has("cyclomatic")) |
  select(.cyclomatic.value > $limit) |
  "\(.qualified_name): complexity \(.cyclomatic.value) exceeds \($limit)"
' metrics.json
```

Use `--explain` to locate the contributing decisions. A comment rule can inspect
`prefix_comment.chars` or each `inline_comments[].chars` and report that entry's
span. Rules should reject unknown values. The comparison example above narrows
these checks to touched components.

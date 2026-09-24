# Pipeline

cmdiff matches code blocks between two versions of a file and diffs them per
block, so a moved function shows as one changed unit instead of a delete plus
an add. This page walks through the stages in execution order and names the
entry point of each.

## Stages

1. Parse. `pkg/parser` wraps tree-sitter for TS, TSX, JS, JSX, Go, Bash, C and
   C++. For `.h` files both C and C++ grammars run and the parse with fewer
   errors wins; ties keep C++.
2. Extract. `pkg/extract` turns the AST into `SemanticBlock`s: functions, methods,
   classes, interfaces, JSX elements, string templates, imports, constants,
   comments. Namespaces annotate scope instead of becoming components; local
   declarations and lambdas stay inside their owning callable.
3. Match. `matching.MatchBlocks` pairs old and new blocks in stages:
   1. exact name and kind,
   2. exact comment text,
   3a. distance table over top-level containers,
   3b. recursive matching of children inside matched containers,
   3c. residual distance table restricted to identical ancestor scopes,
   3d. rescue of hoisted callables across scopes (function declarations hoist,
      methods belong to their class), then hierarchical matching of their
      children,
   4. remaining comments pair through a comment-only similarity table, but not
      against code.
4. Collapse. `CollapseMatchedSubBlocks` replaces matched children inside
   matched parents with canonical references. See
   [doc/canonical-references.md](canonical-references.md).
5. Prefix attachment. `AttachPrefixComments` merges doc-comment runs into the
   component they document; `SuppressEnclosedComments` drops comments that
   already render inside their container. See [doc/prefix-comments.md](prefix-comments.md)
   and [doc/enclosed-comments.md](enclosed-comments.md).
6. Word diff. `pkg/diff` fills each pair's `InnerDiff` from its final source text.
   See [doc/word-diff.md](word-diff.md).
7. Report. `pkg/report` renders one section per kind (Imports, Functions,
   Lambdas, Classes, Comments), a whole-file supplement under Other that only
   fills gaps, and a Summary. See [doc/test-invariants.md](test-invariants.md)
   for the coverage and duplication guarantees this layout must hold.

Steps 4 and 5 are the only stages that mutate block sources; both run before
the word diff so `InnerDiff` includes the rewritten text.

## Hoisted callable rescue

Matching stage 3d rescues orphaned named callables: function declarations
hoist and methods belong to their class, so both survive a container refactor
such as a class method ported to a nested function. They pair by exact name
across scopes; arrows and lambdas close over their scope and stay out. Both
containers must be orphaned, because children of matched containers keep
their hierarchical matching authority. Rescued callables then get hierarchical
child matching like any matched pair.

## Data model

`pkg/ir` defines the two structs every stage speaks:

- `SemanticBlock`: ID, Kind, Name, Parent, Span, Source. Kinds cover imports,
  functions, methods, arrow functions, lambdas, classes, interfaces, enums,
  JSX, templates, macro functions, namespaces, concepts, comments and more.
- `CorrelatedPair`: Old and New block pointers, Confidence, Supplemental flag.
  One-sided pairs have a nil Old or New; supplemental pairs come from the
  whole-file fallback rather than matching.

## Similarity scoring

`matching.BlockSimilarity` scores a candidate pair as a weighted sum:

- name 0.35, body text 0.50, kind 0.15.
- Kind term: same kind 1.0, compatible kinds (say function versus method)
  0.6, anything else 0.3, so conversions stay matchable.
- Comments score without the name term; source text alone decides.
- Pairs below the threshold (default 0.5) do not match.

The displayed header percentage is `PairSimilarity`: the same formula with a
display name term that gives anonymous ordinal pairs full name credit, so
identical bodies show 100%.

Anonymous blocks carry per-side ordinals (`arrow_function.03`) for display;
ordinals track position on one side and never act as identity across sides.

## Movement tags

A matched pair renders `[moved]` when its position falls outside the longest
common subsequence of container order, and `[converted]` when its kind changed.
Comments move only when their enclosing container changed, never by rewording.
The `[whitespace]` tag marks pairs whose only change is formatting; see
[doc/whitespace-classification.md](whitespace-classification.md) for how that
decision is made. Invariant tests pin the tags end to end:
[doc/test-invariants.md](test-invariants.md).

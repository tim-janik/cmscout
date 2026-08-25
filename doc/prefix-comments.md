# Prefix comment attachment

`AttachPrefixComments` in `pkg/correlate/prefix.go` merges doc-comment runs that sit
directly before a component into that component so they render as one diff unit.

## What counts as a prefix

A comment run directly before a component with no blank line between run and
component (EndLine+1 == StartLine) and only whitespace in the gap. A `//` comment
line is one tree-sitter node per line; without run expansion only the last comment
line would attach and the start of the comment would be lost as diff context.

## Attachment rules

- The whole run attaches, on both sides of a matched pair (per side for
  added/removed comments): sources are prepended to the component's Source, Span
  extends to the first comment, and consumed pairs are dropped from the standalone
  Comments section.
- All-or-nothing per group: the maximal contiguous run through the counterparts
  (commentRunThrough) attaches only when the full run is a clean prefix of the
  component; otherwise the entire group stays standalone so no text renders twice
  (inside the component diff and again as a standalone pair).
- Runs are trimmed to blocks whose counterpart attaches too
  (trimRunToCounterparts); a diverged block (e.g. reordered) stays standalone.
- Matched prefixes attach only when both sides succeed onto the same component
  pair; comments that moved between parents stay independent entries.
- Enclosed comments (inside a component) are handled elsewhere in the report;
  this pass covers the "belongs to" case only.

## Extraction-time run merging

Adjacent own-line single-line comments (a run of consecutive `//` lines, or
one-line `/* ... */` blocks) merge into one comment block during extraction
(`mergeCommentRuns`, right after the AST walk). A unified diff presents a
contiguous comment body as one hunk; cmdiff must too, or a multi-line doc
comment becomes several one-line "comment [added]" entries. Runs are not
limited to component prefixes, so a standalone file header renders as one
comment as well. Blank lines, inline comments and multi-line `/* ... */`
blocks end a run.

`main.go` calls the pass after `CollapseMatchedSubBlocks` and before word-level
Diff so the InnerDiff includes the prefix text. Stage order:
[doc/pipeline.md](pipeline.md). How absorbed comments interact with collapsed
references: [doc/canonical-references.md](canonical-references.md).

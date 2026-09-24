# Canonical references

`CollapseMatchedSubBlocks` in `pkg/correlate/collapse.go` replaces a matched child's
source with a canonical reference comment so a parent's diff stays small.
`cmd/cmscout/main.go` calls it on the correlation result before
`AttachPrefixComments` and word-level Diff. This page describes how references
are built, folded and absorbed.

## Format and numbering

A reference looks like `// [matched: method foo]`. Anonymous blocks get
ordinals per side and per kind, in source order, for example
`// [matched: arrow_function.03]`.

Two unchanged anonymous blocks must receive the same number on old and new
sides so they pair as [unchanged] rather than bogus [moved]. Old and new sides
can legitimately number the same pair differently (arrow_function.03 versus
arrow_function.01) because counters run independently; the pass therefore
writes the new side's identity on both sides. Collapsed parents stay
byte-identical that way. Otherwise an unchanged parent would show a phantom
diff line under a below-100% header.

The pass also collapses the matched child of added and removed parents into one
reference, using the new side's identity (a class-to-function conversion
relabels as a function). A matched parent keeps collapsing after such a
conversion.

## Folding rules

- References fold only at own-line positions. A reference nested mid-expression
  stays inline.
- Only own-line prefix comments ending right before the child fold into it,
  and only across a whitespace gap. Trailing same-line comments stay in the
  source.
- Two back-to-back matched children produce adjacent reference lines;
  absorption must not eat the first reference while processing the second.
  References are not prefix comments.
- Namespaces are a scope, not a component, so nothing collapses into them.
  Out-of-class members render through their own method pair.

## Absorption of prefix comments

Prefix runs attach per [doc/prefix-comments.md](prefix-comments.md); this
section covers how absorption interacts with references. Absorption does not
depend on an exact text match. A prefix comment whose sides match by
similarity is absorbed too.

A reworded absorbed comment must stay visible: canonical references are
identical on both sides, so absorbing the rewording would hide it. The pair
therefore survives next to the collapsed method.
TestChangedCommentInsideMatchedContainer pins this end to end, see
[doc/test-invariants.md](test-invariants.md).

One-sided absorption never silences the other side. If only the old comment was
absorbed, the new comment's pair stays in the report, and duplicate comment text
removes neither occurrence. A comment that moved between parents can be
absorbed on both sides, which suppresses the pair by design.

## No global name relation

`CollapseMatchedSubBlocks` never relates two blocks by (kind, name) alone:

- A block with a different ID and parent than a matched child keeps its real
  source, even with the same name. An added sibling method must not be rewritten
  into a "// [matched: method foo]" reference.
- A method inside a class and a top-level function can share a name without
  sharing an ID; children collapse per block ID, with no global name map.

Contained elements nested in a removed parent (for example a Lit template
element) are replaced by their canonical reference inside the removed source so
their text does not render twice.

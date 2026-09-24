# Test invariants

The invariant suite in `cmd/cmdiff/invariant_test.go` pins down guarantees that
span the whole pipeline: parse (`pkg/parser`), block matching (`pkg/matching`,
`pkg/correlate`), child collapsing (`CollapseMatchedSubBlocks`), prefix comment
attachment (`AttachPrefixComments`), word diff and report (`pkg/report`). This
page states each guarantee and names the tests that enforce it;
[doc/pipeline.md](pipeline.md) walks the stages themselves.

## Coverage

assertCoverage checks that every non-empty line of both inputs appears in the
command output, side-aware and occurrence aware. A line that occurs k times on
the old side must render at least k times as an old-side line (removed or
context), likewise for the new side. A context line stands for one old line and
one new line and counts toward both sides. Reducing both inputs to a set of line
contents cannot catch a missing occurrence or an old line standing in for a new
one, so counting occurrences is required.

## Header similarity agrees with the shown diff

assertHeaderSimilarityMatchesDiff enforces that header percentage and rendered
diff describe the same text:

- A pair displayed at 100% similarity, without a [whitespace] tag, must not
  render added or removed lines below it.
- A pair with a change in its diff must display below 100%.

Both numbers derive from the pair's final source text (`matching.PairSimilarity`
and InnerDiff), so any regression that lets them diverge fails here. The known
failure signature is a 100% header rendered above a phantom -/+ line, caused
by collapse writing non-canonical references (spin_drag_pointermove).

## Repeated lines survive rendering

De-duplication must never collapse repeated lines within one coherent
rendering. An unchanged block and the raw diff show every occurrence, closing
braces included, so each snippet stays valid on its own even when the same line
appears several times. The whole-file coverage supplement follows the same rule
in its own added/removed hunks while still suppressing lines the semantic
sections already showed.

## De-duplication stays minimal

assertDiffNoDuplication checks that the raw diff shows every unique input line
exactly once. Lines whose content occurs more than once across both inputs are
exempt: they legitimately render once per occurrence and remain covered by the
coverage check.

assertReviewDedupMinimized pins two properties of the review output:

1. The coverage supplement renders in the Other section, after all semantic
   sections, and shares no (type, content) line with them. It only fills gaps.
   In the unknown-language fallback the whole-file pair lands in Other with no
   earlier sections, so the check passes vacuously there.
2. Total content lines stay within twice the input size plus a small constant.
   The factor of two exists because a changed block intentionally shows both
   inside its parent (say a class) and as its own section. Anything beyond that
   bound means a coverage or duplication regression.

## CLI flags

TestCLIFlagCombinations runs every diff and review flag, and combinations of
them, end to end. Each invocation must succeed, keep the coverage invariant for
modes that preserve raw lines (word-diff renders replacements as combined word
lines instead), and surface the expected markers.

TestSkipUnchangedFlag exercises --skip-unchanged end to end: identical blocks
disappear from the detailed listing and from the coverage supplement, changed
content still renders, and the summary still counts the suppressed blocks.

TestIgnoreAllSpaceEndToEnd uses a file with a whitespace-only line change and a
separate real change. The output must represent both raw forms; the old text
must never be emitted as if it were the new text. Classification rules:
[doc/whitespace-classification.md](whitespace-classification.md).

## Input size

The project has no input size limit by design. Large files must be accepted and
diffed correctly; single-line inputs keep the LCS table small.

## Scope-aware pairing

TestReorderedClassesWithDuplicateMethods covers two classes that both define
foo, reordered, with one real change in A.bar. Old A.foo pairs with new A.foo,
old B.foo with new B.foo, the report shows exactly two matched foo pairs at
100%, and no false cross-container method change.

## Canonicalization keeps real sources

TestAddedMethodNotRewrittenE2E and TestRemovedMethodKeepsSourceE2E pin that
added and removed methods render their real source inside their class, never
rewritten into a "// [matched: method foo]" reference by global name
canonicalization. See [doc/canonical-references.md](canonical-references.md).

## Renames keep descendants matched

TestNestedRenameDeepStaysMatched walks outer, inner renamed to renamedInner,
then deep. Recursive hierarchical matching keeps deep a matched unchanged
block after its parent was renamed; it never surfaces as an added/removed
pair.

## Comments pair separately from code

TestChangedCommentInsideMatchedContainer covers a reworded comment nested
under a matched container: comments pair through the comment-only similarity
table, never against a code child. The pair survives collapse even though both
sides are absorbed into the matched method's reference (see
[doc/canonical-references.md](canonical-references.md)), renders in the
Comments section, and the whole-file supplement does not re-show the raw lines
as a removal/addition pair.

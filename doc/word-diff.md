# Word diff

--word-diff splits paired lines into words and diffs them at word granularity
(pkg/diff), rendering removed and added words with markers and colors
(pkg/report).

## Markers and colors

By default added words get a green "+" and removed words a red "~". All
markers and colors live in `WordDiffStyle` (pkg/report/text.go) and can be
edited, e.g. to brace style:

	WordDiffStyle.AddedPrefix = "{+"
	WordDiffStyle.AddedSuffix = "+}"
	WordDiffStyle.RemovedPrefix = "{-"
	WordDiffStyle.RemovedSuffix = "-}"
	WordDiffStyle.AddedColor = "green"
	WordDiffStyle.RemovedColor = "red"

Setting prefixes and suffixes to "" yields color-only rendering like git's
default. The line prefix still matches the line type (" " context, "+" added).

## Span consolidation

Per-word markup gets noisy beyond a point, so changed words consolidate into
spans:

- Each maximal run of changed words becomes one removed span plus one added
  span; common prefix and suffix words stay outside as context.
- When more than `WordDiffSpanThreshold` of a line's words changed (default
  `DefaultWordDiffSpanThreshold` = 0.4), all runs merge into a single span
  covering the whole changed range, keeping leading and trailing context. A
  negative threshold disables that merge.
- Adjacent removed and added runs form replacement blocks: the added line
  carries the word diff and the removed line is suppressed.

Under --ignore-all-space:

- Whitespace-only word differences are suppressed entirely: a line whose only
  difference is whitespace renders as plain context, no word-diff markers at all.
- Common whitespace never breaks a changed region. Otherwise short changed
  tokens like "+" or "/" render as isolated one-character marks, unreadable
  next to the words they belong to.
- Whitespace stripped from span edges is re-emitted as a plain space on the side that had
  it instead of marked fragments ("~ ~").
- Context words carry a side (`ir.DiffWordSide`): matched words belong to both
  sides, one-sided whitespace words render as context but own exactly one
  side, so span consolidation rebuilds each side's text without duplicating
  words.

# Whitespace classification

--ignore-all-space decides per line whether two versions differ only in
formatting. `pkg/report/whitespace.go` classifies lexically, on token streams,
because byte-level comparison mislabels real changes.

## What counts as formatting

Pure formatting increments the Whitespace counter:

- indentation and trailing space,
- spacing around operators and braces.

Meaningful changes increment Changed. They can hide inside otherwise untouched
lines: strings, template literals, comments, regex literals, template
interpolations, and identifier or operator boundaries are all token territory.

Multi-character operators are single tokens. Splitting one apart turns
`x === y` into `x = = = y`, which is a change; adding space around it is not.

## Per-language literals

lexicalTokensForLanguage applies language-specific rules. The same character
means different things per language:

- Backticks: templates in JS/TS, raw strings in Go, command substitutions in
  Bash, plain punctuation in C/C++.
- C++ raw strings stay atomic, including encoding prefixes before the quote.
- C/C++ encoded literal prefixes (`L`, `u`, `u8`, `U`) belong to the token, so
  a string with prefix stays one unit.

Classification compares per side with isWhitespaceOnlyMatchForLanguages, since
the two inputs may use different languages and rules differ per side.

## Summary counters

The summary follows the same classification. A meaningful change inside a
string, comment, or token increments Changed and no Whitespace row appears; a
pure formatting change increments Whitespace alone.

End-to-end expectations for --ignore-all-space live in
[doc/test-invariants.md](test-invariants.md); the report stage itself in
[doc/pipeline.md](pipeline.md).

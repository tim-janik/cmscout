# Enclosed comments

A comment strictly inside a function-like container already renders as part of
that container's source diff. `SuppressEnclosedComments`
(pkg/correlate/enclosed.go) therefore drops such comment pairs from the
standalone Comments section; keeping them would show the text twice.

## Rules

- Function-like containers are functions, methods, object methods, lambdas,
  arrow functions, lifecycle methods and macro functions. Classes and
  namespaces do not count: a comment inside a class but before a method is a
  prefix to that method (see doc/prefix-comments.md), not an inner comment of
  the class itself.
- A matched comment pair counts as enclosed only when both sides sit inside
  the same container pair. Different container pairs mean the comment moved
  between functions, so it stays visible.
- One-sided comments are enclosed when their own side sits inside a container
  present in the result.

The pass runs in cmd/cmdiff/main.go after CollapseMatchedSubBlocks and
AttachPrefixComments, before the word diff (stage overview:
doc/pipeline.md).

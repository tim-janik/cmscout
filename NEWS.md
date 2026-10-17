## CMSCOUT 0.2.0

### Added:

* Introduced CMSCOUT, a code motion aware diff tool for TypeScript,
  JavaScript, TSX, JSX, Go, Bash, C and C++.
* Added block matching across renames, moves and language conversions,
  with per-block similarity ratings and change summaries.
* Added support for functions, classes, methods, imports, constants,
  JSX elements, string templates and C/C++ function-like macros.
* Added line and word diffs, whitespace filtering, and the `--summary`,
  `--skip-unchanged` and `--simple-diff` options.
* Added Git external diff integration through `git-diff-wrapper.sh`,
  with `NO_COLOR` and `CMSCOUT_*` options.
* Added `cmscout --version`.

### Changed:

* Grouped doc comments with the code they describe and suppressed duplicate
  comments already shown inside functions.
* Replaced matched child blocks with reference comments in parent diffs,
  so the parent diff stays small when methods move or change.
* Consolidated word diff highlights into readable spans.
* Kept added and removed block bodies white by default, with configurable
  full-line colors via `--added-style` and `--removed-style`.
* Reordered summary counts by significance and omitted zero counts.

### Fixed:

* Made matching of moved inner functions deterministic.
* Fixed HTML template extraction for void elements regardless of case,
  and rejected unfinished opening tags.
* Counted missing tokens as parse errors.
* Suppressed whitespace-only blocks when both `--skip-unchanged` and
  `--ignore-all-space` are set.
* Fixed Git wrapper handling of merge conflicts and `CMSCOUT_*` variables.

### Build and releases:

* Added source and Linux x86-64 release archives built and tested on Ubuntu 24.04.
* Built release binaries from the source archive with `make distcheck`,
  including tests, vet, packaged binary checks and SHA256 checksums.
* Added automatic draft releases for annotated tags and nightly prereleases
  for lightweight tags, with version checks against `.version`.
* Added CI builds, tests and vet checks, daily archive checks and IRC notifications.
* Included documentation and dependency license texts in binary archives.
* Documented block matching, comment handling, whitespace classification,
  word diffs and test guarantees.

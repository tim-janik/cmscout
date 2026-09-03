# cmscout

cmscout matches code blocks across languages (functions, classes, methods, JSX elements, string templates, imports, constants) and diffs per block.
A renamed method shows as one changed unit instead of a delete plus an add; a moved function keeps its block identity.

Parsing uses tree-sitter. A staged hierarchical matcher then pairs blocks by name, kind, text similarity, and scope ancestry.
The output is deterministic: the same input always produces the same report.

## When to use it

Use cmscout for stable block diffs across code motion: a block moves within a file, or a rewrite reidentifies the same constructs in another language.

Line diffs (diff -u) treat every line independently. When a method moves, reorders, changes parameters, or alters its body, the change drowns in added and removed lines.
cmscout pairs the blocks first, re-identifies the method's before and after versions, then renders line or word diffs for just the parts that changed.

cmscout matches structural blocks in TS/TSX, JS/JSX, Go, Bash, C, and C++ and renders the change per block: similarity percentages, word-level intra-line changes, and `[changed]`/`[moved]`/`[converted]`/`[whitespace]` tags where applicable.

## How it works

The pipeline has several stages (detailed in [doc/pipeline.md](doc/pipeline.md)):
- AST parsing via [tree-sitter](https://tree-sitter.github.io/)
- Extraction of `SemanticBlock`s like functions, methods, classes, interfaces, JSX elements, string templates, imports, etc.
- Matching of semantic blocks in hierarchical stages by *name and kind*, *comment text*, *distance tables*
- Collapsing replaces matched children inside matched parents with canonical reference comments (`// [matched: method foo]`), so the parent's diff stays small. See [doc/canonical-references.md](doc/canonical-references.md).
- Prefix attachment merges doc-comment runs directly before a component into that component's diff. See [doc/prefix-comments.md](doc/prefix-comments.md) and [doc/enclosed-comments.md](doc/enclosed-comments.md).
- Word diff splits each changed line into words, diffs them at word granularity, and merges long adjacent runs into spans. See [doc/word-diff.md](doc/word-diff.md).
- Reporting renders one section per kind (imports, constants, classes, methods, functions, lifecycle, jsx, comments) and a summary.
- Each block header shows a similarity percentage and the source code, with `[changed]`, `[moved]`, `[converted]`, and `[whitespace]` tags where they apply. See [doc/whitespace-classification.md](doc/whitespace-classification.md).

### Supported languages

Detection is extension-based and case-insensitive: `.ts`, `.tsx`, `.js`, `.mjs`, `.cjs`, `.jsx`, `.go`, `.sh`, `.bash`, `.c`, and the C++ family `.cc`, `.cpp`, `.cxx`, `.c++`, `.hh`, `.hpp`, `.hxx`, `.h++`, `.tcc`. Legacy `.C` maps to C++.
For `.h` files, both C and C++ grammars run and the parse with fewer errors wins, so pure-C headers use C while C++ headers stay correct.
C and C++ split function-like macros into their own block kind.

### Line coverage

Every non-empty line of the before and after input files appears in the report output.
Files in unsupported languages or with no semantic blocks fall back to a whole-file line diff.

### Output example

Real output, excerpted (Imports, Methods, Lifecycle, Templates, and Other sections omitted):

```
diff --cmscout testdata/old/knob.tsx testdata/new/knob.tsx
Constants
@@ -4,1 +4,1 @@  VERSION  98% similarity  [changed]
-export const VERSION = '1.0.0';
+export const VERSION = '1.0.1';

Classes
@@ -6,37 +6,38 @@  Knob  93% similarity  [changed]
 export class Knob extends LitElement {
   static styles = css`
-    .knob { width: 100px; height: 100px; }
+    .knob { width: 120px; height: 120px; }
   `;
   private last_ = 0;
   private delta = 0;
   private enabled = true;
+  private snapped = false;
   // [matched: lifecycle connectedCallback]
   // [matched: lifecycle disconnectedCallback]
-  updated() {
-    this.updateDelta();
+  connectedCallbackUpdated() {
+    this.handleUpdate();
   }
   // [matched: method handleUpdate]
   // [matched: method notifyValueChanged]
   // [matched: method render]
 }

Summary
  Added:     1
  Removed:   1
  Renamed:   2
  Changed:   4
  Matched:   10
  Unchanged: 6
```


## Install

There are no prebuilt binaries; build from source.
Requires Go 1.25+ and a C compiler.
Tree-sitter grammars are C libraries, so builds need `CGO_ENABLED=1`, which the Makefile sets.

```sh
git clone https://github.com/tim-janik/cmscout
cd cmscout
make build		# produces ./cmscout
make test		# full test suite
make run		# runs cmscout on testdata/ fixture files
make vet
```

or from the clone:

```sh
CGO_ENABLED=1 go install ./cmd/cmscout
```


### Git integration

Use the included `git-diff-wrapper.sh` to replace git's default diff for a file:

```sh
GIT_EXTERNAL_DIFF=/path/to/git-diff-wrapper.sh git diff -- <file>
git -c diff.external=/path/to/git-diff-wrapper.sh log --ext-diff -p
```

The wrapper respects `NO_COLOR`, `CMCSOUT_WORD_DIFF`, `CMCSOUT_ADDED_STYLE`, `CMCSOUT_REMOVED_STYLE`, and `CMCSOUT_KEEP_UNCHANGED` environment variables. See the script for details.

### Single-file statistics

`cmscout --stats <file>` parses one file and prints per-semantic-block statistics:
block size in lines/chars, the doc-comment prefix size (the "prefix command"),
per-comment sizes with an over-length flag (`--max-comment-lines`, default 5),
container method counts, and a best-effort branch count as a cyclomatic
complexity precursor. Every record carries the file name and line span, and
comment records carry the fully qualified function name — the data points are
designed for later linting rules and for assessing whether patches increase or
decrease block complexity. See [doc/stats.md](doc/stats.md).

```sh
cmscout --stats --max-comment-lines 3 testdata/new/knob.tsx
```

## License

MPL-2.0, see [LICENSE](LICENSE).

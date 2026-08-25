# cmdiff

cmdiff matches code blocks across languages (functions, classes, methods, JSX elements, string templates, imports, constants) and diffs per block.
A renamed method shows as one changed unit instead of a delete plus an add; a moved function keeps its block identity.

Under the hood, tree-sitter is used for parsing, then a staged hierarchical matcher pairs blocks by name, kind, text similarity, and scope ancestry.
The tool correlates semantic entities and renders deterministic before/after views.

## When to use it

cmdiff is useful for stable block diffs across code motion: semantic block movements within a file, or cross-language reidentification of semantic constructs when the before/after files are part of a rewrite.

Line diffs (diff -u) treat every line independently, so when a method moves or is reordered within a file and has its parameters changed or some body alteration, changes become invisible in a slur of added / removed lines.
cmdiff pairs the blocks first, re-identifies the method's before and after versions in a pair, and can render precise line or word diffs for just the method parts that actually changed.

cmdiff matches structural blocks in TS/TSX, JS/JSX, Go, Bash, C, and C++ and renders the change per block: similarity percentages, word-level intra-line changes, and `[moved]`/`[converted]`/`[whitespace]` tags where applicable.

## How it works

The pipeline has several stages (detailed in [doc/pipeline.md](doc/pipeline.md)):
- AST parsing via [tree-sitter](https://tree-sitter.github.io/)
- Extraction of `SemanticBlock`s like functions, methods, classes, interfaces, JSX elements, string templates, imports, etc.
- Matching of semantic blocks in hierarchical stages by *name and kind*, *comment text*, *distance tables*
- Collapsing replaces matched children inside matched parents with canonical reference comments (`// [matched: method foo]`), so the parent's diff stays small. See [doc/canonical-references.md](doc/canonical-references.md).
- Prefix attachment for doc-comment runs directly before a component, so the comments become part of that component's diff, see also [doc/prefix-comments.md](doc/prefix-comments.md) and [doc/enclosed-comments.md](doc/enclosed-comments.md).
- Word diff for paired blocks, this splits each changed line into words and diffs them at word granularity and consolidates too long adjacent runs into spans, see [doc/word-diff.md](doc/word-diff.md).
- Reporting renders one section per kind (imports, constants, classes, methods, functions, lifecycle, jsx, comments) and a summary
- Each block shows a header with similarity percentage and its source code. `[moved]`, `[converted]` and `[whitespace]` tags mark the blocks they apply to, see also [doc/whitespace-classification.md](doc/whitespace-classification.md).

### Supported languages

Detection is extension-based and case-insensitive: `.ts`, `.tsx`, `.js`, `.mjs`, `.cjs`, `.jsx`, `.go`, `.sh`, `.bash`, `.c`, and the C++ family `.cc`, `.cpp`, `.cxx`, `.c++`, `.hh`, `.hpp`, `.hxx`, `.h++`, `.tcc`, with legacy `.C` mapping to C++
and `.h` runs both C and C++ grammars and keeps the parse with fewer errors, so pure-C headers use C while C++ headers stay correct.
C and C++ additionally split off function-like macros as their own block kind.

### Line Coverage

Every non-empty line of the before and after input files appears in the report output.
Files in unsupported languages or with no semantic blocks fall back to a whole-file line diff.

### Output example

Real output, excerpted (Imports, Methods, Lifecycle, Templates, and Other sections omitted):

```
diff --cmdiff testdata/old/knob.tsx testdata/new/knob.tsx
Constants
@@ -4,1 +4,1 @@  VERSION  98% similarity
-export const VERSION = '1.0.0';
+export const VERSION = '1.0.1';

Classes
@@ -6,37 +6,38 @@  Knob  93% similarity
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
git clone https://github.com/tim-janik/cmdiff
cd cmdiff
make build		# produces ./cmdiff
make test		# full test suite
make run		# runs cmdiff on testdata/ fixture files
make vet
```

or from the clone:

```sh
CGO_ENABLED=1 go install ./cmd/cmdiff
```


### Git integration

Use the included `git-diff-wrapper.sh` to replace git's default diff for a file:

```sh
GIT_EXTERNAL_DIFF=/path/to/git-diff-wrapper.sh git diff -- <file>
git -c diff.external=/path/to/git-diff-wrapper.sh log --ext-diff -p
```

The wrapper respects `NO_COLOR`, `CMDIFF_WORD_DIFF`, `CMDIFF_ADDED_STYLE`, `CMDIFF_REMOVED_STYLE`, and `CMDIFF_KEEP_UNCHANGED` environment variables. See the script for details.


## License

MPL-2.0, see [LICENSE](LICENSE).

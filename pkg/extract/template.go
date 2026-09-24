// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

// Lit html templates have no JSX-like AST node: this scanner emits one KindTemplate block
// per top-level element (${...} skipped as atomic, mismatched markup skipped conservatively).

package extract

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"cmdiff/pkg/ir"
)

// isHtmlTaggedTemplate reports whether the template_string node is the
// argument of a bare `html` tagged-template call (the Lit convention).
func isHtmlTaggedTemplate(node *tree_sitter.Node, src []byte) bool {
	parent := node.Parent()
	if parent == nil || parent.Kind() != "call_expression" {
		return false
	}
	tag := parent.ChildByFieldName("function")
	if tag == nil {
		for i := uint(0); i < parent.ChildCount(); i++ {
			if c := parent.Child(i); c != nil && c.Kind() == "identifier" {
				tag = c
				break
			}
		}
	}
	return tag != nil && tag.Kind() == "identifier" && tag.Utf8Text(src) == "html"
}

// extractTemplateElements emits one parentless KindTemplate block per top-level element.
func (e *Extractor) extractTemplateElements(node *tree_sitter.Node, src []byte, blocks *[]ir.SemanticBlock) {
	text := node.Utf8Text(src) // includes the surrounding backticks
	if len(text) < 2 || text[0] != '`' {
		return
	}
	bodyEnd := len(text)
	if text[bodyEnd-1] == '`' {
		bodyEnd--
	}
	body := text[1:bodyEnd]

	startPos := node.StartPosition()
	startByte := node.StartByte()
	for _, el := range templateElements(body) {
		elText := body[el[0]:el[1]]
		name, _ := readTagName(body, el[0]+1)
		sr, sc := offsetPosition(startPos.Row, startPos.Column, body[:el[0]])
		er, ec := offsetPosition(startPos.Row, startPos.Column, body[:el[1]])
		*blocks = append(*blocks, *e.newBlock(ir.KindTemplate, name, elText,
			ir.SourceSpan{
				StartByte: startByte + uint(el[0]) + 1,
				EndByte:   startByte + uint(el[1]) + 1,
				StartLine: sr,
				StartCol:  sc,
				EndLine:   er,
				EndCol:    ec,
			}, ""))
	}
}

// templateElements finds top-level element offsets; ${...} spans are skipped atomically.
func templateElements(body string) [][2]int {
	var out [][2]int
	i := 0
	for i < len(body) {
		if body[i] == '$' && i+1 < len(body) && body[i+1] == '{' {
			i = skipInterpolation(body, i)
			continue
		}
		if body[i] != '<' || i+1 >= len(body) || !isTagStartByte(body[i+1]) {
			i++
			continue
		}
		if start, end, ok := scanElement(body, i); ok {
			out = append(out, [2]int{start, end})
			i = end
		} else {
			i++
		}
	}
	return out
}

// scanElement scans one element with a tag-name stack for nesting; mismatched markup yields ok=false.
func scanElement(body string, i int) (int, int, bool) {
	name, j := readTagName(body, i+1)
	if name == "" {
		return 0, 0, false
	}
	j, selfClosing := skipOpenTag(body, j)
	if selfClosing {
		return i, j, true
	}
	stack := []string{name}
	k := j
	for k < len(body) {
		switch {
		case body[k] == '<' && k+1 < len(body) && body[k+1] == '/':
			closeName, m := readTagName(body, k+2)
			m = skipToGt(body, m)
			if closeName != "" && len(stack) > 0 && stack[len(stack)-1] == closeName {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return i, m, true
				}
			}
			k = m
		case body[k] == '<' && k+1 < len(body) && isTagStartByte(body[k+1]):
			nested, m := readTagName(body, k+1)
			if nested == "" {
				k++
				continue
			}
			m, selfClose := skipOpenTag(body, m)
			if !selfClose {
				stack = append(stack, nested)
			}
			k = m
		case body[k] == '$' && k+1 < len(body) && body[k+1] == '{':
			k = skipInterpolation(body, k)
		default:
			k++
		}
	}
	return 0, 0, false
}

// skipOpenTag scans past the opening tag's '>' (or self-closing '/'), skipping quotes and ${...}.
func skipOpenTag(body string, j int) (int, bool) {
	k := j
	for k < len(body) {
		switch body[k] {
		case '"', '\'':
			k = skipQuotedAttr(body, k)
		case '$':
			if k+1 < len(body) && body[k+1] == '{' {
				k = skipInterpolation(body, k)
				continue
			}
			k++
		case '>':
			return k + 1, false
		case '/':
			if k+1 < len(body) && body[k+1] == '>' {
				return k + 2, true
			}
			k++
		default:
			k++
		}
	}
	return len(body), false
}

// skipQuotedAttr scans a quoted attribute value, skipping ${...} (which may contain the quote char).
func skipQuotedAttr(body string, j int) int {
	q := body[j]
	k := j + 1
	for k < len(body) {
		switch {
		case body[k] == '\\':
			k += 2
		case body[k] == q:
			return k + 1
		case body[k] == '$' && k+1 < len(body) && body[k+1] == '{':
			k = skipInterpolation(body, k)
		default:
			k++
		}
	}
	return len(body)
}

// skipInterpolation scans a ${...} span to its closing '}', tracking brace depth and strings.
func skipInterpolation(body string, j int) int {
	depth := 1
	k := j + 2
	for k < len(body) && depth > 0 {
		switch body[k] {
		case '{':
			depth++
		case '}':
			depth--
		case '"', '\'':
			q := body[k]
			k++
			for k < len(body) && body[k] != q {
				if body[k] == '\\' {
					k++
				}
				k++
			}
			if k < len(body) {
				k++ // closing quote
			}
			continue
		}
		k++
	}
	return k
}

// skipToGt returns the index just past the next '>' (used for closing tags).
func skipToGt(body string, j int) int {
	for j < len(body) && body[j] != '>' {
		j++
	}
	if j < len(body) {
		return j + 1
	}
	return len(body)
}

// readTagName reads an element tag name starting at body[i].
func readTagName(body string, i int) (string, int) {
	j := i
	for j < len(body) && isTagNameByte(body[j]) {
		j++
	}
	return body[i:j], j
}

func isTagStartByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isTagNameByte(b byte) bool {
	return isTagStartByte(b) || b >= '0' && b <= '9' || b == '-' || b == '_' || b == ':'
}

// offsetPosition advances a base position over prefix text (element positions inside a template).
func offsetPosition(row, col uint, prefix string) (uint, uint) {
	nl := strings.Count(prefix, "\n")
	if nl == 0 {
		return row, col + uint(len(prefix))
	}
	last := strings.LastIndex(prefix, "\n")
	return row + uint(nl), uint(len(prefix) - last - 1)
}

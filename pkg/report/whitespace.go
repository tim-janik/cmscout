// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package report

import (
	"strings"

	"cmdiff/pkg/ir"
)

// lexicalTokens: language-neutral tokenization for whitespace-only classification —
// strings/comments/regexes/template chunks atomic, ${...} recursive, operators longest-first.
func lexicalTokens(s string) []string {
	return lexicalTokensForLanguage(s, "")
}

// lexicalTokensForLanguage applies language-specific literal rules.
func lexicalTokensForLanguage(s, language string) []string {
	var tokens []string
	i := 0
	n := len(s)
	last := "" // last emitted token, used to tell regex literals from division
	for i < n {
		c := s[i]
		switch {
		case isSpaceByte(c):
			i++
		case (language == "c" || language == "cpp") && cStringPrefixLen(s, i) > 0:
			j := skipCQuoted(s, i)
			if language == "cpp" {
				j = skipCppLiteralSuffix(s, j)
			}
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '"' || c == '\'':
			j := skipQuoted(s, i)
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '`' && language == "go":
			// Go raw strings: whitespace inside is source content, not formatting.
			j := skipRawBacktick(s, i)
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '`' && language == "bash":
			// Bash backtick substitutions stay atomic: changes inside them are not formatting.
			j := skipQuoted(s, i)
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '`' && (language == "c" || language == "cpp"):
			// Treat C/C++ backticks as punctuation.
			tokens = append(tokens, s[i:i+1])
			last = s[i : i+1]
			i++
		case c == '`':
			i = scanTemplate(s, i, &tokens, &last, language)
		case language == "cpp" && cppRawStringPrefixLen(s, i) > 0:
			// Keep C++ raw strings atomic.
			j := skipCppRawString(s, i)
			j = skipCppLiteralSuffix(s, j)
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '#' && ((language == "bash" && isBashCommentStart(s, i)) ||
			(language != "bash" && language != "c" && language != "cpp" &&
				(i+1 >= n || isSpaceByte(s[i+1])))):
			// Bash comments run to the end of the line as one atomic token.
			j := i
			for j < n && s[j] != '\n' {
				j++
			}
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '/' && i+1 < n && s[i+1] == '/':
			// Line comment: one token to the end of the line.
			j := i
			for j < n && s[j] != '\n' {
				j++
			}
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '/' && i+1 < n && s[i+1] == '*':
			// Block comment: one token through the closing delimiter.
			j := i + 2
			for j+1 < n && !(s[j] == '*' && s[j+1] == '/') {
				j++
			}
			if j+1 < n {
				j += 2
			} else {
				j = n
			}
			tokens = append(tokens, s[i:j])
			last = s[i:j]
			i = j
		case c == '/' && regexCanFollow(last):
			// Regex literal (JS/TS, Bash after =~): atomic token through flags; unterminated ⇒ punctuation.
			if end := scanRegex(s, i); end >= 0 {
				tokens = append(tokens, s[i:end])
				last = s[i:end]
				i = end
				continue
			}
			fallthrough
		default:
			// Split byte runs into identifier runs, multi-char operators, and single punctuation.
			runStart := i
			for i < n {
				c = s[i]
				if isSpaceByte(c) || c == '"' || c == '\'' || c == '`' ||
					(c == '/' && i+1 < n && (s[i+1] == '/' || s[i+1] == '*')) ||
					((language == "c" || language == "cpp") && i > runStart && !isIdentByte(s[i-1]) &&
						(cppRawStringPrefixLen(s, i) > 0 || cStringPrefixLen(s, i) > 0)) {
					break
				}
				i++
			}
			for _, tok := range splitRunTokens(s[runStart:i]) {
				tokens = append(tokens, tok)
				last = tok
			}
		}
	}
	return tokens
}

// isBashCommentStart reports whether # starts a Bash comment at i.
func isBashCommentStart(s string, i int) bool {
	if i == 0 || isSpaceByte(s[i-1]) {
		return true
	}
	switch s[i-1] {
	case ';', '|', '&':
		return true
	default:
		return false
	}
}

// skipQuoted: index past the closing quote (honoring backslash escapes); len(s) if unterminated.
func skipQuoted(s string, i int) int {
	q := s[i]
	j := i + 1
	for j < len(s) {
		if s[j] == '\\' {
			j += 2
			continue
		}
		if s[j] == q {
			return j + 1
		}
		j++
	}
	return len(s)
}

// skipRawBacktick: Go raw strings close at the next backtick (no backslash escapes).
func skipRawBacktick(s string, i int) int {
	if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
		return i + 1 + end + 1
	}
	return len(s)
}

// cStringPrefixLen recognizes C/C++ encoded literal prefixes.
func cStringPrefixLen(s string, i int) int {
	for _, prefix := range []string{"u8", "u", "U", "L"} {
		if strings.HasPrefix(s[i:], prefix+`"`) || strings.HasPrefix(s[i:], prefix+`'`) {
			return len(prefix)
		}
	}
	return 0
}

func skipCQuoted(s string, i int) int {
	prefixLen := cStringPrefixLen(s, i)
	if prefixLen == 0 {
		return min(len(s), i+1)
	}
	return skipQuoted(s, i+prefixLen)
}

func skipCppLiteralSuffix(s string, i int) int {
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	return i
}

// cppRawStringPrefixLen recognizes C++ raw-literal prefixes.
func cppRawStringPrefixLen(s string, i int) int {
	for _, prefix := range []string{"u8R", "uR", "UR", "LR", "R"} {
		if strings.HasPrefix(s[i:], prefix+`"`) {
			return len(prefix)
		}
	}
	return 0
}

// skipCppRawString returns the end of one C++ raw string token.
func skipCppRawString(s string, i int) int {
	// s[i:] starts with R" or one of the encoding-prefixed forms.
	n := len(s)
	prefixLen := cppRawStringPrefixLen(s, i)
	if prefixLen == 0 {
		return min(n, i+1)
	}
	p := i + prefixLen + 1 // past the opening quote
	openParen := strings.IndexByte(s[p:], '(')
	if openParen < 0 {
		return n // malformed; keep the rest atomic
	}
	delim := s[p : p+openParen]
	contentStart := p + openParen + 1 // past '('
	close := ")" + delim + "\""
	idx := strings.Index(s[contentStart:], close)
	if idx < 0 {
		return n
	}
	return contentStart + idx + len(close)
}

// scanTemplate: one token per text chunk (atomic), ${...} tokenized recursively;
// returns the index past the closing backtick (or len(s) when unterminated).
func scanTemplate(s string, i int, tokens *[]string, last *string, language string) int {
	n := len(s)
	chunkStart := i
	j := i + 1
	for j < n {
		switch s[j] {
		case '\\':
			j += 2
		case '`':
			*tokens = append(*tokens, s[chunkStart:j+1])
			*last = s[chunkStart : j+1]
			return j + 1
		case '$':
			if j+1 < n && s[j+1] == '{' {
				*tokens = append(*tokens, s[chunkStart:j+2])
				*last = s[chunkStart : j+2]
				exprStart := j + 2
				k := exprStart
				depth := 1
				for k < n && depth > 0 {
					switch s[k] {
					case '\\':
						k += 2
					case '"', '\'', '`':
						k = skipQuoted(s, k)
					case '{':
						depth++
						k++
					case '}':
						depth--
						k++
					default:
						k++
					}
				}
				if depth > 0 {
					// Unterminated interpolation: remainder stays one atomic chunk (conservative).
					*tokens = append(*tokens, s[exprStart:])
					*last = s[exprStart:]
					return n
				}
				for _, tok := range lexicalTokensForLanguage(s[exprStart:k-1], language) {
					*tokens = append(*tokens, tok)
					*last = tok
				}
				j = k
				chunkStart = k
				continue
			}
			j++
		default:
			j++
		}
	}
	*tokens = append(*tokens, s[chunkStart:])
	*last = s[chunkStart:]
	return n
}

// scanRegex: one atomic token through the closing slash and flags; -1 if unterminated before end of line.
func scanRegex(s string, i int) int {
	n := len(s)
	j := i + 1
	for j < n {
		switch s[j] {
		case '\\':
			j += 2
		case '[':
			// Character class: '/' inside does not close the literal.
			j++
			for j < n {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == ']' {
					j++
					break
				}
				j++
			}
		case '/':
			j++
			// Trailing flags are letters in JS/TS (and none in Go).
			for j < n && isIdentByte(s[j]) {
				j++
			}
			return j
		case '\n':
			return -1 // regex literals cannot span lines
		default:
			j++
		}
	}
	return -1
}

// regexCanFollow: '/' opens a regex after operators/keywords; uncertain cases stay punctuation (errs toward Changed).
func regexCanFollow(last string) bool {
	if last == "" || strings.HasPrefix(last, "//") || strings.HasPrefix(last, "/*") {
		return true
	}
	_, ok := regexFollowSet[last]
	return ok
}

// regexFollowSet: tokens after which '/' starts a regex (operators, opening punctuation,
// expression-head keywords); operands are absent so `a / b` is division.
var regexFollowSet = map[string]bool{
	"=": true, "=>": true, "==": true, "===": true, "!=": true, "!==": true,
	"<": true, ">": true, "<=": true, ">=": true, "<<": true, ">>": true,
	"(": true, "[": true, "{": true, ",": true, ";": true, ":": true,
	"!": true, "&": true, "&&": true, "&&=": true, "|": true, "||": true,
	"||=": true, "?": true, "??": true, "??=": true, "?.": true,
	"+": true, "-": true, "*": true, "**": true, "**=": true,
	"%": true, "^": true, "~": true, "++": true, "--": true,
	"+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "::": true, "->": true,
	"<-": true, "=~": true, "...": true,
	"return": true, "case": true, "typeof": true, "instanceof": true,
	"in": true, "of": true, "new": true, "do": true, "else": true,
	"yield": true, "await": true, "delete": true, "void": true,
	"throw": true, "default": true,
}

// multiCharOperators lists operators from longest to shortest.
var multiCharOperators = []string{
	">>>=", ">>>", ">>=", "<<=", "<<<", "&&=", "||=", "??=", "**=", "->*", "<=>", "%:%:", "##",
	"===", "!==", "...", ";;&",
	">>", "<<", "&&", "||", "??", "?.", "++", "--", "**", ".*", "<:", ":>", "<%", "%>", "%:",
	"==", "!=", "<=", ">=", "=>", "::", "<-", "->", ":=",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "=~", ";;", ";&",
}

// matchMultiChar returns the longest multi-character operator at the start
// of run, or "" when run starts with single-byte punctuation.
func matchMultiChar(run string) string {
	for _, op := range multiCharOperators {
		if strings.HasPrefix(run, op) {
			return op
		}
	}
	return ""
}

// splitRunTokens: identifier runs, multi-char operators (longest first), single punctuation.
func splitRunTokens(run string) []string {
	var out []string
	for len(run) > 0 {
		if isIdentByte(run[0]) {
			j := 1
			for j < len(run) && isIdentByte(run[j]) {
				j++
			}
			out = append(out, run[:j])
			run = run[j:]
			continue
		}
		if op := matchMultiChar(run); op != "" {
			out = append(out, op)
			run = run[len(op):]
			continue
		}
		out = append(out, run[:1])
		run = run[1:]
	}
	return out
}

// isIdentByte reports whether b can be part of an identifier or number.
func isIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_' || b == '$'
}

// equalTokenSequences reports whether two token sequences are identical.
func equalTokenSequences(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isWhitespaceOnlyMatch: equal lexical token sequences = formatting-only; changes inside
// strings/comments/regexes/template text or at token boundaries are semantic.
func isWhitespaceOnlyMatch(p *ir.CorrelatedPair) bool {
	return isWhitespaceOnlyMatchForLanguages(p, "", "")
}

// isWhitespaceOnlyMatchForLanguages compares language-specific tokens.
func isWhitespaceOnlyMatchForLanguages(p *ir.CorrelatedPair, oldLanguage, newLanguage string) bool {
	if p.Old == nil || p.New == nil {
		return false
	}
	if p.Old.Source == p.New.Source {
		return false // identical, not a change at all
	}
	return equalTokenSequences(
		lexicalTokensForLanguage(p.Old.Source, oldLanguage),
		lexicalTokensForLanguage(p.New.Source, newLanguage),
	)
}

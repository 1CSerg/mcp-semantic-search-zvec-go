package redact

import (
	"path/filepath"
	"strings"
	"unicode"
)

// SanitizeErrorText removes absolute filesystem paths from public error/message fields.
func SanitizeErrorText(s string) string {
	if s == "" {
		return s
	}
	return sanitizeTokens(s)
}

func sanitizeTokens(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		start, kind := scanPathStart(s, i)
		if kind == pathNone {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString(s[i:start])
		end := pathEnd(s, start, kind)
		b.WriteString("<redacted>")
		i = end
	}
	return b.String()
}

type pathKind int

const (
	pathNone pathKind = iota
	pathUnix
	pathWindows
	pathUNC
)

func scanPathStart(s string, i int) (int, pathKind) {
	if i+1 < len(s) && s[i] == '\\' && s[i+1] == '\\' {
		return i, pathUNC
	}
	if i+1 < len(s) && ((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) && s[i+1] == ':' {
		if i+2 < len(s) && (s[i+2] == '\\' || s[i+2] == '/') {
			return i, pathWindows
		}
	}
	if s[i] == '/' && i+1 < len(s) && (unicode.IsLetter(rune(s[i+1])) || s[i+1] == '_') {
		return i, pathUnix
	}
	return i, pathNone
}

func pathEnd(s string, start int, kind pathKind) int {
	if kind == pathNone {
		return start + 1
	}
	i := start
	for i < len(s) {
		c := s[i]
		if c == '"' || c == '\'' || c == ';' || c == ',' || c == ')' || c == ']' {
			break
		}
		i++
	}
	for i > start && !isAbsPathPrefix(s[start:i], kind) {
		i--
	}
	if i > start && isAbsPathPrefix(s[start:i], kind) {
		return i
	}
	return start + 1
}

func isAbsPathPrefix(s string, kind pathKind) bool {
	switch kind {
	case pathUnix:
		return isUnixAbsPath(s)
	default:
		return filepath.IsAbs(s)
	}
}

func isUnixAbsPath(s string) bool {
	if s == "" || s[0] != '/' {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '/', '-', '_', '.', ' ':
		default:
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= '0' && s[i] <= '9') {
				continue
			}
			if s[i] > 127 {
				continue
			}
			return false
		}
	}
	return true
}

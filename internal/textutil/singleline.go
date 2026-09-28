package textutil

import (
	"strings"
	"unicode"
)

// SingleLine collapses line breaks and consecutive whitespace into single spaces,
// trimming leading and trailing whitespace. This prevents multi-line strings from
// corrupting single-line terminal components like status bars.
func SingleLine(s string) string {
	var b strings.Builder
	inSpace := false

	for _, r := range s {
		if r == '\r' || r == '\n' || r == '\t' || unicode.IsSpace(r) {
			if !inSpace && b.Len() > 0 {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		b.WriteRune(r)
		inSpace = false
	}

	return strings.TrimSpace(b.String())
}

// Package textutil provides display-width-aware string helpers (CJK/wide
// character handling), truncation/wrapping, and human-readable age
// formatting. These are pure functions with no external dependencies so
// they can be unit tested in isolation and reused across the TUI.
package textutil

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/width"
)

// CharWidth returns the number of terminal columns a single rune occupies:
// 0 for combining marks, 2 for East-Asian wide/fullwidth runes, 1 otherwise.
func CharWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) { // non-spacing combining mark
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	default:
		return 1
	}
}

// DisplayWidth returns the total terminal column width of s.
func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += CharWidth(r)
	}
	return w
}

// Truncate shortens s so its display width fits within maxCols columns,
// appending an ellipsis when truncation occurs. Correctly handles wide runes.
func Truncate(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(s, "")
	if DisplayWidth(s) <= maxCols {
		return s
	}
	budget := maxCols - 1 // reserve one column for the ellipsis
	out := make([]rune, 0, len(s))
	used := 0
	for _, r := range s {
		cw := CharWidth(r)
		if used+cw > budget {
			break
		}
		out = append(out, r)
		used += cw
	}
	return string(out) + "…"
}

// TruncateRunes truncates s to at most maxRunes unicode code points, guaranteeing
// that multi-byte UTF-8 sequences are never split and the result is strictly valid UTF-8.
// If truncated and appendEllipsis is true, "…" is appended.
func TruncateRunes(s string, maxRunes int, appendEllipsis bool) string {
	s = strings.ToValidUTF8(s, "")
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if appendEllipsis {
		return string(runes[:maxRunes]) + "…"
	}
	return string(runes[:maxRunes])
}


// PadToWidth truncates s to cols columns, then right-pads with spaces so the
// result occupies exactly cols display columns.
func PadToWidth(s string, cols int) string {
	t := Truncate(s, cols)
	pad := cols - DisplayWidth(t)
	if pad > 0 {
		return t + spaces(pad)
	}
	return t
}

// WrapByWidth wraps text into lines each fitting within maxCols display
// columns. It wraps on spaces when possible and breaks mid-run for long
// unspaced text (e.g. Chinese), keeping wide runes intact.
func WrapByWidth(text string, maxCols int) []string {
	if maxCols <= 0 {
		return []string{text}
	}
	var lines []string
	current := ""
	currentW := 0

	flush := func() {
		lines = append(lines, current)
		current = ""
		currentW = 0
	}

	for _, word := range fields(text) {
		wordW := DisplayWidth(word)
		sepW := 0
		if current != "" {
			sepW = 1
		}
		if current != "" && currentW+sepW+wordW > maxCols {
			flush()
			sepW = 0
		}
		if wordW <= maxCols {
			if current != "" {
				current += " " + word
				currentW += sepW + wordW
			} else {
				current = word
				currentW = wordW
			}
			continue
		}
		// Word wider than a line: break by runes.
		if current != "" {
			flush()
		}
		for _, r := range word {
			cw := CharWidth(r)
			if currentW+cw > maxCols {
				flush()
			}
			current += string(r)
			currentW += cw
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// FormatAge returns a compact human-readable age like "2h ago" or "3d ago"
// relative to now. Returns "just now" for very recent times.
func FormatAge(t, now time.Time) string {
	if t.IsZero() {
		return "?"
	}
	secs := int(now.Sub(t).Seconds())
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs < 60:
		return "just now"
	case secs < 3600:
		return fmt.Sprintf("%dm ago", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh ago", secs/3600)
	case secs < 86400*30:
		return fmt.Sprintf("%dd ago", secs/86400)
	case secs < 86400*365:
		return fmt.Sprintf("%dmo ago", secs/(86400*30))
	default:
		return fmt.Sprintf("%dy ago", secs/(86400*365))
	}
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

// fields splits on runs of whitespace (like strings.Fields) without importing
// strings to keep intent explicit; kept simple and unicode-aware.
func fields(s string) []string {
	var out []string
	start := -1
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsSpace(r) {
			if start >= 0 {
				out = append(out, string(runes[start:i]))
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, string(runes[start:]))
	}
	return out
}

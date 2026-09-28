package textutil

import (
	"testing"
	"time"
)

func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"你好", 4},      // two wide runes
		{"a你b", 4},     // 1 + 2 + 1
		{"café", 4},    // é is narrow
		{"日本語テスト", 12}, // 6 wide runes
	}
	for _, c := range cases {
		if got := DisplayWidth(c.in); got != c.want {
			t.Errorf("DisplayWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		cols int
		want int // expected display width of result (<= cols)
	}{
		{"hello world", 5, 5},
		{"hello", 10, 5},
		{"你好世界", 5, 5}, // 2 wide chars = 4 + ellipsis = 5
		{"abc", 0, 0},
	}
	for _, c := range cases {
		got := Truncate(c.in, c.cols)
		if w := DisplayWidth(got); w > c.cols {
			t.Errorf("Truncate(%q,%d)=%q width=%d exceeds %d", c.in, c.cols, got, w, c.cols)
		}
	}
	if Truncate("hello", 10) != "hello" {
		t.Error("Truncate should not modify strings that fit")
	}

	// Test invalid UTF-8 sanitization in Truncate
	invalidUTF8 := string([]byte{0xff, 0xfe, 'a', 'b'})
	sanitized := Truncate(invalidUTF8, 10)
	if sanitized != "ab" {
		t.Errorf("Truncate didn't sanitize invalid UTF-8: got %q, want %q", sanitized, "ab")
	}
}

func TestTruncateRunes(t *testing.T) {
	// Chinese characters: each rune is 3 bytes.
	// Raw byte slicing "你好世界"[:4] would cut the second rune in half.
	text := "你好世界"
	gotWithEllipsis := TruncateRunes(text, 2, true)
	if gotWithEllipsis != "你好…" {
		t.Errorf("TruncateRunes with ellipsis: got %q, want %q", gotWithEllipsis, "你好…")
	}

	gotWithoutEllipsis := TruncateRunes(text, 2, false)
	if gotWithoutEllipsis != "你好" {
		t.Errorf("TruncateRunes without ellipsis: got %q, want %q", gotWithoutEllipsis, "你好")
	}

	// If within limit, returns unmodified string
	if got := TruncateRunes(text, 10, true); got != text {
		t.Errorf("TruncateRunes within limit: got %q, want %q", got, text)
	}

	// Test with invalid UTF-8 byte sequence
	invalid := string([]byte{'a', 0xe4, 0xbd, 'b'}) // incomplete 3-byte rune 0xe4 0xbd
	sanitized := TruncateRunes(invalid, 10, false)
	if sanitized != "ab" {
		t.Errorf("TruncateRunes should sanitize invalid UTF-8: got %q, want %q", sanitized, "ab")
	}
}

func TestPadToWidth(t *testing.T) {
	got := PadToWidth("hi", 5)
	if DisplayWidth(got) != 5 {
		t.Errorf("PadToWidth width = %d, want 5", DisplayWidth(got))
	}
	if got != "hi   " {
		t.Errorf("PadToWidth = %q, want 'hi   '", got)
	}
}

func TestWrapByWidth(t *testing.T) {
	// ASCII wrapping on spaces
	lines := WrapByWidth("the quick brown fox", 10)
	for _, l := range lines {
		if DisplayWidth(l) > 10 {
			t.Errorf("line %q exceeds 10 cols", l)
		}
	}
	// CJK with no spaces must still wrap (mid-run break)
	cjk := "你好世界这是一个测试文本内容"
	lines = WrapByWidth(cjk, 8)
	if len(lines) < 2 {
		t.Errorf("expected CJK text to wrap into multiple lines, got %d", len(lines))
	}
	for _, l := range lines {
		if DisplayWidth(l) > 8 {
			t.Errorf("CJK line %q width %d exceeds 8", l, DisplayWidth(l))
		}
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		delta time.Duration
		want  string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{45 * 24 * time.Hour, "1mo ago"},
		{400 * 24 * time.Hour, "1y ago"},
	}
	for _, c := range cases {
		got := FormatAge(now.Add(-c.delta), now)
		if got != c.want {
			t.Errorf("FormatAge(-%v) = %q, want %q", c.delta, got, c.want)
		}
	}
	if FormatAge(time.Time{}, now) != "?" {
		t.Error("zero time should format as ?")
	}
}

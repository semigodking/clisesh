package summarize

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDescribeOrPrompt(t *testing.T) {
	// Title fits and there are prompts -> use title, no LLM.
	desc, needs := DescribeOrPrompt(Item{Title: "Short title", Prompts: []string{"do a thing"}}, 80)
	if needs || desc != "Short title" {
		t.Errorf("short title: desc=%q needs=%v", desc, needs)
	}
	// Long title with prompts -> needs LLM.
	long := ""
	for i := 0; i < 100; i++ {
		long += "x"
	}
	_, needs = DescribeOrPrompt(Item{Title: long, Prompts: []string{"do a thing"}}, 80)
	if !needs {
		t.Error("long title should need LLM summarization")
	}
	// No prompts -> EmptySession, no LLM, even with a long title (decision 1a).
	desc, needs = DescribeOrPrompt(Item{Title: long}, 80)
	if needs || desc != EmptySession {
		t.Errorf("empty prompts w/ long title: desc=%q needs=%v", desc, needs)
	}
	// Completely empty item -> EmptySession, no LLM.
	desc, needs = DescribeOrPrompt(Item{}, 80)
	if needs || desc != EmptySession {
		t.Errorf("empty item: desc=%q needs=%v", desc, needs)
	}
	// CJK title width respected: 41 wide chars = 82 cols > 80 -> needs LLM.
	cjk := ""
	for i := 0; i < 41; i++ {
		cjk += "好"
	}
	_, needs = DescribeOrPrompt(Item{Title: cjk, Prompts: []string{"p"}}, 80)
	if !needs {
		t.Error("wide CJK title exceeding titleLen should need LLM")
	}
}

func TestIsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		prompts []string
		want    bool
	}{
		{"nil", nil, true},
		{"single empty", []string{""}, true},
		{"whitespace only", []string{"  \n "}, true},
		{"has content", []string{"hi"}, false},
	}
	for _, tc := range cases {
		if got := IsEmpty(tc.prompts); got != tc.want {
			t.Errorf("IsEmpty(%q) = %v, want %v", tc.prompts, got, tc.want)
		}
	}
}

func TestParseNumberedList(t *testing.T) {
	items := []Item{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	resp := "1: First task\n2. Second task\n> 3: Third task\n4: out of range"
	got := ParseNumberedList(resp, items)
	if got["a"] != "First task" {
		t.Errorf("a = %q", got["a"])
	}
	if got["b"] != "Second task" {
		t.Errorf("b = %q", got["b"])
	}
	if got["c"] != "Third task" { // blockquote stripped
		t.Errorf("c = %q", got["c"])
	}
	if len(got) != 3 {
		t.Errorf("out-of-range index should be ignored, got %d entries", len(got))
	}
}

func TestParseStripsQuotes(t *testing.T) {
	items := []Item{{ID: "a"}}
	got := ParseNumberedList(`1: "Quoted description"`, items)
	if got["a"] != "Quoted description" {
		t.Errorf("quotes not stripped: %q", got["a"])
	}
}

func TestRunBatchedMixesTitleAndLLM(t *testing.T) {
	items := []Item{
		{ID: "t", Title: "Has title", Prompts: []string{"some content"}}, // no LLM (title fits)
		{ID: "p", Prompts: []string{"summarize me please"}},              // needs LLM
	}
	called := 0
	llm := func(ctx context.Context, prompt string) (string, error) {
		called++
		// Only the LLM-needing item ("p") is in the batch at index 1.
		return "1: generated summary", nil
	}
	res, err := RunBatched(context.Background(), items, 80, 1500, 15, llm)
	if err != nil {
		t.Fatal(err)
	}
	if res["t"] != "Has title" {
		t.Errorf("title item = %q, want 'Has title'", res["t"])
	}
	if res["p"] != "generated summary" {
		t.Errorf("llm item = %q, want 'generated summary'", res["p"])
	}
	if called != 1 {
		t.Errorf("llm called %d times, want 1", called)
	}
}

// TestRunBatchedSkipsEmpties ensures empty items map to EmptySession and are
// never handed to the LLM: with 3 empty + 2 real items, the LLM sees only 2.
func TestRunBatchedSkipsEmpties(t *testing.T) {
	items := []Item{
		{ID: "e1"}, // empty
		{ID: "e2", Title: "Long-ish but empty prompts"},   // empty (no prompts)
		{ID: "e3", Prompts: []string{"   ", "\n"}},        // empty (whitespace)
		{ID: "r1", Prompts: []string{"first real task"}},  // real
		{ID: "r2", Prompts: []string{"second real task"}}, // real
	}
	// Long titles so real items require the LLM instead of title reuse.
	items[3].Title = strings.Repeat("x", 100)
	items[4].Title = strings.Repeat("y", 100)

	var seen int
	llm := func(ctx context.Context, prompt string) (string, error) {
		// Count how many sessions were placed into the batch prompt.
		seen += strings.Count(prompt, "=== SESSION ")
		return "1: r1 desc\n2: r2 desc", nil
	}
	res, err := RunBatched(context.Background(), items, 80, 1500, 15, llm)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"e1", "e2", "e3"} {
		if res[id] != EmptySession {
			t.Errorf("%s = %q, want EmptySession", id, res[id])
		}
	}
	if seen != 2 {
		t.Errorf("llm asked about %d items, want 2 (only the real ones)", seen)
	}
	if res["r1"] == "" || res["r2"] == "" {
		t.Errorf("real items should be summarized: r1=%q r2=%q", res["r1"], res["r2"])
	}
}

func TestBuildBatchPromptTruncates(t *testing.T) {
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	items := []Item{{ID: "x", Prompts: []string{string(long)}}}
	prompt := BuildBatchPrompt(items, 80, 100)
	if len(prompt) > 1000 {
		t.Errorf("content should be truncated; prompt len = %d", len(prompt))
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[1;31mhello\x1b[0m\rworld"
	if got := StripANSI(in); got != "helloworld" {
		t.Errorf("StripANSI = %q", got)
	}
}

func TestCleanPrompt(t *testing.T) {
	// Replaces markdown code block
	in := "Please review this code:\n```go\nfunc main() {\n  println(\"hello\")\n}\n```\nThanks!"
	cleaned := CleanPrompt(in, 400)
	if strings.Contains(cleaned, "func main") {
		t.Errorf("expected code block to be stripped, got %q", cleaned)
	}
	if !strings.Contains(cleaned, "[code]") {
		t.Errorf("expected placeholder [code], got %q", cleaned)
	}

	// Truncates if over maxLen
	longPrompt := strings.Repeat("hello world ", 50)
	truncated := CleanPrompt(longPrompt, 50)
	if len(truncated) > 60 || !strings.HasSuffix(truncated, "…") {
		t.Errorf("expected truncated string with ellipsis, got %q (len %d)", truncated, len(truncated))
	}

	// Multibyte (CJK / Emoji) safety: truncating should never break UTF-8 sequences.
	cjkPrompt := "这是一个测试长提示，包含中文和表情符号🚀🌟🎉，用于验证截断不会切断UTF-8多字节字符。"
	for maxRunes := 1; maxRunes <= 20; maxRunes++ {
		res := CleanPrompt(cjkPrompt, maxRunes)
		if !utf8.ValidString(res) {
			t.Fatalf("CleanPrompt produced invalid UTF-8 for maxLen=%d: %q", maxRunes, res)
		}
	}
}

func TestBuildBatchPromptUTF8Safety(t *testing.T) {
	items := []Item{
		{
			ID:      "1",
			Title:   "会话标题",
			Prompts: []string{"这是第一条提示 🍎", "这是第二条提示 💡 包含很长的描述信息"},
		},
	}
	// Try different character bounds
	for maxChars := 5; maxChars <= 30; maxChars++ {
		prompt := BuildBatchPrompt(items, 80, maxChars)
		if !utf8.ValidString(prompt) {
			t.Fatalf("BuildBatchPrompt produced invalid UTF-8 with maxChars=%d: %q", maxChars, prompt)
		}
	}
}

func TestSelectPrompts(t *testing.T) {
	// <= 3 prompts: keep all
	short := []string{"p1", "p2"}
	gotShort := SelectPrompts(short, 2, 1)
	if len(gotShort) != 2 || gotShort[0] != "p1" || gotShort[1] != "p2" {
		t.Errorf("expected all prompts for short list, got %v", gotShort)
	}

	// Exactly 3: keep all
	exact := []string{"p1", "p2", "p3"}
	gotExact := SelectPrompts(exact, 2, 1)
	if len(gotExact) != 3 {
		t.Errorf("expected 3 prompts, got %v", gotExact)
	}

	// 10 prompts: head 2, tail 1
	long := []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10"}
	gotLong := SelectPrompts(long, 2, 1)
	if len(gotLong) != 3 {
		t.Fatalf("expected 3 prompts, got %d: %v", len(gotLong), gotLong)
	}
	if gotLong[0] != "p1" || gotLong[1] != "p2" || gotLong[2] != "p10" {
		t.Errorf("expected [p1, p2, p10], got %v", gotLong)
	}

	// Tail-only: head=0, tail=2
	tailOnly := SelectPrompts(long, 0, 2)
	if len(tailOnly) != 2 || tailOnly[0] != "p9" || tailOnly[1] != "p10" {
		t.Errorf("expected [p9, p10], got %v", tailOnly)
	}

	// Head-only: head=2, tail=0
	headOnly := SelectPrompts(long, 2, 0)
	if len(headOnly) != 2 || headOnly[0] != "p1" || headOnly[1] != "p2" {
		t.Errorf("expected [p1, p2], got %v", headOnly)
	}
}

func TestRunBatchedSingleItemFallbackParsing(t *testing.T) {
	// When backend returns plain text without "1: " prefix for a single item batch
	items := []Item{
		{ID: "single-1", Prompts: []string{"some long prompt that needs summarizing"}},
	}
	items[0].Title = strings.Repeat("x", 100) // force LLM usage

	llm := func(ctx context.Context, prompt string) (string, error) {
		// Single-line summary without "1: " prefix
		return "Fix bug in authentication flow\nSome extra explanations", nil
	}

	res, err := RunBatched(context.Background(), items, 80, 1500, 1, llm)
	if err != nil {
		t.Fatalf("RunBatched: %v", err)
	}
	if res["single-1"] != "Fix bug in authentication flow" {
		t.Errorf("single-1 = %q, want 'Fix bug in authentication flow'", res["single-1"])
	}
}

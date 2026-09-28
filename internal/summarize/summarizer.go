// Package summarize generates short session descriptions. The Summarizer
// interface is backend-agnostic; concrete backends (subprocess, openai) live
// in subpackages. Prompt construction and response parsing are shared here.
package summarize

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/semigodking/clisesh/internal/textutil"
)

// Item is one session to describe.
type Item struct {
	ID      string
	Title   string
	Prompts []string
}

// Summarizer produces descriptions (id -> text) for a set of items.
type Summarizer interface {
	Summarize(ctx context.Context, items []Item) (map[string]string, error)
}

// DefaultBatchSize is how many LLM-needing items are grouped per request.
const DefaultBatchSize = 5

// NoDescription is the placeholder when nothing can describe a session.
const NoDescription = "(no description)"

// EmptySession is the sentinel description for a session that has no user
// prompts at all. Such sessions are never sent to the LLM.
const EmptySession = "(empty session)"

// IsEmpty reports whether a session has no usable user prompt content. A
// session is empty when the normalized, joined prompts are the empty string
// (nil, all-blank, or whitespace-only prompts all count as empty).
func IsEmpty(prompts []string) bool {
	return concatPrompts(prompts) == ""
}

// DescribeOrPrompt decides how to describe an item given a title-length
// threshold. It returns (description, needsLLM). When needsLLM is true, the
// caller should summarize the item's prompts; description is empty.
func DescribeOrPrompt(it Item, titleLen int) (string, bool) {
	// A session with no user prompts is empty regardless of its title
	// (decision 1a): never summarize it, never reuse the title.
	if IsEmpty(it.Prompts) {
		return EmptySession, false
	}
	if t := strings.TrimSpace(it.Title); t != "" && textutil.DisplayWidth(t) <= titleLen {
		return t, false
	}
	if len(concatPrompts(it.Prompts)) == 0 && strings.TrimSpace(it.Title) == "" {
		return NoDescription, false
	}
	return "", true
}

var codeBlockRe = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\n?.*?\n?```")

// CleanPrompt strips markdown code blocks, normalizes newlines/whitespace,
// and truncates a single prompt to maxLen characters to prevent single-turn bloat.
func CleanPrompt(p string, maxLen int) string {
	s := codeBlockRe.ReplaceAllString(p, "[code]")
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	s = strings.Join(strings.Fields(s), " ")
	return textutil.TruncateRunes(s, maxLen, true)
}

// SelectPrompts extracts up to headCount prompts from the start and tailCount
// prompts from the end of allPrompts, preserving chronological order and avoiding
// duplicate turns. If len(allPrompts) <= headCount+tailCount, all are included.
func SelectPrompts(allPrompts []string, headCount, tailCount int) []string {
	n := len(allPrompts)
	if headCount <= 0 && tailCount <= 0 {
		return allPrompts
	}
	if headCount <= 0 {
		if n <= tailCount {
			return allPrompts
		}
		return allPrompts[n-tailCount:]
	}
	if tailCount <= 0 {
		if n <= headCount {
			return allPrompts
		}
		return allPrompts[:headCount]
	}
	if n <= headCount+tailCount {
		return allPrompts
	}
	selected := make([]string, 0, headCount+tailCount)
	for i := 0; i < headCount && i < n; i++ {
		selected = append(selected, allPrompts[i])
	}
	tailStart := n - tailCount
	if tailStart < headCount {
		tailStart = headCount
	}
	for i := tailStart; i < n; i++ {
		selected = append(selected, allPrompts[i])
	}
	return selected
}

// concatPrompts joins prompts into a single normalized string using default head=2, tail=1, maxLen=400.
func concatPrompts(prompts []string) string {
	return formatPrompts(prompts, 2, 1, 400)
}

// formatPrompts selects head/tail turns, cleans code noise, and joins prompts into a single string.
func formatPrompts(prompts []string, headCount, tailCount, promptMaxChars int) string {
	selected := SelectPrompts(prompts, headCount, tailCount)
	parts := make([]string, 0, len(selected))
	for _, p := range selected {
		cleaned := CleanPrompt(p, promptMaxChars)
		if cleaned != "" {
			parts = append(parts, cleaned)
		}
	}
	return strings.Join(parts, " ")
}

// BuildBatchPrompt constructs the batch summarization prompt for items that
// need LLM summarization. maxChars bounds each session's content.
func BuildBatchPrompt(items []Item, titleLen, maxChars int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Generate brief, descriptive titles (under %d characters) for these %d sessions.\n",
		titleLen, len(items))
	b.WriteString("Focus on the main task, topic, or problem. Be concise and specific.\n")
	b.WriteString("Respond with ONLY a numbered list where each line is \"<n>: <description>\".\n\n")
	for i, it := range items {
		content := concatPrompts(it.Prompts)
		if it.Title != "" {
			content = it.Title + " " + content
		}
		if maxChars > 0 {
			content = textutil.TruncateRunes(content, maxChars, false)
		}
		fmt.Fprintf(&b, "=== SESSION %d ===\n%s\n\n", i+1, content)
	}
	b.WriteString("Response:")
	return b.String()
}

var (
	numberedRe = regexp.MustCompile(`^\s*(\d+)[:.]?\s*(.+)$`)
	ansiRe     = regexp.MustCompile(`\x1b\[[0-9;]*[mKHFABCDJsu]|\x1b\][^\x07]*\x07|\r`)
)

// StripANSI removes ANSI escape sequences and carriage returns.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// ParseNumberedList maps a numbered-list response to item IDs by position.
// items provides the ID for each 1-based index in the response.
func ParseNumberedList(response string, items []Item) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(StripANSI(response), "\n") {
		trimmed := strings.TrimSpace(line)
		// A model may prefix list items with a markdown blockquote.
		trimmed = strings.TrimPrefix(trimmed, "> ")
		m := numberedRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil || idx < 1 || idx > len(items) {
			continue
		}
		desc := cleanDescription(m[2])
		if desc != "" {
			out[items[idx-1].ID] = desc
		}
	}
	return out
}

// LLMFunc runs one batch prompt and returns the raw model response.
type LLMFunc func(ctx context.Context, prompt string) (string, error)

// RunBatched applies the shared description strategy: title-eligible items are
// resolved without the LLM; the rest are batched and summarized via llm.
// titleLen bounds title reuse and target description length; maxChars bounds
// per-session content in the prompt; batchSize groups LLM-needing items.
func RunBatched(ctx context.Context, items []Item, titleLen, maxChars, batchSize int, llm LLMFunc) (map[string]string, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	result := make(map[string]string, len(items))
	var needLLM []Item
	for _, it := range items {
		// Empty sessions are resolved to the sentinel and never sent to the
		// LLM, regardless of any title they may carry (decision 1a).
		if IsEmpty(it.Prompts) {
			result[it.ID] = EmptySession
			continue
		}
		if desc, needs := DescribeOrPrompt(it, titleLen); !needs {
			result[it.ID] = desc
		} else {
			needLLM = append(needLLM, it)
		}
	}
	for start := 0; start < len(needLLM); start += batchSize {
		end := start + batchSize
		if end > len(needLLM) {
			end = len(needLLM)
		}
		batch := needLLM[start:end]
		prompt := BuildBatchPrompt(batch, titleLen, maxChars)
		resp, err := llm(ctx, prompt)
		if err != nil {
			return result, err
		}
		parsed := ParseNumberedList(resp, batch)
		if len(parsed) == 0 && len(batch) == 1 {
			// Fallback: if only one session was batched, the backend may return a plain string
			// without the "1: " prefix.
			trimmed := cleanDescription(StripANSI(resp))
			if idx := strings.IndexByte(trimmed, '\n'); idx != -1 {
				trimmed = strings.TrimSpace(trimmed[:idx])
			}
			if trimmed != "" {
				result[batch[0].ID] = textutil.TruncateRunes(trimmed, titleLen, true)
			}
		} else {
			for id, desc := range parsed {
				result[id] = desc
			}
		}
	}
	return result, nil
}

// cleanDescription strips markdown blockquote prefixes and surrounding quotes.
func cleanDescription(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "> ")
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
}

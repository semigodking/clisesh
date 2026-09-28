// Package subprocess implements summarize.Summarizer by shelling out to a
// configurable local agent CLI (e.g. kiro-cli, agy) in non-interactive mode.
package subprocess

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/semigodking/clisesh/internal/summarize"
)

// execCommand builds the command; overridable in tests.
var execCommand = exec.CommandContext

// Backend runs summarization via a configurable CLI subprocess.
type Backend struct {
	Command   string
	Args      []string
	Model     string
	TitleLen  int
	Timeout   time.Duration
	BatchSize int
	MaxChars  int
}

// New returns a configured subprocess backend. Args support {model} and {prompt}.
func New(command string, args []string, model string, titleLen int, timeout time.Duration) *Backend {
	return NewWithOptions(command, args, model, titleLen, timeout, summarize.DefaultBatchSize, 1500)
}

// NewWithOptions returns a subprocess backend with custom batch size and max chars.
func NewWithOptions(command string, args []string, model string, titleLen int, timeout time.Duration, batchSize, maxChars int) *Backend {
	if batchSize <= 0 {
		batchSize = summarize.DefaultBatchSize
	}
	if maxChars <= 0 {
		maxChars = 1500
	}
	return &Backend{
		Command:   command,
		Args:      args,
		Model:     model,
		TitleLen:  titleLen,
		Timeout:   timeout,
		BatchSize: batchSize,
		MaxChars:  maxChars,
	}
}

// Summarize implements summarize.Summarizer.
func (b *Backend) Summarize(ctx context.Context, items []summarize.Item) (map[string]string, error) {
	return summarize.RunBatched(ctx, items, b.TitleLen, b.MaxChars, b.BatchSize,
		func(ctx context.Context, prompt string) (string, error) {
			return b.runOnce(ctx, prompt)
		})
}

func (b *Backend) runOnce(ctx context.Context, prompt string) (string, error) {
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	} else {
		timeout *= 2 // batches get more time
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the arg list, substituting {model} and {prompt} per arg. If no arg
	// contains {prompt}, append the prompt as the final arg.
	args := make([]string, 0, len(b.Args)+1)
	hasPrompt := false
	for _, a := range b.Args {
		a = strings.ReplaceAll(a, "{model}", b.Model)
		if strings.Contains(a, "{prompt}") {
			a = strings.ReplaceAll(a, "{prompt}", prompt)
			hasPrompt = true
		}
		args = append(args, a)
	}
	if !hasPrompt {
		args = append(args, prompt)
	}

	for i := range args {
		args[i] = strings.ToValidUTF8(args[i], "")
	}

	cmd := execCommand(cctx, b.Command, args...)
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s timed out after %s", b.Command, timeout)
		}
		return "", fmt.Errorf("%s failed: %v: %s", b.Command, err, stderr.String())
	}
	return summarize.StripANSI(stdout.String()), nil
}

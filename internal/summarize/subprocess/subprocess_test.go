package subprocess

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/semigodking/clisesh/internal/summarize"
)

// TestHelperProcess is not a real test; it's invoked as a fake `kiro-cli`.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Emit a numbered-list response as kiro-cli would (with some ANSI noise).
	os.Stdout.WriteString("\x1b[32m1: Generated description one\x1b[0m\n2: Generated description two\n")
	os.Exit(0)
}

// fakeExec substitutes execCommand to run the helper process above.
func fakeExec(ctx context.Context, name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--"}
	cmd := exec.CommandContext(ctx, os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestSummarizeViaSubprocess(t *testing.T) {
	orig := execCommand
	execCommand = fakeExec
	defer func() { execCommand = orig }()

	b := New("kiro-cli", nil, "test-model", 80, 5*time.Second)
	items := []summarize.Item{
		{ID: "id1", Prompts: []string{"first prompt"}},
		{ID: "id2", Prompts: []string{"second prompt"}},
	}
	got, err := b.Summarize(context.Background(), items)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got["id1"] != "Generated description one" {
		t.Errorf("id1 = %q", got["id1"])
	}
	if got["id2"] != "Generated description two" {
		t.Errorf("id2 = %q", got["id2"])
	}
}

// fakeExecTimeout runs a helper that sleeps, to trigger the context timeout.
func fakeExecTimeout(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperSleep", "--")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_SLEEP=1")
	return cmd
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_SLEEP") != "1" {
		return
	}
	time.Sleep(10 * time.Second)
	os.Exit(0)
}

func TestSubprocessTimeout(t *testing.T) {
	orig := execCommand
	execCommand = fakeExecTimeout
	defer func() { execCommand = orig }()

	// Timeout is doubled internally, so use a tiny value.
	b := New("kiro-cli", nil, "test-model", 80, 50*time.Millisecond)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"p"}}}
	_, err := b.Summarize(context.Background(), items)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestTitleShortCircuitsNoSubprocess(t *testing.T) {
	// If all items have usable titles, the subprocess must never run.
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		t.Fatal("subprocess should not be invoked when titles suffice")
		return nil
	}
	defer func() { execCommand = exec.CommandContext }()

	b := New("kiro-cli", nil, "m", 80, time.Second)
	items := []summarize.Item{{ID: "a", Title: "Nice title", Prompts: []string{"has content"}}}
	got, err := b.Summarize(context.Background(), items)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "Nice title" {
		t.Errorf("a = %q", got["a"])
	}
}

// TestTokenSubstitution verifies a custom command and arg template have their
// {model}/{prompt} tokens substituted and the correct command name is invoked.
func TestTokenSubstitution(t *testing.T) {
	var gotName string
	var gotArgs []string
	orig := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = args
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	defer func() { execCommand = orig }()

	b := New("agy", []string{"--model", "{model}", "--print", "{prompt}"}, "custom-model", 80, 5*time.Second)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"summarize this"}}}
	if _, err := b.Summarize(context.Background(), items); err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if gotName != "agy" {
		t.Errorf("command name = %q, want %q", gotName, "agy")
	}
	// {model} must be substituted.
	foundModel := false
	for _, a := range gotArgs {
		if a == "custom-model" {
			foundModel = true
		}
		if strings.Contains(a, "{model}") {
			t.Errorf("unsubstituted {model} token in arg %q", a)
		}
		if strings.Contains(a, "{prompt}") {
			t.Errorf("unsubstituted {prompt} token in arg %q", a)
		}
	}
	if !foundModel {
		t.Errorf("model not substituted into args: %v", gotArgs)
	}
	// {prompt} must be substituted with the batched prompt (contains our text).
	foundPrompt := false
	for _, a := range gotArgs {
		if strings.Contains(a, "summarize this") {
			foundPrompt = true
		}
	}
	if !foundPrompt {
		t.Errorf("prompt not substituted into args: %v", gotArgs)
	}
}

// TestPromptAppendedWhenNoToken verifies that when no {prompt} token is present
// in the arg template, the prompt is appended as the final argument.
func TestPromptAppendedWhenNoToken(t *testing.T) {
	var gotArgs []string
	orig := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotArgs = args
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	defer func() { execCommand = orig }()

	b := New("agy", []string{"--print"}, "m", 80, 5*time.Second)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"needle prompt"}}}
	if _, err := b.Summarize(context.Background(), items); err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "--print" {
		t.Errorf("args = %v, want first arg --print", gotArgs)
	}
	last := gotArgs[len(gotArgs)-1]
	if !strings.Contains(last, "needle prompt") {
		t.Errorf("last arg = %q, expected appended prompt", last)
	}
}

// TestSubprocessSanitizesArgsInvalidUTF8 verifies that even if broken UTF-8 byte sequences
// enter the prompt or command args, they are sanitized to valid UTF-8 before being passed to execCommand.
func TestSubprocessSanitizesArgsInvalidUTF8(t *testing.T) {
	var capturedArgs []string
	orig := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	defer func() { execCommand = orig }()

	// Deliberately broken UTF-8 byte sequence
	brokenPrompt := "test " + string([]byte{0xff, 0xfe, 0x80, 'o', 'k'})
	b := New("kiro-cli", []string{"-p", "{prompt}"}, "m", 80, 5*time.Second)
	_, err := b.runOnce(context.Background(), brokenPrompt)
	if err != nil {
		t.Fatalf("runOnce failed: %v", err)
	}

	for i, arg := range capturedArgs {
		if !utf8.ValidString(arg) {
			t.Errorf("arg %d is not valid UTF-8: %q", i, arg)
		}
		if i == 1 && !strings.Contains(arg, "ok") {
			t.Errorf("arg %d does not contain expected sanitized text: %q", i, arg)
		}
	}
}

// TestSubprocessSetsStdinToEmptyReader verifies that cmd.Stdin is set so the child process does not hang on stdin.
func TestSubprocessSetsStdinToEmptyReader(t *testing.T) {
	orig := execCommand
	var capturedCmd *exec.Cmd
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		capturedCmd = cmd
		return cmd
	}
	defer func() { execCommand = orig }()

	b := New("kiro-cli", nil, "m", 80, 5*time.Second)
	_, err := b.runOnce(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if capturedCmd == nil {
		t.Fatal("expected captured command")
	}
	if capturedCmd.Stdin == nil {
		t.Error("expected capturedCmd.Stdin to be non-nil (bytes.NewReader(nil)) to prevent child process stdin hangs")
	}
}

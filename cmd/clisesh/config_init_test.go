package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/semigodking/clisesh/internal/config"
)

// TestWriteDefaultConfigCreatesLoadableFile verifies writeDefaultConfig creates
// the file (including parent dirs), embeds the header comment, and that the
// result reloads to the built-in defaults.
func TestWriteDefaultConfigCreatesLoadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")

	if err := writeDefaultConfig(path); err != nil {
		t.Fatalf("writeDefaultConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if !strings.Contains(string(data), "# clisesh configuration") {
		t.Errorf("written config missing header comment marker; got:\n%s", data)
	}

	cfg, err := config.Load(config.Flags{ConfigPath: path})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Provider != "all" {
		t.Errorf("provider = %q, want %q", cfg.Provider, "all")
	}
	if cfg.Summarizer.Backend == "" {
		t.Errorf("summarizer backend is empty, want non-empty")
	}
}

// TestRunConfigInitOverwriteWithForce verifies force=true overwrites an
// existing file without prompting or error.
func TestRunConfigInitOverwriteWithForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("provider = \"kiro\"\n"), 0o644); err != nil {
		t.Fatalf("seeding existing file: %v", err)
	}

	var out bytes.Buffer
	if err := runConfigInit(path, true, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runConfigInit force: %v", err)
	}
	if !strings.Contains(out.String(), "Wrote default config to") {
		t.Errorf("expected write confirmation, got %q", out.String())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# clisesh configuration") {
		t.Errorf("file was not overwritten with default config; got:\n%s", data)
	}
	cfg, err := config.Load(config.Flags{ConfigPath: path})
	if err != nil {
		t.Fatalf("config.Load after overwrite: %v", err)
	}
	if cfg.Provider != "all" {
		t.Errorf("provider = %q, want %q after overwrite", cfg.Provider, "all")
	}
}

// TestRunConfigInitPromptDeclineDoesNotOverwrite verifies that when the file
// exists, force is false, and the user answers non-affirmatively, the file is
// left untouched and no error is returned.
func TestRunConfigInitPromptDeclineDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "provider = \"kiro\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding existing file: %v", err)
	}

	var out bytes.Buffer
	if err := runConfigInit(path, false, strings.NewReader("n\n"), &out); err != nil {
		t.Fatalf("runConfigInit decline: %v", err)
	}
	if !strings.Contains(out.String(), "Aborted.") {
		t.Errorf("expected abort message, got %q", out.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("file changed despite decline; got:\n%s", data)
	}
}

// TestRunConfigInitPromptAcceptOverwrites verifies an affirmative answer
// overwrites the existing file.
func TestRunConfigInitPromptAcceptOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("provider = \"kiro\"\n"), 0o644); err != nil {
		t.Fatalf("seeding existing file: %v", err)
	}

	var out bytes.Buffer
	if err := runConfigInit(path, false, strings.NewReader("Y\n"), &out); err != nil {
		t.Fatalf("runConfigInit accept: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# clisesh configuration") {
		t.Errorf("file not overwritten on 'Y'; got:\n%s", data)
	}
}

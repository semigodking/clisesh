package config

import (
	"os"
	"path/filepath"
	"testing"
)

// clearEnv unsets all CLISESH_* and XDG vars this package reads so tests are
// hermetic regardless of the host environment.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "CLISESH_CONFIG",
		"CLISESH_PROVIDER", "CLISESH_THEME", "CLISESH_TITLE_LEN",
		"CLISESH_CACHE_DIR", "CLISESH_SUMMARIZER_BACKEND",
		"CLISESH_SUMMARIZER_MODEL", "CLISESH_SUMMARIZER_BATCH_SIZE",
		"CLISESH_SUMMARIZER_MAX_PROMPTS", "CLISESH_SUMMARIZER_HEAD_PROMPTS",
		"CLISESH_SUMMARIZER_TAIL_PROMPTS",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestDefaults(t *testing.T) {
	clearEnv(t)
	// Point config at a non-existent file so no host file is read.
	cfg, err := Load(Flags{ConfigPath: filepath.Join(t.TempDir(), "none.toml")})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "all" {
		t.Errorf("provider = %q, want all", cfg.Provider)
	}
	if cfg.Theme != "default" {
		t.Errorf("theme = %q, want default", cfg.Theme)
	}
	if cfg.TitleLen != 80 {
		t.Errorf("title_len = %d, want 80", cfg.TitleLen)
	}
	if cfg.Summarizer.Backend != "subprocess.kiro" {
		t.Errorf("backend = %q, want subprocess.kiro", cfg.Summarizer.Backend)
	}
	if cfg.Summarizer.BatchSize != 5 {
		t.Errorf("batch_size = %d, want 5", cfg.Summarizer.BatchSize)
	}
	if cfg.Summarizer.MaxPrompts != 10 {
		t.Errorf("max_prompts = %d, want 10", cfg.Summarizer.MaxPrompts)
	}
	if cfg.Summarizer.HeadPrompts != 2 {
		t.Errorf("head_prompts = %d, want 2", cfg.Summarizer.HeadPrompts)
	}
	if cfg.Summarizer.TailPrompts != 1 {
		t.Errorf("tail_prompts = %d, want 1", cfg.Summarizer.TailPrompts)
	}
	if cfg.Summarizer.PromptMaxChars != 400 {
		t.Errorf("prompt_max_chars = %d, want 400", cfg.Summarizer.PromptMaxChars)
	}
	if cfg.Summarizer.MaxChars != 1500 {
		t.Errorf("max_chars = %d, want 1500", cfg.Summarizer.MaxChars)
	}
	if cfg.CacheDir == "" {
		t.Error("cache_dir should be resolved to a default, got empty")
	}

	kind, name, sp, _, err := cfg.Summarizer.ResolveBackend()
	if err != nil {
		t.Fatalf("ResolveBackend: %v", err)
	}
	if kind != "subprocess" || name != "kiro" || sp.Command != "kiro-cli" {
		t.Errorf("unexpected default backend: kind=%s, name=%s, cmd=%s", kind, name, sp.Command)
	}
}

func TestMultiBackendFileLoading(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
provider = "antigravity"
theme = "gray"
title_len = 100

[summarizer]
backend = "summarizer.subprocess.agy"

[summarizer.subprocess.agy]
command = "agy"
args = ["--print", "{prompt}"]

[summarizer.subprocess.kiro]
command = "kiro-cli"
args = ["chat", "{prompt}"]

[summarizer.llm.deepseek]
base_url = "https://api.deepseek.com/v1"
api_key = "sk-deepseek"
model = "deepseek-chat"
format = "openai"

[summarizer.llm.glm]
base_url = "https://open.bigmodel.cn/api/paas/v4"
api_key = "sk-glm"
model = "glm-4-flash"
format = "system_user"
`), 0o644)

	cfg, err := Load(Flags{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "antigravity" {
		t.Errorf("provider = %q, want antigravity", cfg.Provider)
	}
	if cfg.Theme != "gray" {
		t.Errorf("theme = %q, want gray", cfg.Theme)
	}

	// 1. Resolve subprocess.agy (full path summarizer.subprocess.agy)
	kind, name, sp, _, err := cfg.Summarizer.ResolveBackend()
	if err != nil {
		t.Fatalf("ResolveBackend: %v", err)
	}
	if kind != "subprocess" || name != "agy" || sp.Command != "agy" {
		t.Errorf("expected subprocess.agy, got %s.%s cmd=%s", kind, name, sp.Command)
	}

	// 2. Resolve short path "llm.deepseek"
	cfg.Summarizer.Backend = "llm.deepseek"
	kind, name, _, l, err := cfg.Summarizer.ResolveBackend()
	if err != nil {
		t.Fatalf("ResolveBackend llm.deepseek: %v", err)
	}
	if kind != "llm" || name != "deepseek" || l.Model != "deepseek-chat" {
		t.Errorf("expected llm.deepseek, got %s.%s model=%s", kind, name, l.Model)
	}

	// 3. Resolve single name "glm"
	cfg.Summarizer.Backend = "glm"
	kind, name, _, l, err = cfg.Summarizer.ResolveBackend()
	if err != nil {
		t.Fatalf("ResolveBackend glm: %v", err)
	}
	if kind != "llm" || name != "glm" || l.Format != "system_user" {
		t.Errorf("expected glm, got %s.%s format=%s", kind, name, l.Format)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`provider = "kiro"`+"\n"), 0o644)

	t.Setenv("CLISESH_PROVIDER", "antigravity")
	t.Setenv("CLISESH_TITLE_LEN", "42")
	t.Setenv("CLISESH_SUMMARIZER_BACKEND", "subprocess.agy")

	cfg, err := Load(Flags{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "antigravity" {
		t.Errorf("env should override file: provider = %q", cfg.Provider)
	}
	if cfg.TitleLen != 42 {
		t.Errorf("env title_len = %d, want 42", cfg.TitleLen)
	}
	if cfg.Summarizer.Backend != "subprocess.agy" {
		t.Errorf("env backend = %q, want subprocess.agy", cfg.Summarizer.Backend)
	}
}

func TestFlagsOverrideEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("CLISESH_PROVIDER", "antigravity")
	tl := 55
	cfg, err := Load(Flags{
		ConfigPath: filepath.Join(t.TempDir(), "none.toml"),
		Provider:   "kiro",
		TitleLen:   &tl,
		Backend:    "llm.openai",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "kiro" {
		t.Errorf("flag should override env: provider = %q", cfg.Provider)
	}
	if cfg.TitleLen != 55 {
		t.Errorf("flag title_len = %d, want 55", cfg.TitleLen)
	}
	if cfg.Summarizer.Backend != "llm.openai" {
		t.Errorf("flag backend = %q, want llm.openai", cfg.Summarizer.Backend)
	}
}

func TestXDGConfigPath(t *testing.T) {
	clearEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	want := filepath.Join(xdg, "clisesh", "config.toml")
	if got := DefaultConfigPath(); got != want {
		t.Errorf("DefaultConfigPath = %q, want %q", got, want)
	}
}

func TestMalformedTOML(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("this is = = not valid toml ["), 0o644)
	if _, err := Load(Flags{ConfigPath: path}); err == nil {
		t.Error("expected error for malformed TOML")
	}
}

func TestSecretResolutionAndRedaction(t *testing.T) {
	clearEnv(t)
	t.Setenv("MY_SECRET_KEY", "sk-topsecret")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[summarizer.llm.openai]
api_key = "${MY_SECRET_KEY}"

[summarizer.llm.deepseek]
api_key = "literal-secret"
`), 0o644)

	cfg, err := Load(Flags{ConfigPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Summarizer.LLM["openai"].APIKey != "sk-topsecret" {
		t.Errorf("env ref not resolved: %q", cfg.Summarizer.LLM["openai"].APIKey)
	}
	r := cfg.Redacted()
	if r.Summarizer.LLM["openai"].APIKey != "***" {
		t.Errorf("openai key not redacted: %q", r.Summarizer.LLM["openai"].APIKey)
	}
	if r.Summarizer.LLM["deepseek"].APIKey != "***" {
		t.Errorf("deepseek key not redacted: %q", r.Summarizer.LLM["deepseek"].APIKey)
	}
	// Original must be unchanged by Redacted().
	if cfg.Summarizer.LLM["openai"].APIKey != "sk-topsecret" {
		t.Error("Redacted must not mutate the original config")
	}
}

func TestResolveBackendErrors(t *testing.T) {
	s := SummarizerConfig{
		Backend: "subprocess.nonexistent",
		Subprocess: map[string]SubprocessConfig{
			"kiro": {},
		},
	}
	if _, _, _, _, err := s.ResolveBackend(); err == nil {
		t.Error("expected error for nonexistent subprocess")
	}

	s.Backend = "nonexistent"
	if _, _, _, _, err := s.ResolveBackend(); err == nil {
		t.Error("expected error for nonexistent backend")
	}
}

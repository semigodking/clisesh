// Package config loads clisesh configuration by layering, in increasing
// precedence: built-in defaults, a TOML file, CLISESH_* environment
// variables, and CLI flags. The file location is XDG-aware.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// SubprocessConfig configures an arbitrary CLI as a summarizer backend.
type SubprocessConfig struct {
	Command    string   `toml:"command"`     // e.g. "kiro-cli", "agy"
	Args       []string `toml:"args"`        // arg template; tokens {prompt} and {model}
	Model      string   `toml:"model"`       // optional per-backend model override
	TimeoutSec int      `toml:"timeout_sec"` // optional per-backend timeout
}

// LLMConfig configures an HTTP API summarizer backend (OpenAI, DeepSeek, GLM, etc.).
type LLMConfig struct {
	BaseURL     string            `toml:"base_url"`
	APIKey      string            `toml:"api_key"`
	Model       string            `toml:"model"`
	Temperature float64           `toml:"temperature"`
	TimeoutSec  int               `toml:"timeout_sec"`
	Format      string            `toml:"format"`  // "openai" (default), "system_user"
	Headers     map[string]string `toml:"headers"` // optional custom HTTP headers
}

// SummarizerConfig selects and configures the summarization backends.
type SummarizerConfig struct {
	Backend        string                      `toml:"backend"` // e.g. "subprocess.kiro", "llm.deepseek", or full "summarizer.subprocess.agy"
	Model          string                      `toml:"model"`   // global fallback model
	TitleLen       int                         `toml:"title_len"`
	TimeoutSec     int                         `toml:"timeout_sec"`
	BatchSize      int                         `toml:"batch_size"`        // sessions per batch (default: 5)
	MaxPrompts     int                         `toml:"max_prompts"`       // max prompts extracted per session (default: 10)
	HeadPrompts    int                         `toml:"head_prompts"`      // initial prompts to include (default: 2)
	TailPrompts    int                         `toml:"tail_prompts"`      // latest prompts to include (default: 1)
	PromptMaxChars int                         `toml:"prompt_max_chars"`  // char cap per single prompt (default: 400)
	MaxChars       int                         `toml:"max_chars"`         // char cap per session in prompt (default: 1500)
	Subprocess     map[string]SubprocessConfig `toml:"subprocess"`        // [summarizer.subprocess.<name>]
	LLM            map[string]LLMConfig        `toml:"llm"`               // [summarizer.llm.<name>]
}

// Config is the fully-resolved application configuration.
type Config struct {
	Provider   string            `toml:"provider"`
	Theme      string            `toml:"theme"`
	TitleLen   int               `toml:"title_len"`
	CacheDir   string            `toml:"cache_dir"`
	Summarizer SummarizerConfig  `toml:"summarizer"`
	Overrides  map[string]string `toml:"theme_overrides"`

	// configPath records where the file was (or would be) loaded from.
	configPath string
}

// Flags holds command-line overrides. Empty string / nil means "unset".
type Flags struct {
	ConfigPath string
	Provider   string
	Theme      string
	TitleLen   *int
	Model      string
	Backend    string
}

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		Provider: "all",
		Theme:    "default",
		TitleLen: 80,
		CacheDir: "", // resolved lazily to ~/.cache/clisesh
		Summarizer: SummarizerConfig{
			Backend:        "subprocess.kiro",
			Model:          "qwen3-coder-next",
			TitleLen:       80,
			TimeoutSec:     60,
			BatchSize:      5,
			MaxPrompts:     10,
			HeadPrompts:    2,
			TailPrompts:    1,
			PromptMaxChars: 400,
			MaxChars:       1500,
			Subprocess: map[string]SubprocessConfig{
				"kiro": {
					Command: "kiro-cli",
					Args:    []string{"chat", "--no-interactive", "--model", "{model}", "--effort", "low", "--wrap", "never", "{prompt}"},
				},
				"agy": {
					Command: "agy",
					Args:    []string{"--print", "{prompt}"},
				},
			},
			LLM: map[string]LLMConfig{
				"openai": {
					BaseURL:     "https://api.openai.com/v1",
					APIKey:      "${OPENAI_API_KEY}",
					Model:       "gpt-4o-mini",
					Temperature: 0.2,
					TimeoutSec:  60,
					Format:      "openai",
				},
			},
		},
		Overrides: map[string]string{},
	}
}

// DefaultConfigPath returns the XDG-aware config file path.
func DefaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "clisesh", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "clisesh", "config.toml")
}

// DefaultCacheDir returns the XDG-aware cache directory.
func DefaultCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "clisesh")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cache", "clisesh")
}

// Load resolves configuration by layering defaults < file < env < flags.
// A missing config file is not an error.
func Load(flags Flags) (Config, error) {
	cfg := Default()

	// Determine config path: flag > env > default.
	path := DefaultConfigPath()
	if env := os.Getenv("CLISESH_CONFIG"); env != "" {
		path = env
	}
	if flags.ConfigPath != "" {
		path = flags.ConfigPath
	}
	cfg.configPath = path

	// Layer 2: file (if present).
	if data, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	// Layer 3: environment.
	applyEnv(&cfg)

	// Layer 4: flags.
	applyFlags(&cfg, flags)

	// Resolve derived defaults and secret indirection.
	if cfg.CacheDir == "" {
		cfg.CacheDir = DefaultCacheDir()
	}

	// Resolve env references across all LLM configs.
	for name, l := range cfg.Summarizer.LLM {
		l.APIKey = resolveEnvRef(l.APIKey)
		for hk, hv := range l.Headers {
			l.Headers[hk] = resolveEnvRef(hv)
		}
		cfg.Summarizer.LLM[name] = l
	}

	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("CLISESH_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("CLISESH_THEME"); v != "" {
		cfg.Theme = v
	}
	if v := os.Getenv("CLISESH_TITLE_LEN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TitleLen = n
		}
	}
	if v := os.Getenv("CLISESH_CACHE_DIR"); v != "" {
		cfg.CacheDir = v
	}
	if v := os.Getenv("CLISESH_SUMMARIZER_BACKEND"); v != "" {
		cfg.Summarizer.Backend = v
	}
	if v := os.Getenv("CLISESH_SUMMARIZER_MODEL"); v != "" {
		cfg.Summarizer.Model = v
	}
	if v := os.Getenv("CLISESH_SUMMARIZER_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Summarizer.BatchSize = n
		}
	}
	if v := os.Getenv("CLISESH_SUMMARIZER_MAX_PROMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Summarizer.MaxPrompts = n
		}
	}
	if v := os.Getenv("CLISESH_SUMMARIZER_HEAD_PROMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Summarizer.HeadPrompts = n
		}
	}
	if v := os.Getenv("CLISESH_SUMMARIZER_TAIL_PROMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Summarizer.TailPrompts = n
		}
	}
}

func applyFlags(cfg *Config, f Flags) {
	if f.Provider != "" {
		cfg.Provider = f.Provider
	}
	if f.Theme != "" {
		cfg.Theme = f.Theme
	}
	if f.TitleLen != nil {
		cfg.TitleLen = *f.TitleLen
	}
	if f.Model != "" {
		cfg.Summarizer.Model = f.Model
	}
	if f.Backend != "" {
		cfg.Summarizer.Backend = f.Backend
	}
}

var envRefRe = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// resolveEnvRef expands a "${ENV_VAR}" reference to its value. A plain string
// is returned unchanged. An unset referenced variable resolves to "".
func resolveEnvRef(s string) string {
	if m := envRefRe.FindStringSubmatch(s); m != nil {
		return os.Getenv(m[1])
	}
	return s
}

// Path returns the config file path that was used (or would be used).
func (c Config) Path() string { return c.configPath }

// Redacted returns a copy of the config with secrets masked, for printing.
func (c Config) Redacted() Config {
	rc := c
	if rc.Summarizer.LLM != nil {
		redactedLLM := make(map[string]LLMConfig, len(rc.Summarizer.LLM))
		for k, v := range rc.Summarizer.LLM {
			if v.APIKey != "" {
				v.APIKey = "***"
			}
			redactedLLM[k] = v
		}
		rc.Summarizer.LLM = redactedLLM
	}
	return rc
}

// ResolveBackend resolves the configured backend string into either a SubprocessConfig or LLMConfig.
// It accepts formats like:
//   - "summarizer.subprocess.agy", "subprocess.agy", "agy"
//   - "summarizer.llm.deepseek", "llm.deepseek", "deepseek"
func (s SummarizerConfig) ResolveBackend() (kind string, name string, sp SubprocessConfig, l LLMConfig, err error) {
	b := strings.TrimSpace(s.Backend)
	b = strings.TrimPrefix(b, "summarizer.")

	parts := strings.SplitN(b, ".", 2)
	if len(parts) == 2 {
		prefix, targetName := parts[0], parts[1]
		switch prefix {
		case "subprocess":
			if cfg, ok := s.Subprocess[targetName]; ok {
				return "subprocess", targetName, cfg, LLMConfig{}, nil
			}
			return "", "", SubprocessConfig{}, LLMConfig{}, fmt.Errorf("unknown subprocess backend %q; configured: %v", targetName, mapKeys(s.Subprocess))
		case "llm":
			if cfg, ok := s.LLM[targetName]; ok {
				return "llm", targetName, SubprocessConfig{}, cfg, nil
			}
			return "", "", SubprocessConfig{}, LLMConfig{}, fmt.Errorf("unknown llm backend %q; configured: %v", targetName, mapKeys(s.LLM))
		default:
			return "", "", SubprocessConfig{}, LLMConfig{}, fmt.Errorf("unknown backend prefix %q in %q (expected 'subprocess' or 'llm')", prefix, s.Backend)
		}
	}

	// Single token (e.g. "agy", "deepseek", "kiro")
	inSubprocess, spOk := s.Subprocess[b]
	inLLM, llmOk := s.LLM[b]

	if spOk && llmOk {
		return "", "", SubprocessConfig{}, LLMConfig{}, fmt.Errorf("ambiguous backend %q exists in both subprocess and llm; use 'subprocess.%s' or 'llm.%s'", b, b, b)
	}
	if spOk {
		return "subprocess", b, inSubprocess, LLMConfig{}, nil
	}
	if llmOk {
		return "llm", b, SubprocessConfig{}, inLLM, nil
	}

	return "", "", SubprocessConfig{}, LLMConfig{}, fmt.Errorf("unknown backend %q; available subprocess: %v, available llm: %v",
		s.Backend, mapKeys(s.Subprocess), mapKeys(s.LLM))
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

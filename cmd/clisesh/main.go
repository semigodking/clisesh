// Command clisesh is a terminal UI for browsing, summarizing, resuming, and
// deleting agent CLI sessions across multiple providers (Kiro, Antigravity).
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/semigodking/clisesh/internal/cache"
	"github.com/semigodking/clisesh/internal/config"
	"github.com/semigodking/clisesh/internal/provider"
	_ "github.com/semigodking/clisesh/internal/provider/antigravity" // register antigravity
	_ "github.com/semigodking/clisesh/internal/provider/claude"      // register claude
	_ "github.com/semigodking/clisesh/internal/provider/codex"       // register codex
	_ "github.com/semigodking/clisesh/internal/provider/kiro"        // register kiro
	"github.com/semigodking/clisesh/internal/textutil"
	"github.com/semigodking/clisesh/internal/theme"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		runTUI(nil)
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("clisesh %s\n", version)
	case "config":
		runConfig(os.Args[2:])
	case "theme":
		runTheme(os.Args[2:])
	case "cache":
		runCache(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "summarize":
		runSummarize(os.Args[2:])
	case "tui":
		runTUI(os.Args[2:])
	case "providers":
		for _, n := range provider.Names() {
			fmt.Println(n)
		}
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}

// runTheme implements `clisesh theme preview <name>` and `clisesh theme list`.
func runTheme(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "list":
		for _, n := range theme.Names() {
			fmt.Println(n)
		}
	case "preview":
		name := "default"
		if len(args) > 0 {
			name = args[0]
		}
		th, err := theme.Load(name, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("theme: %s\n", th.Name())
		for _, role := range theme.AllRoles() {
			label := fmt.Sprintf(" %-9s ", theme.RoleName(role))
			fmt.Println(th.Style(role).Render(label) + "  " + theme.RoleName(role))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown theme subcommand: %s\n", sub)
		os.Exit(2)
	}
}

// runCache implements `clisesh cache stats`, reporting cached description
// counts per provider cache file found in the cache directory.
func runCache(args []string) {
	fs := flag.NewFlagSet("cache", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config file")
	sub := "stats"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		sub = args[0]
		args = args[1:]
	}
	_ = fs.Parse(args)

	cfg, err := config.Load(config.Flags{ConfigPath: *cfgPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	switch sub {
	case "stats":
		fmt.Printf("cache dir: %s\n", cfg.CacheDir)
		matches, _ := filepath.Glob(filepath.Join(cfg.CacheDir, "*-descriptions.json"))
		if len(matches) == 0 {
			fmt.Println("(no cache files)")
			return
		}
		for _, m := range matches {
			base := filepath.Base(m)
			provider := strings.TrimSuffix(base, "-descriptions.json")
			c := cache.Open(cfg.CacheDir, provider)
			fmt.Printf("  %-14s %d description(s)\n", provider, c.Len())
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown cache subcommand: %s\n", sub)
		os.Exit(2)
	}
}

// runList prints normalized sessions for the configured (or --provider) provider.
func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config file")
	providerName := fs.String("provider", "", "provider to list")
	baseDir := fs.String("dir", "", "override provider base/session directory")
	_ = fs.Parse(args)

	cfg, err := config.Load(config.Flags{ConfigPath: *cfgPath, Provider: *providerName})
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	prov, err := newProvider(cfg.Provider, *baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	sessions, err := prov.List(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "list error: %v\n", err)
		os.Exit(1)
	}
	now := timeNow()
	for _, s := range sessions {
		age := textutil.FormatAge(s.UpdatedAt, now)
		title := s.Title
		if title == "" {
			title = "(no title)"
		}
		lock := " "
		if s.Locked {
			lock = "●"
		}
		fmt.Printf("%-12s %s %-8s  %9s  %s\n", s.ProviderID, lock, s.ShortID(), age, title)
	}
	if len(sessions) == 0 {
		fmt.Println("(no sessions)")
	}
}

func timeNow() time.Time { return time.Now().UTC() }

// printHelp prints usage for all commands.
func printHelp() {
	fmt.Print(`clisesh — multi-CLI agent session manager

Usage:
  clisesh                       launch the TUI with the configured provider
  clisesh tui [flags]           launch the TUI explicitly
  clisesh list [flags]          list sessions
  clisesh summarize [flags] [id...]  generate & cache descriptions
  clisesh providers             list available providers
  clisesh theme list|preview <name>  inspect color themes
  clisesh cache stats           show cached description counts
  clisesh config print          show effective configuration
  clisesh config init [--force] write a default config file
  clisesh version               print version

Common flags:
  --config <path>   config file (default: ~/.config/clisesh/config.toml)
  --provider <name> provider to use (kiro, antigravity)
  --theme <name>    color theme
  --dir <path>      override the provider's session directory
  --backend <name>  summarizer backend (e.g. subprocess.kiro, subprocess.agy, llm.deepseek)

Configuration precedence: CLI flags > env (CLISESH_*) > config file > defaults.
See docs/ for design and per-component specs.
`)
}

// runConfig implements `clisesh config <sub> [flags]`, e.g.
// `clisesh config print --provider kiro`. The subcommand must come first.
func runConfig(args []string) {
	sub := "print"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		sub = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("config", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config file")
	provider := fs.String("provider", "", "override active provider")
	theme := fs.String("theme", "", "override theme")
	model := fs.String("model", "", "override summarizer model")
	backend := fs.String("backend", "", "override summarizer backend")
	force := fs.Bool("force", false, "overwrite existing config without prompting (config init)")
	_ = fs.Parse(args)

	// `config init` writes a default config file; it must not require an
	// existing/loadable config, so handle it before Load.
	if sub == "init" {
		if err := runConfigInit(resolveConfigPath(*cfgPath), *force, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "config init error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(config.Flags{
		ConfigPath: *cfgPath,
		Provider:   *provider,
		Theme:      *theme,
		Model:      *model,
		Backend:    *backend,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	switch sub {
	case "print":
		fmt.Printf("# effective config (from %s)\n", cfg.Path())
		if err := toml.NewEncoder(os.Stdout).Encode(cfg.Redacted()); err != nil {
			fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown config subcommand: %s\n", sub)
		os.Exit(2)
	}
}

// resolveConfigPath mirrors config.Load's path precedence: an explicit flag
// wins, then the CLISESH_CONFIG env var, then the XDG-aware default path.
func resolveConfigPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if env := os.Getenv("CLISESH_CONFIG"); env != "" {
		return env
	}
	return config.DefaultConfigPath()
}

// configHeader is prepended to generated config files to document precedence
// and location.
const configHeader = `# clisesh configuration
# Location: $XDG_CONFIG_HOME/clisesh/config.toml (default ~/.config/clisesh/config.toml)
# Precedence (highest wins): CLI flags > env (CLISESH_*) > this file > defaults.
# Regenerate with: clisesh config init

`

// renderDefaultConfig returns the default configuration rendered as TOML with
// a leading header comment block. It is generated from config.Default() so it
// always matches the real defaults.
func renderDefaultConfig() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(configHeader)
	if err := toml.NewEncoder(&buf).Encode(config.Default()); err != nil {
		return nil, fmt.Errorf("encoding default config: %w", err)
	}
	return buf.Bytes(), nil
}

// writeDefaultConfig writes the default configuration TOML (with header) to
// path, creating parent directories as needed. It always overwrites and never
// prompts; overwrite policy is the caller's responsibility.
func writeDefaultConfig(path string) error {
	data, err := renderDefaultConfig()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// runConfigInit handles `clisesh config init`. When the target file already
// exists and force is false, it prompts (via in/out) before overwriting; a
// non-affirmative answer aborts without error. Otherwise it writes the default
// config to path.
func runConfigInit(path string, force bool, in io.Reader, out io.Writer) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(out, "Config already exists at %s. Overwrite? [y/N]: ", path)
			r := bufio.NewReader(in)
			line, _ := r.ReadString('\n')
			if ans := strings.ToLower(strings.TrimSpace(line)); ans != "y" {
				fmt.Fprintln(out, "Aborted.")
				return nil
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking %s: %w", path, err)
		}
	}
	if err := writeDefaultConfig(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote default config to %s\n", path)
	return nil
}

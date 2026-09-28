package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/semigodking/clisesh/internal/cache"
	"github.com/semigodking/clisesh/internal/config"
	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/summarize"
	"github.com/semigodking/clisesh/internal/summarize/llm"
	"github.com/semigodking/clisesh/internal/summarize/subprocess"
)

// buildSummarizer constructs the configured summarization backend.
func buildSummarizer(cfg config.Config) (summarize.Summarizer, error) {
	s := cfg.Summarizer
	titleLen := s.TitleLen
	if titleLen <= 0 {
		titleLen = cfg.TitleLen
	}
	defaultTimeout := time.Duration(s.TimeoutSec) * time.Second

	kind, _, spCfg, llmCfg, err := s.ResolveBackend()
	if err != nil {
		return nil, fmt.Errorf("resolving summarizer backend: %w", err)
	}

	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = summarize.DefaultBatchSize
	}
	maxChars := s.MaxChars
	if maxChars <= 0 {
		maxChars = 1500
	}

	switch kind {
	case "subprocess":
		timeout := defaultTimeout
		if spCfg.TimeoutSec > 0 {
			timeout = time.Duration(spCfg.TimeoutSec) * time.Second
		}
		model := spCfg.Model
		if model == "" {
			model = s.Model
		}
		return subprocess.NewWithOptions(spCfg.Command, spCfg.Args, model, titleLen, timeout, batchSize, maxChars), nil
	case "llm":
		timeout := defaultTimeout
		if llmCfg.TimeoutSec > 0 {
			timeout = time.Duration(llmCfg.TimeoutSec) * time.Second
		}
		model := llmCfg.Model
		if model == "" {
			model = s.Model
		}
		return llm.NewWithOptions(llmCfg.BaseURL, llmCfg.APIKey, model, llmCfg.Temperature, llmCfg.Format, llmCfg.Headers, titleLen, timeout, batchSize, maxChars), nil
	default:
		return nil, fmt.Errorf("unsupported summarizer backend kind %q", kind)
	}
}

// runSummarize implements `clisesh summarize [flags] <id...>`, generating and
// caching descriptions for the given session ids (or all sessions if none).
func runSummarize(args []string) {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config file")
	providerName := fs.String("provider", "", "provider")
	baseDir := fs.String("dir", "", "override provider base/session directory")
	backend := fs.String("backend", "", "summarizer backend (e.g. subprocess.agy, llm.deepseek)")
	_ = fs.Parse(args)
	ids := fs.Args()

	cfg, err := config.Load(config.Flags{
		ConfigPath: *cfgPath,
		Provider:   *providerName,
		Backend:    *backend,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	prov, err := newProvider(cfg.Provider, *baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	summ, err := buildSummarizer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	sessions, err := prov.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list error: %v\n", err)
		os.Exit(1)
	}

	// Build the item set (filter to requested ids if any).
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var items []summarize.Item
	byID := map[string]domain.Session{}
	for _, s := range sessions {
		byID[s.ID] = s
		if len(want) > 0 && !want[s.ID] {
			continue
		}
		prompts, _ := prov.UserPrompts(ctx, s.ID, 10)
		items = append(items, summarize.Item{ID: s.ID, Title: s.Title, Prompts: prompts})
	}

	descs, err := summ.Summarize(ctx, items)
	if err != nil {
		fmt.Fprintf(os.Stderr, "summarize error: %v\n", err)
		os.Exit(1)
	}

	c := cache.Open(cfg.CacheDir, cfg.Provider)
	for id, d := range descs {
		fmt.Printf("%s: %s\n", domain.Session{ID: id}.ShortID(), d)
		c.Set(id, d)
	}
	if err := c.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "cache save error: %v\n", err)
	}
}

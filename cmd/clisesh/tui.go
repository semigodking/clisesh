package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/semigodking/clisesh/internal/cache"
	"github.com/semigodking/clisesh/internal/config"
	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/summarize"
	"github.com/semigodking/clisesh/internal/theme"
	"github.com/semigodking/clisesh/internal/tui"
)

// runTUI loads config, builds the provider/summarizer/theme, prepares sessions
// (filling descriptions from cache, generating any missing), launches the
// Bubble Tea picker, and resumes the chosen session after the UI exits.
func runTUI(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	cfgPath := fs.String("config", "", "path to config file")
	providerName := fs.String("provider", "", "provider")
	themeName := fs.String("theme", "", "theme")
	baseDir := fs.String("dir", "", "override provider base/session directory")
	_ = fs.Parse(args)

	cfg, err := config.Load(config.Flags{
		ConfigPath: *cfgPath,
		Provider:   *providerName,
		Theme:      *themeName,
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
	th, err := theme.Load(cfg.Theme, cfg.Overrides)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	summ, err := buildSummarizer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sessions, err := prov.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list error: %v\n", err)
		os.Exit(1)
	}

	c := cache.Open(cfg.CacheDir, cfg.Provider)
	sessions, missing := classifyDescriptions(ctx, prov, c, sessions)

	var pendingIDs []string
	promptsCache := make(map[string][]string)
	if summ != nil {
		for _, it := range missing {
			pendingIDs = append(pendingIDs, it.ID)
			if len(it.Prompts) > 0 {
				promptsCache[it.ID] = it.Prompts
			}
		}
	}
	// Initial display for sessions: for non-pending sessions, ensure fallback descriptions are filled;
	// for pending ones, mark them as Summarizing… so the UI immediately reflects in-flight status.
	idset := map[string]bool{}
	for _, id := range pendingIDs {
		idset[id] = true
	}
	for i := range sessions {
		if idset[sessions[i].ID] {
			sessions[i].Description = "Queued for summary…"
		} else if sessions[i].Description == "" {
			if sessions[i].Title != "" {
				sessions[i].Description = sessions[i].Title
			} else {
				sessions[i].Description = summarize.NoDescription
			}
		}
	}
	_ = c.Save()

	model := tui.New(prov, summ, c, th, sessions, pendingIDs, promptsCache, ctx, cancel)
	if cfg.Summarizer.BatchSize > 0 {
		model.SetBatchSize(cfg.Summarizer.BatchSize)
	}
	if cfg.Summarizer.MaxPrompts > 0 {
		model.SetMaxPrompts(cfg.Summarizer.MaxPrompts)
	}
	p := tea.NewProgram(model, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}

	if fm, ok := final.(tui.Model); ok {
		if id := fm.ResumeID(); id != "" {
			// Cancel any remaining background jobs before process replacement.
			cancel()
			// Replaces the current process (exec) on success.
			if err := prov.Resume(context.Background(), id); err != nil {
				fmt.Fprintf(os.Stderr, "resume error: %v\n", err)
				os.Exit(1)
			}
		}
	}
}

// classifyDescriptions resolves descriptions that can be decided without an
// LLM and collects the rest into missing. A non-empty cached description is
// trusted as-is. A cache miss OR a cached EmptySession triggers a fresh prompt
// fetch: if the session is still empty it is (re)marked EmptySession and cached
// (persisted for stability) and excluded from missing; otherwise it is queued
// for summarization (decisions 1a and 2a).
func classifyDescriptions(ctx context.Context, prov domain.SessionProvider, c *cache.Cache, sessions []domain.Session) (out []domain.Session, missing []summarize.Item) {
	return classifyDescriptionsWithMax(ctx, prov, c, sessions, 10)
}

func classifyDescriptionsWithMax(ctx context.Context, prov domain.SessionProvider, c *cache.Cache, sessions []domain.Session, maxPrompts int) (out []domain.Session, missing []summarize.Item) {
	if maxPrompts <= 0 {
		maxPrompts = 10
	}
	for i := range sessions {
		if d, ok := c.Get(sessions[i].ID); ok && d != summarize.EmptySession {
			sessions[i].Description = d
			continue
		}
		// Cache miss or cached empty sentinel: re-check the actual prompts.
		prompts, _ := prov.UserPrompts(ctx, sessions[i].ID, maxPrompts)
		if summarize.IsEmpty(prompts) {
			sessions[i].Description = summarize.EmptySession
			c.Set(sessions[i].ID, summarize.EmptySession)
			continue
		}
		missing = append(missing, summarize.Item{
			ID:      sessions[i].ID,
			Title:   sessions[i].Title,
			Prompts: prompts,
		})
	}
	return sessions, missing
}

// applyDescriptions writes freshly generated descriptions onto sessions and the
// cache, then applies a safety net: any still-empty Description falls back to
// the session title, or NoDescription when there is no title. The caller is
// responsible for persisting the cache (a single Save).
func applyDescriptions(sessions []domain.Session, descs map[string]string, c *cache.Cache) []domain.Session {
	for i := range sessions {
		if d, ok := descs[sessions[i].ID]; ok {
			sessions[i].Description = d
			if c != nil {
				c.Set(sessions[i].ID, d)
			}
		}
	}
	for i := range sessions {
		if sessions[i].Description == "" {
			if sessions[i].Title != "" {
				sessions[i].Description = sessions[i].Title
			} else {
				sessions[i].Description = summarize.NoDescription
			}
		}
	}
	return sessions
}

// fillDescriptions is a synchronous convenience that classifies sessions,
// summarizes any missing items when a summarizer is available, and applies the
// results. It is used by tests and non-interactive callers; runTUI uses the
// spinner-driven variant. The cache is persisted here.
func fillDescriptions(ctx context.Context, prov domain.SessionProvider, summ summarize.Summarizer, c *cache.Cache, cfg config.Config, sessions []domain.Session) []domain.Session {
	sessions, missing := classifyDescriptions(ctx, prov, c, sessions)
	descs := map[string]string{}
	if len(missing) > 0 && summ != nil {
		if d, err := summ.Summarize(ctx, missing); err == nil {
			descs = d
		}
	}
	sessions = applyDescriptions(sessions, descs, c)
	_ = c.Save()
	return sessions
}

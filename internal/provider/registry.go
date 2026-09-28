// Package provider maintains a registry mapping provider names to factories
// that build a domain.SessionProvider. Concrete providers register themselves
// via Register (typically in an init function), so cmd/clisesh can construct
// the configured provider by name without importing each provider package
// directly for selection.
package provider

import (
	"fmt"
	"sort"
	"sync"

	"github.com/semigodking/clisesh/internal/domain"
)

// Factory builds a provider instance. baseDir, when non-empty, overrides the
// provider's default storage location (used for testing); providers that do
// not support an override may ignore it.
type Factory func(baseDir string) (domain.SessionProvider, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register associates a provider name with its factory. Called from provider
// package init functions.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[name] = f
}

// New constructs the named provider. baseDir may be "" for the default path.
func New(name, baseDir string) (domain.SessionProvider, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (have: %v)", name, Names())
	}
	return f(baseDir)
}

// Names returns the sorted list of registered provider names.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for n := range factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// All constructs every registered provider using default locations. Providers
// that fail to construct are skipped. Used for the aggregate ("all") view.
func All() []domain.SessionProvider {
	names := Names()
	out := make([]domain.SessionProvider, 0, len(names))
	for _, n := range names {
		if p, err := New(n, ""); err == nil {
			out = append(out, p)
		}
	}
	return out
}

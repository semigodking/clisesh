package main

import (
	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/provider"
	"github.com/semigodking/clisesh/internal/provider/aggregate"
)

// newProvider builds the session provider for the configured name. The special
// name "all" (or empty) aggregates every registered provider. baseDir, when
// set, overrides a single provider's session directory (ignored for "all").
func newProvider(name, baseDir string) (domain.SessionProvider, error) {
	if name == aggregate.ID || name == "" {
		return aggregate.New(provider.All()), nil
	}
	return provider.New(name, baseDir)
}

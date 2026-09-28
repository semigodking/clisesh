// Package aggregate combines multiple SessionProviders into one, presenting a
// merged session list across all supported CLIs. Per-session operations are
// routed to the owning provider (by Session.ProviderID). It is itself a
// domain.SessionProvider, so the TUI treats "all providers" identically to a
// single provider.
package aggregate

import (
	"context"
	"sort"

	"github.com/semigodking/clisesh/internal/domain"
)

// ID is the synthetic provider id used when aggregating.
const ID = "all"

// Provider aggregates several child providers.
type Provider struct {
	children map[string]domain.SessionProvider // keyed by child ID
	order    []string                          // stable child ordering
}

// New builds an aggregate over the given child providers.
func New(children []domain.SessionProvider) *Provider {
	p := &Provider{children: map[string]domain.SessionProvider{}}
	for _, c := range children {
		p.children[c.ID()] = c
		p.order = append(p.order, c.ID())
	}
	sort.Strings(p.order)
	return p
}

// ID returns the synthetic aggregate id.
func (p *Provider) ID() string { return ID }

// Capabilities returns the union of child capabilities. Whether a specific
// action is possible for a given session is decided at routing time (an action
// on a session whose provider lacks the capability is a safe no-op/error).
func (p *Provider) Capabilities() domain.Capabilities {
	var c domain.Capabilities
	for _, child := range p.children {
		cc := child.Capabilities()
		c.HasTitle = c.HasTitle || cc.HasTitle
		c.HasParent = c.HasParent || cc.HasParent
		c.CanResume = c.CanResume || cc.CanResume
		c.CanDelete = c.CanDelete || cc.CanDelete
		c.CascadeDelete = c.CascadeDelete || cc.CascadeDelete
		c.ProvidesPrompts = c.ProvidesPrompts || cc.ProvidesPrompts
	}
	return c
}

// List merges sessions from all children, newest-first.
func (p *Provider) List(ctx context.Context) ([]domain.Session, error) {
	var all []domain.Session
	for _, name := range p.order {
		sessions, err := p.children[name].List(ctx)
		if err != nil {
			continue // a failing provider must not break the whole list
		}
		all = append(all, sessions...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	return all, nil
}

// child looks up the owning provider for a session id by consulting each
// child's list. Because ids are UUIDs this is unambiguous; we cache nothing
// to keep the routing simple and always-correct.
func (p *Provider) childFor(ctx context.Context, id string) domain.SessionProvider {
	for _, name := range p.order {
		sessions, err := p.children[name].List(ctx)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if s.ID == id {
				return p.children[name]
			}
		}
	}
	return nil
}

// UserPrompts routes to the owning provider.
func (p *Provider) UserPrompts(ctx context.Context, id string, max int) ([]string, error) {
	if c := p.childFor(ctx, id); c != nil {
		return c.UserPrompts(ctx, id, max)
	}
	return nil, nil
}

// ValidateResume routes to the owning provider.
func (p *Provider) ValidateResume(ctx context.Context, id string) error {
	if c := p.childFor(ctx, id); c != nil {
		return c.ValidateResume(ctx, id)
	}
	return domain.ErrNotSupported
}

// Resume routes to the owning provider.
func (p *Provider) Resume(ctx context.Context, id string) error {
	if c := p.childFor(ctx, id); c != nil {
		return c.Resume(ctx, id)
	}
	return domain.ErrNotSupported
}

// Children routes to the owning provider.
func (p *Provider) Children(ctx context.Context, id string) ([]string, error) {
	if c := p.childFor(ctx, id); c != nil {
		return c.Children(ctx, id)
	}
	return nil, nil
}

// Delete groups ids by owning provider and delegates, merging deleted ids.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	byChild := map[string][]string{}
	for _, id := range ids {
		if c := p.childFor(ctx, id); c != nil {
			byChild[c.ID()] = append(byChild[c.ID()], id)
		}
	}
	var deleted []string
	for childID, group := range byChild {
		d, err := p.children[childID].Delete(ctx, group)
		if err != nil {
			continue
		}
		deleted = append(deleted, d...)
	}
	sort.Strings(deleted)
	return deleted, nil
}

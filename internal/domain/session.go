// Package domain defines the core, dependency-free model shared across
// clisesh: the normalized Session, provider Capabilities, and the
// SessionProvider interface. No package here may import provider, tui,
// config, theme, cache, or summarize packages — dependencies point inward.
package domain

import "time"

// Session is the provider-neutral representation of a single agent
// conversation. Concrete providers (Kiro, Antigravity, ...) map their
// on-disk formats onto this struct so the rest of the app never needs to
// know about provider-specific storage layouts.
type Session struct {
	// ID is the provider-native unique identifier (usually a UUID).
	ID string
	// ProviderID identifies which provider owns this session (e.g. "kiro").
	ProviderID string
	// Title is the session's own title if the provider records one. May be
	// empty for providers that do not store titles (e.g. Antigravity).
	Title string
	// Description is the cached or freshly generated human-readable summary
	// used for display. It is populated by the app, not the provider.
	Description string
	// CWD is the working directory the session was started in, if known.
	CWD string
	// UpdatedAt is the last-modified time used for sorting (newest first).
	UpdatedAt time.Time
	// Locked reports whether the session is currently active/live and must
	// not be deleted by bulk operations.
	Locked bool
	// ParentID links a subagent session to the session that spawned it.
	// Empty for top-level sessions or providers without parent linkage.
	ParentID string
	// Raw holds provider-specific metadata for debugging or advanced use.
	Raw map[string]any
}

// IsSubagent reports whether this session was spawned by another session.
func (s Session) IsSubagent() bool {
	return s.ParentID != ""
}

// ShortID returns the first 8 characters of the ID for compact display,
// or the whole ID if it is shorter.
func (s Session) ShortID() string {
	if len(s.ID) <= 8 {
		return s.ID
	}
	return s.ID[:8]
}

// Capabilities advertises which features a provider supports so the UI can
// adapt gracefully — hiding or disabling actions a provider cannot perform.
type Capabilities struct {
	// HasTitle is true if the provider stores a session title.
	HasTitle bool
	// HasParent is true if the provider records parent/child (subagent) links.
	HasParent bool
	// CanResume is true if sessions can be resumed.
	CanResume bool
	// CanDelete is true if sessions can be deleted.
	CanDelete bool
	// CascadeDelete is true if deleting a session should also delete its
	// child (subagent) sessions.
	CascadeDelete bool
	// ProvidesPrompts is true if the provider can extract the user's prompts
	// from a session (used as summarization input).
	ProvidesPrompts bool
}

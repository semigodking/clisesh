package domain

import (
	"context"
	"errors"
)

// ErrNotSupported is returned by provider methods for capabilities the
// provider does not implement. Callers should check Capabilities() before
// invoking optional operations, but methods must still fail safely.
var ErrNotSupported = errors.New("operation not supported by provider")

// SessionProvider is the abstraction that lets clisesh support multiple
// agent CLIs (Kiro, Antigravity, ...) without architectural change. A
// provider maps a specific on-disk (or remote) session store onto the
// normalized domain.Session model and advertises its Capabilities so the
// UI adapts to what it can and cannot do.
type SessionProvider interface {
	// ID returns the stable provider identifier (e.g. "kiro").
	ID() string

	// Capabilities reports which optional features this provider supports.
	Capabilities() Capabilities

	// List returns all user-facing sessions, newest first. Providers should
	// exclude sessions the UI must never show (e.g. subagent sessions when
	// HasParent is true) unless the caller opts in via configuration.
	List(ctx context.Context) ([]Session, error)

	// UserPrompts returns up to max of the user's prompt texts for a session,
	// used as input for summarization. Returns ErrNotSupported if the
	// provider cannot extract prompts (ProvidesPrompts == false).
	UserPrompts(ctx context.Context, id string, max int) ([]string, error)

	// Resume hands control to the underlying CLI to resume a session.
	// Implementations typically exec/replace the current process.
	// Returns ErrNotSupported if CanResume == false.
	Resume(ctx context.Context, id string) error

	// ValidateResume checks whether a session can be resumed (e.g. underlying CLI
	// executable is installed on PATH) without exiting or replacing the process.
	ValidateResume(ctx context.Context, id string) error

	// Delete removes the given sessions and returns the IDs actually deleted
	// (which may include cascaded children when CascadeDelete == true).
	// Returns ErrNotSupported if CanDelete == false.
	Delete(ctx context.Context, ids []string) (deleted []string, err error)

	// Children returns the IDs of subagent sessions whose parent is id.
	// Returns nil (not an error) when HasParent == false.
	Children(ctx context.Context, id string) ([]string, error)
}

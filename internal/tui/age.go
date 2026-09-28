package tui

import (
	"time"

	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/textutil"
)

// nowFunc is overridable in tests for deterministic age rendering.
var nowFunc = func() time.Time { return time.Now().UTC() }

// ageOf returns the human-readable age of a session.
func ageOf(s domain.Session) string {
	return textutil.FormatAge(s.UpdatedAt, nowFunc())
}

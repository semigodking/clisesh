// Package tui implements the Bubble Tea full-screen session picker. It depends
// only on domain, theme, cache, and summarize — never on a concrete provider —
// and adapts its behavior to the provider's advertised Capabilities.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/semigodking/clisesh/internal/cache"
	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/summarize"
	"github.com/semigodking/clisesh/internal/textutil"
	"github.com/semigodking/clisesh/internal/theme"
)

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeConfirmDelete
	modeInfo
	modeError
	modeEditDesc
)

// summarizeQueueState holds background summarization queue state.
// A pointer to this struct is shared across Bubbletea value-receiver method calls
// (such as Init and Update) so that mutations to queue and in-flight tracking
// are never lost.  The progress status string also lives here so that
// Init()'s side-effects (which run inside a value-receiver copy) are visible
// to the outer model that Bubbletea uses for rendering.
type summarizeQueueState struct {
	inFlight   map[string]bool
	queue      []string
	total      int
	done       int
	success    int
	batchSize  int
	maxPrompts int
	// origDescs preserves the description of each session before it was marked
	// as "Summarizing…" or "Queued for summary…", so that if summarization fails
	// or is aborted, the session is restored to its exact previous state.
	origDescs map[string]string
	// status is the in-progress summarize message (e.g. "summarizing (2/5)…").
	// Storing it here—rather than in Model.status—ensures that the value set
	// during Init()'s popAndDispatchChunk call is visible in the outer model
	// that Bubbletea holds, because both share this pointer.
	status string
}

// Model is the Bubble Tea model for the picker.
type Model struct {
	provider   domain.SessionProvider
	summarizer summarize.Summarizer
	cache      *cache.Cache
	theme      theme.Theme
	caps       domain.Capabilities

	sessions []domain.Session
	cursor   int
	offset   int
	marked   map[string]bool
	mode     mode
	filter   string

	width, height int
	status        string

	lastErr    string // full multiline error text from last operation failure
	errOffset  int    // scroll offset in error dialog
	infoOffset int    // scroll offset in session info dialog

	// Description editing state
	editInput     textinput.Model
	editSessionID string

	// resumeID is set to the chosen session id when the user resumes; main
	// performs the actual exec after the TUI exits so the alt-screen is torn
	// down cleanly first.
	resumeID string

	quitting bool

	ctx    context.Context
	cancel context.CancelFunc

	pendingSummaries []string
	// promptsCache holds pre-fetched prompts (id → []string) from
	// classifyDescriptions so dispatchChunk can avoid a second prov.UserPrompts
	// call. A second read can fail when the session file is locked or modified
	// by the active CLI between the two reads.
	promptsCache map[string][]string

	summState *summarizeQueueState
}

// New builds a Model. sessions should already carry Description (from cache or
// placeholder) and be sorted newest-first.
// promptsCache may be nil; when provided it maps session ID to pre-fetched
// user prompts (from classifyDescriptions) so that dispatchChunk can avoid a
// redundant prov.UserPrompts call per session.
func New(p domain.SessionProvider, s summarize.Summarizer, c *cache.Cache, th theme.Theme, sessions []domain.Session, pendingSummaries []string, promptsCache map[string][]string, ctx context.Context, cancel context.CancelFunc) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	ti := textinput.New()
	ti.Prompt = "Description: "
	ti.CharLimit = 256
	return Model{
		provider:         p,
		summarizer:       s,
		cache:            c,
		theme:            th,
		caps:             p.Capabilities(),
		sessions:         sessions,
		marked:           map[string]bool{},
		mode:             modeNormal,
		ctx:              ctx,
		cancel:           cancel,
		pendingSummaries: pendingSummaries,
		promptsCache:     promptsCache,
		editInput:        ti,
		summState: &summarizeQueueState{
			inFlight:   map[string]bool{},
			origDescs:  map[string]string{},
			batchSize:  1, // 1 session per chunk so each record updates UI immediately when finished
			maxPrompts: 10,
		},
	}
}

// openEditDesc opens the description editing dialog for a session.
func (m *Model) openEditDesc(s domain.Session) tea.Cmd {
	m.mode = modeEditDesc
	m.editSessionID = s.ID
	initVal := s.Description
	if initVal == summarizingPlaceholder || initVal == queuedPlaceholder || initVal == summarize.NoDescription {
		if s.Title != "" {
			initVal = s.Title
		} else {
			initVal = ""
		}
	}
	m.editInput.SetValue(initVal)
	m.editInput.CursorEnd()
	return m.editInput.Focus()
}

// saveEditedDesc persists the edited description in memory and in the cache.
func (m *Model) saveEditedDesc() {
	newDesc := strings.TrimSpace(m.editInput.Value())
	if newDesc == "" {
		newDesc = summarize.NoDescription
	}
	id := m.editSessionID
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			m.sessions[i].Description = newDesc
			break
		}
	}
	if m.summState != nil && m.summState.origDescs != nil {
		m.summState.origDescs[id] = newDesc
	}
	if m.cache != nil {
		m.cache.Set(id, newDesc)
		_ = m.cache.Save()
	}
	m.mode = modeNormal
	m.editSessionID = ""
	m.editInput.Blur()
	m.status = "description updated"
}

// cancelEditDesc discards editing and returns to normal mode.
func (m *Model) cancelEditDesc() {
	m.mode = modeNormal
	m.editSessionID = ""
	m.editInput.Blur()
}

// SetBatchSize configures the chunk size for summarization requests.
func (m *Model) SetBatchSize(n int) {
	if n > 0 && m.summState != nil {
		m.summState.batchSize = n
	}
}

// SetMaxPrompts configures how many prompts to extract per session during summarization.
func (m *Model) SetMaxPrompts(n int) {
	if n > 0 && m.summState != nil {
		m.summState.maxPrompts = n
	}
}

// Init implements tea.Model. It launches background auto-summarization on startup if there are pending sessions.
func (m Model) Init() tea.Cmd {
	if len(m.pendingSummaries) > 0 {
		return m.startSummarize(m.pendingSummaries, "summarizing")
	}
	return nil
}

// ResumeID returns the session id the user chose to resume (empty if none).
func (m Model) ResumeID() string { return m.resumeID }

// filtered returns the sessions matching the current filter (case-insensitive
// over title, description, and short id).
func (m Model) filtered() []domain.Session {
	if m.filter == "" {
		return m.sessions
	}
	needle := strings.ToLower(m.filter)
	var out []domain.Session
	for _, s := range m.sessions {
		hay := strings.ToLower(s.Title + " " + s.Description + " " + s.ShortID())
		if strings.Contains(hay, needle) {
			out = append(out, s)
		}
	}
	return out
}

// current returns the session under the cursor, or false if none.
func (m Model) current() (domain.Session, bool) {
	f := m.filtered()
	if m.cursor < 0 || m.cursor >= len(f) {
		return domain.Session{}, false
	}
	return f[m.cursor], true
}

// summarizingPlaceholder is shown on a session's description while a
// background summarization call is actively in flight for it.
const summarizingPlaceholder = "Summarizing…"

// queuedPlaceholder is shown on a session's description when it is scheduled
// in the summarization queue and waiting for its turn.
const queuedPlaceholder = "Queued for summary…"

// summarizeChunkDoneMsg carries the results of a single chunk summarization.
type summarizeChunkDoneMsg struct {
	descs      map[string]string
	ids        []string // the ids that were in this chunk
	actionVerb string
	err        error
}

// resummarizeDoneMsg is kept for test backwards-compatibility.
type resummarizeDoneMsg = summarizeChunkDoneMsg

// startSummarize enqueues target IDs, immediately sets active vs queued placeholders,
// and returns a command that starts processing the first chunk.
func (m *Model) startSummarize(ids []string, actionVerb string) tea.Cmd {
	if m.summarizer == nil || !m.caps.ProvidesPrompts || len(ids) == 0 {
		return nil
	}
	if m.summState == nil {
		m.summState = &summarizeQueueState{
			inFlight:   map[string]bool{},
			batchSize:  1,
			maxPrompts: 10,
		}
	}
	if m.summState.inFlight == nil {
		m.summState.inFlight = map[string]bool{}
	}

	activeOrQueued := map[string]bool{}
	for id := range m.summState.inFlight {
		activeOrQueued[id] = true
	}
	for _, id := range m.summState.queue {
		activeOrQueued[id] = true
	}

	var freshIDs []string
	for _, id := range ids {
		if !activeOrQueued[id] {
			freshIDs = append(freshIDs, id)
			activeOrQueued[id] = true
		}
	}
	if len(freshIDs) == 0 {
		m.status = "already summarizing selected session(s)"
		return nil
	}

	m.lastErr = ""
	m.errOffset = 0

	if actionVerb == "" {
		actionVerb = "summarizing"
	}

	if m.summState.origDescs == nil {
		m.summState.origDescs = map[string]string{}
	}

	// Capture original descriptions before updating placeholders, so failures can be restored.
	for _, id := range freshIDs {
		if _, already := m.summState.origDescs[id]; !already {
			for _, s := range m.sessions {
				if s.ID == id {
					if s.Description != summarizingPlaceholder && s.Description != queuedPlaceholder {
						m.summState.origDescs[id] = s.Description
					} else if m.cache != nil {
						if cached, ok := m.cache.Get(id); ok && cached != summarizingPlaceholder && cached != queuedPlaceholder {
							m.summState.origDescs[id] = cached
						}
					}
					break
				}
			}
		}
	}

	// Enqueue all fresh IDs.
	m.summState.queue = append(m.summState.queue, freshIDs...)
	m.summState.total += len(freshIDs)

	// Mark newly enqueued sessions as queued initially.
	freshSet := map[string]bool{}
	for _, id := range freshIDs {
		freshSet[id] = true
	}
	for i := range m.sessions {
		if freshSet[m.sessions[i].ID] {
			m.sessions[i].Description = queuedPlaceholder
		}
	}

	// Fill available worker pool slots.
	return m.refillWorkers(actionVerb)
}

// restoreDescription recovers the original description of a session when summarization fails or is aborted.
// Precedence: original snapshot description > cached description > session title > NoDescription.
func (m *Model) restoreDescription(id string, title string) string {
	if m.summState != nil && m.summState.origDescs != nil {
		if orig, ok := m.summState.origDescs[id]; ok && orig != "" && orig != summarizingPlaceholder && orig != queuedPlaceholder {
			return orig
		}
	}
	if m.cache != nil {
		if cached, ok := m.cache.Get(id); ok && cached != "" && cached != summarizingPlaceholder && cached != queuedPlaceholder {
			return cached
		}
	}
	if title != "" {
		return title
	}
	return summarize.NoDescription
}

// refillWorkers fills available worker pool slots (up to batchSize) from queue.
func (m *Model) refillWorkers(actionVerb string) tea.Cmd {
	if m.summState == nil || len(m.summState.queue) == 0 {
		return nil
	}

	poolSize := m.summState.batchSize
	if poolSize <= 0 {
		poolSize = 1
	}
	availableSlots := poolSize - len(m.summState.inFlight)
	if availableSlots <= 0 {
		return nil
	}
	if availableSlots > len(m.summState.queue) {
		availableSlots = len(m.summState.queue)
	}

	nextIDs := append([]string(nil), m.summState.queue[:availableSlots]...)
	m.summState.queue = m.summState.queue[availableSlots:]

	nextSet := map[string]bool{}
	for _, id := range nextIDs {
		nextSet[id] = true
		m.summState.inFlight[id] = true
	}

	for i := range m.sessions {
		if nextSet[m.sessions[i].ID] {
			m.sessions[i].Description = summarizingPlaceholder
		}
	}

	if actionVerb == "" {
		actionVerb = "summarizing"
	}
	m.updateSummarizeStatus(actionVerb)

	var cmds []tea.Cmd
	for _, id := range nextIDs {
		cmds = append(cmds, m.dispatchSingle(id, actionVerb))
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// updateSummarizeStatus updates status and summState.status.
func (m *Model) updateSummarizeStatus(actionVerb string) {
	if m.summState == nil {
		return
	}
	currentNum := m.summState.done + 1
	if currentNum > m.summState.total {
		currentNum = m.summState.total
	}
	activeCount := len(m.summState.inFlight)
	m.summState.status = fmt.Sprintf("%s (%d/%d): %d session(s)…", actionVerb, currentNum, m.summState.total, activeCount)
	m.status = m.summState.status
}

// dispatchSingle builds the async tea.Cmd to summarize one single session.
func (m *Model) dispatchSingle(id string, actionVerb string) tea.Cmd {
	prov := m.provider
	summ := m.summarizer
	ctx := m.ctx

	title := ""
	for _, s := range m.sessions {
		if s.ID == id {
			title = s.Title
			break
		}
	}

	var cachedPrompts []string
	var hasCached bool
	if m.promptsCache != nil {
		cachedPrompts, hasCached = m.promptsCache[id]
	}

	targetID := id
	maxP := 10
	if m.summState != nil && m.summState.maxPrompts > 0 {
		maxP = m.summState.maxPrompts
	}

	return func() tea.Msg {
		if ctx.Err() != nil {
			return summarizeChunkDoneMsg{ids: []string{targetID}, actionVerb: actionVerb, err: ctx.Err()}
		}
		prompts := cachedPrompts
		if !hasCached {
			prompts, _ = prov.UserPrompts(ctx, targetID, maxP)
		}
		sessionTitle := ""
		if len(prompts) == 0 {
			sessionTitle = title
		}
		item := summarize.Item{ID: targetID, Title: sessionTitle, Prompts: prompts}
		descs, err := summ.Summarize(ctx, []summarize.Item{item})
		return summarizeChunkDoneMsg{descs: descs, ids: []string{targetID}, actionVerb: actionVerb, err: err}
	}
}

// startResummarize is a wrapper around startSummarize for user-triggered re-summarize.
func (m *Model) startResummarize(ids []string) tea.Cmd {
	return m.startSummarize(ids, "re-summarizing")
}

// applyResummarize handles completed chunk results and kicks off the next chunk if any.
func (m *Model) applyResummarize(msg summarizeChunkDoneMsg) tea.Cmd {
	if m.summState == nil {
		m.summState = &summarizeQueueState{inFlight: map[string]bool{}}
	}

	// Release finished chunk IDs from the in-flight tracking set.
	for _, id := range msg.ids {
		delete(m.summState.inFlight, id)
	}

	// Check how many of msg.ids are still alive in m.sessions.
	surviving := map[string]bool{}
	for _, s := range m.sessions {
		surviving[s.ID] = true
	}
	aliveCount := 0
	for _, id := range msg.ids {
		if surviving[id] {
			aliveCount++
		}
	}

	m.summState.done += len(msg.ids)

	if msg.err != nil {
		if msg.err != context.Canceled && aliveCount > 0 {
			m.lastErr = msg.err.Error()
			m.errOffset = 0
			cleanErr := textutil.SingleLine(msg.err.Error())
			m.status = "summarize failed: " + cleanErr
			m.summState.status = "" // clear in-progress status on error
		}
		// Restore descriptions for failed chunk so they don't stay stuck on placeholder.
		for i := range m.sessions {
			for _, id := range msg.ids {
				if m.sessions[i].ID == id && m.sessions[i].Description == summarizingPlaceholder {
					m.sessions[i].Description = m.restoreDescription(id, m.sessions[i].Title)
					if m.summState != nil && m.summState.origDescs != nil {
						delete(m.summState.origDescs, id)
					}
				}
			}
		}

		// On failure, abort remaining queued sessions rather than continuing to burn
		// through chunks that will also fail. Revert their descriptions back to their original state.
		queuedSet := map[string]bool{}
		for _, qid := range m.summState.queue {
			queuedSet[qid] = true
		}
		for i := range m.sessions {
			id := m.sessions[i].ID
			if queuedSet[id] && m.sessions[i].Description == queuedPlaceholder {
				m.sessions[i].Description = m.restoreDescription(id, m.sessions[i].Title)
				if m.summState != nil && m.summState.origDescs != nil {
					delete(m.summState.origDescs, id)
				}
			}
		}
		m.summState.queue = nil
		m.summState.total = 0
		m.summState.done = 0
		m.summState.status = ""
		m.summState.success = 0
		return nil
	} else if aliveCount > 0 {
		m.lastErr = ""
		m.errOffset = 0
		for i := range m.sessions {
			id := m.sessions[i].ID
			if d, ok := msg.descs[id]; ok {
				m.sessions[i].Description = d
				m.summState.success++
				if m.summState != nil && m.summState.origDescs != nil {
					delete(m.summState.origDescs, id)
				}
				if m.cache != nil {
					m.cache.Set(id, d)
				}
			} else {
				for _, mid := range msg.ids {
					if id == mid && m.sessions[i].Description == summarizingPlaceholder {
						m.sessions[i].Description = m.restoreDescription(id, m.sessions[i].Title)
						if m.summState != nil && m.summState.origDescs != nil {
							delete(m.summState.origDescs, id)
						}
					}
				}
			}
		}
		if m.cache != nil {
			_ = m.cache.Save()
		}
	}

	// Filter summarizeQueue to prune any sessions deleted in the meantime.
	var prunedQueue []string
	for _, id := range m.summState.queue {
		if surviving[id] {
			prunedQueue = append(prunedQueue, id)
		}
	}
	m.summState.queue = prunedQueue

	// If there are more queued items, refill any open worker slots!
	var nextCmd tea.Cmd
	if len(m.summState.queue) > 0 {
		nextCmd = m.refillWorkers(msg.actionVerb)
	}

	// When all queued sessions and in-flight tasks have finished:
	if len(m.summState.queue) == 0 && len(m.summState.inFlight) == 0 {
		m.summState.total = 0
		m.summState.done = 0
		m.summState.status = "" // clear in-progress status; final result goes to m.status
		finalSuccess := m.summState.success
		m.summState.success = 0
		m.summState.origDescs = map[string]string{}
		if m.lastErr == "" && (finalSuccess > 0 || aliveCount > 0) {
			m.status = fmt.Sprintf("summarized %d session(s)", finalSuccess)
		}
		return nextCmd
	}

	// Still have in-flight workers running: update progress status if no error.
	if m.lastErr == "" && len(m.summState.inFlight) > 0 {
		m.updateSummarizeStatus(msg.actionVerb)
	}

	return nextCmd
}

// actionTargets returns marked session ids, else cursor session id.
func (m Model) actionTargets() []string {
	if len(m.marked) > 0 {
		var ids []string
		for _, s := range m.sessions {
			if m.marked[s.ID] {
				ids = append(ids, s.ID)
			}
		}
		return ids
	}
	if s, ok := m.current(); ok {
		return []string{s.ID}
	}
	return nil
}

// deleteTargets returns the deletable (non-locked) ids to delete (marked, else cursor).
// Any locked session is excluded from deletion to prevent crashing running processes.
func (m Model) deleteTargets() []string {
	var ids []string
	if len(m.marked) > 0 {
		for _, s := range m.sessions {
			if m.marked[s.ID] && !s.Locked {
				ids = append(ids, s.ID)
			}
		}
		return ids
	}
	if s, ok := m.current(); ok {
		if !s.Locked {
			return []string{s.ID}
		}
	}
	return nil
}

// hasActiveTarget returns true if any candidate for deletion is currently locked.
func (m Model) hasActiveTarget() bool {
	if len(m.marked) > 0 {
		for _, s := range m.sessions {
			if m.marked[s.ID] && s.Locked {
				return true
			}
		}
		return false
	}
	if s, ok := m.current(); ok {
		return s.Locked
	}
	return false
}

// doDelete performs the deletion via the provider and removes deleted sessions
// (and cache entries) from state.
func (m *Model) doDelete(ids []string) {
	if !m.caps.CanDelete || len(ids) == 0 {
		return
	}
	deleted, err := m.provider.Delete(context.Background(), ids)
	if err != nil {
		m.status = "delete failed: " + err.Error()
		return
	}
	gone := map[string]bool{}
	for _, id := range deleted {
		gone[id] = true
		if m.summState != nil {
			delete(m.summState.inFlight, id)
			delete(m.summState.origDescs, id)
		}
		if m.cache != nil {
			m.cache.Delete(id)
		}
	}
	if m.summState != nil {
		var remainingQueue []string
		for _, id := range m.summState.queue {
			if !gone[id] {
				remainingQueue = append(remainingQueue, id)
			}
		}
		m.summState.queue = remainingQueue
	}
	kept := m.sessions[:0]
	for _, s := range m.sessions {
		if !gone[s.ID] {
			kept = append(kept, s)
		}
	}
	m.sessions = kept
	m.marked = map[string]bool{}
	if m.cache != nil {
		_ = m.cache.Save()
	}
	if m.cursor >= len(m.filtered()) {
		m.cursor = len(m.filtered()) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.status = "deleted " + itoa(len(deleted)) + " session(s)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

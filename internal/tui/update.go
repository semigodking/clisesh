package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update implements tea.Model. It is pure and testable without a terminal.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case summarizeChunkDoneMsg:
		cmd := m.applyResummarize(msg)
		return m, cmd
	case tea.BatchMsg:
		var cmds []tea.Cmd
		for _, c := range msg {
			if c != nil {
				mResult, cmdResult := m.Update(c())
				if newM, ok := mResult.(Model); ok {
					m = newM
				}
				if cmdResult != nil {
					cmds = append(cmds, cmdResult)
				}
			}
		}
		if len(cmds) == 1 {
			return m, cmds[0]
		}
		return m, tea.Batch(cmds...)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	case modeInfo:
		return m.handleInfoKey(msg)
	case modeError:
		return m.handleErrorKey(msg)
	case modeEditDesc:
		return m.handleEditDescKey(msg)
	default:
		return m.handleNormalKey(msg)
	}
}

func (m Model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup", "ctrl+u":
		m.moveCursor(-m.listHeight())
	case "pgdown", "ctrl+d":
		m.moveCursor(m.listHeight())
	case "home", "g":
		m.cursor = 0
		m.offset = 0
	case "end", "G":
		m.cursor = len(m.filtered()) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	case " ":
		if s, ok := m.current(); ok {
			if m.marked[s.ID] {
				delete(m.marked, s.ID)
			} else {
				m.marked[s.ID] = true
			}
			m.moveCursor(1)
		}
	case "enter":
		if m.caps.CanResume {
			if s, ok := m.current(); ok {
				if err := m.provider.ValidateResume(m.ctx, s.ID); err != nil {
					m.lastErr = err.Error()
					m.errOffset = 0
					m.status = "resume failed: CLI not found"
					m.mode = modeError
					return m, nil
				}
				if m.cancel != nil {
					m.cancel()
				}
				m.resumeID = s.ID
				m.quitting = true
				return m, tea.Quit
			}
		}
	case "/":
		m.mode = modeFilter
	case "i":
		if _, ok := m.current(); ok {
			m.infoOffset = 0
			m.prevMode = modeNormal
			m.mode = modeInfo
		}
	case "e":
		if m.lastErr != "" {
			m.errOffset = 0
			m.mode = modeError
		}
	case "m":
		if s, ok := m.current(); ok {
			cmd := m.openEditDesc(s)
			return m, cmd
		}
	case "r":
		if m.caps.ProvidesPrompts {
			if ids := m.actionTargets(); len(ids) > 0 {
				cmd := m.startResummarize(ids)
				m.marked = map[string]bool{}
				return m, cmd
			}
		}
	case "d":
		if m.caps.CanDelete {
			if len(m.deleteTargets()) > 0 {
				m.mode = modeConfirmDelete
			} else if m.hasActiveTarget() {
				m.status = "cannot delete active session(s)"
			}
		}
	}
	return m, nil
}

func (m Model) handleEditDescKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelEditDesc()
		return m, nil
	case "enter":
		m.saveEditedDesc()
		return m, nil
	default:
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd
	}
}

func (m Model) handleInfoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "down", "j":
		m.infoOffset++
	case "up", "k":
		if m.infoOffset > 0 {
			m.infoOffset--
		}
	case "pgdown", "ctrl+d":
		m.infoOffset += 8
	case "pgup", "ctrl+u":
		m.infoOffset -= 8
		if m.infoOffset < 0 {
			m.infoOffset = 0
		}
	case "home", "g":
		m.infoOffset = 0
	case "end", "G":
		m.infoOffset = 100 // clamped in render
	default:
		// Any other key (esc, q, enter, i, etc.) closes the info popup.
		m.infoOffset = 0
		if m.prevMode == modeFilter {
			m.mode = modeFilter
		} else {
			m.mode = modeNormal
		}
		m.prevMode = modeNormal
	}
	return m, nil
}

func (m Model) handleErrorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "down", "j":
		m.errOffset++
	case "up", "k":
		if m.errOffset > 0 {
			m.errOffset--
		}
	case "pgdown", "ctrl+d":
		m.errOffset += 10
	case "pgup", "ctrl+u":
		m.errOffset -= 10
		if m.errOffset < 0 {
			m.errOffset = 0
		}
	case "home", "g":
		m.errOffset = 0
	case "end", "G":
		m.errOffset = 1000 // clamped in render
	case "esc", "q", "enter", "e":
		m.errOffset = 0
		m.mode = modeNormal
	}
	return m, nil
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.filter = ""
		m.mode = modeNormal
		m.cursor = 0
		m.offset = 0
	case "enter":
		m.mode = modeNormal
	case "up":
		m.moveCursor(-1)
	case "down":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.listHeight())
	case "pgdown":
		m.moveCursor(m.listHeight())
	case "tab", "ctrl+i", "alt+i":
		if _, ok := m.current(); ok {
			m.infoOffset = 0
			m.prevMode = modeFilter
			m.mode = modeInfo
		}
	case "backspace":
		if len(m.filter) > 0 {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
			m.cursor = 0
			m.offset = 0
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.cursor = 0
			m.offset = 0
		}
	}
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		ids := m.deleteTargets()
		m.doDelete(ids)
	}
	// Any key (including y after delete) returns to normal mode.
	m.mode = modeNormal
	return m, nil
}

// moveCursor moves the cursor by delta, clamped to the filtered list, and
// keeps the offset so the cursor stays visible.
func (m *Model) moveCursor(delta int) {
	n := len(m.filtered())
	if n == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	lh := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// listHeight is the number of session rows visible (excludes header, status,
// footer). Falls back to a sane value before the first WindowSizeMsg.
func (m Model) listHeight() int {
	h := m.height - 3 // header + status + footer
	if h < 1 {
		return 10
	}
	return h
}

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/textutil"
	"github.com/semigodking/clisesh/internal/theme"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	base := m.viewFrame()
	if m.mode == modeInfo {
		return m.overlayInfo(base)
	}
	if m.mode == modeError {
		return m.overlayError(base)
	}
	if m.mode == modeEditDesc {
		return m.overlayEditDesc(base)
	}
	return base
}

// viewFrame renders the normal full-screen frame (header, list, status, footer).
func (m Model) viewFrame() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	var b strings.Builder
	b.WriteString(m.viewHeader(w))
	b.WriteByte('\n')
	b.WriteString(m.viewList(w))
	b.WriteString(m.viewStatus(w))
	b.WriteByte('\n')
	b.WriteString(m.viewFooter(w))
	return b.String()
}

func (m Model) viewHeader(w int) string {
	title := fmt.Sprintf(" clisesh — %s ", m.provider.ID())
	line := center(title, w)
	return m.theme.Style(theme.Header).Render(textutil.PadToWidth(line, w))
}

func (m Model) viewList(w int) string {
	f := m.filtered()
	lh := m.listHeight()
	var b strings.Builder
	drawn := 0
	for i := m.offset; i < len(f) && drawn < lh; i++ {
		b.WriteString(m.viewRow(f[i], i == m.cursor, w))
		b.WriteByte('\n')
		drawn++
	}
	// Pad remaining rows so the layout is stable.
	for ; drawn < lh; drawn++ {
		b.WriteString(strings.Repeat(" ", 0))
		b.WriteByte('\n')
	}
	return b.String()
}

// viewRow renders a single session row with uniform column positions.
func (m Model) viewRow(s domain.Session, isCursor bool, w int) string {
	mark := " "
	if m.marked[s.ID] {
		mark = "✓"
	}
	lock := " "
	if s.Locked {
		lock = "●"
	}
	badge := ProviderBadge(s.ProviderID)
	age := leftPad(textutil.Truncate(ageOf(s), 9), 9)
	// Uniform prefix: " M L BBB  AGE  [SHORTID]  " (BBB = 3-character provider badge)
	prefix := fmt.Sprintf(" %s %s %s  %s  [%s]  ", mark, lock, badge, age, s.ShortID())
	descWidth := w - textutil.DisplayWidth(prefix)
	if descWidth < 0 {
		descWidth = 0
	}
	desc := textutil.Truncate(s.Description, descWidth)
	line := textutil.PadToWidth(prefix+desc, w)

	switch {
	case isCursor:
		role := theme.Cursor
		return m.colorBadgesInLine(line, s, &role)
	case m.marked[s.ID]:
		role := theme.Marked
		return m.colorBadgesInLine(line, s, &role)
	default:
		return m.colorBadgesInLine(line, s, nil)
	}
}

// colorBadgesInLine renders the session row, coloring the active lock glyph (green)
// and provider badge (signature brand color). When role is non-nil (Cursor or Marked),
// the lock and badge inherit the row's background color so the highlight bar remains solid.
// Layout in prefix:
// index 0: ' '
// index 1: mark (' ' or '✓')
// index 2: ' '
// index 3: lock (' ' or '●')
// index 4: ' '
// index 5..7: provider badge (3 characters)
// index 8..: rest of row
func (m Model) colorBadgesInLine(line string, s domain.Session, role *theme.Role) string {
	runes := []rune(line)
	if len(runes) < 9 {
		if role != nil {
			return m.theme.Style(*role).Render(line)
		}
		return line
	}

	if role == nil {
		var lockPart string
		if s.Locked && runes[3] == '●' {
			lockPart = m.theme.Style(theme.Lock).Render("●")
		} else {
			lockPart = string(runes[3])
		}

		badgeText := string(runes[5:8])
		badgePart := m.theme.ProviderColor(s.ProviderID).Render(badgeText)

		return string(runes[:3]) + lockPart + " " + badgePart + string(runes[8:])
	}

	rowBg := m.theme.BgColor(*role)
	rowStyle := m.theme.Style(*role)
	lockStyle := m.theme.LockStyleWithBg(rowBg)
	badgeStyle := m.theme.ProviderColorWithBg(s.ProviderID, rowBg)

	part0 := rowStyle.Render(string(runes[:3]))
	var partLock string
	if s.Locked && runes[3] == '●' {
		partLock = lockStyle.Render("●")
	} else {
		partLock = rowStyle.Render(string(runes[3]))
	}
	partSpace := rowStyle.Render(" ")
	badgeText := string(runes[5:8])
	partBadge := badgeStyle.Render(badgeText)
	partRest := rowStyle.Render(string(runes[8:]))

	return part0 + partLock + partSpace + partBadge + partRest
}

// statusText returns the text to display in the status bar.
// summState.status (written via the shared pointer) takes priority over
// m.status so that progress set inside Init()'s value-receiver call—which
// cannot modify m.status directly—is still visible in the outer model's View.
func (m Model) statusText() string {
	if m.summState != nil && m.summState.status != "" {
		return m.summState.status
	}
	return m.status
}

func tailByDisplayWidth(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(s, "")
	if textutil.DisplayWidth(s) <= maxCols {
		return s
	}
	runes := []rune(s)
	used := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		cw := textutil.CharWidth(runes[i])
		if used+cw > maxCols {
			break
		}
		used += cw
		start = i
	}
	return string(runes[start:])
}

func (m Model) viewStatus(w int) string {
	f := m.filtered()
	total := len(f)
	pos := 0
	if total > 0 {
		pos = m.cursor + 1
	}
	rightText := fmt.Sprintf(" %d/%d ", pos, total)

	isError := m.mode != modeFilter && m.lastErr != "" && (strings.HasPrefix(m.status, "summarize failed:") || strings.HasPrefix(m.status, "resume failed:"))

	statusBg := m.theme.BgColor(theme.Status)
	statusStyle := m.theme.Style(theme.Status)
	if isError {
		warnStyle := m.theme.Style(theme.Warning)
		warnBg := m.theme.BgColor(theme.Warning)
		if warnBg != "" {
			statusBg = warnBg
			statusStyle = warnStyle
		} else {
			statusStyle = warnStyle
			if statusBg != "" {
				statusStyle = statusStyle.Background(lipgloss.Color(statusBg))
			}
		}
	}

	right := statusStyle.Render(rightText)
	rightW := lipgloss.Width(right)

	var renderedLeft string
	st := m.statusText()
	switch {
	case m.mode == modeFilter:
		filterPrompt := " Filter: "
		cursorGlyph := "▌"
		maxFilterLen := w - rightW - textutil.DisplayWidth(filterPrompt) - textutil.DisplayWidth(cursorGlyph)
		dispFilter := m.filter
		if maxFilterLen > 0 && textutil.DisplayWidth(dispFilter) > maxFilterLen {
			dispFilter = tailByDisplayWidth(dispFilter, maxFilterLen)
		} else if maxFilterLen <= 0 {
			dispFilter = ""
		}
		renderedLeft = statusStyle.Render(filterPrompt + dispFilter + cursorGlyph)
	case st != "":
		cleanStatus := textutil.SingleLine(st)
		if isError {
			hintKey := m.formatKeyBadge("E", statusBg)
			hintKeyW := lipgloss.Width(hintKey)
			hintStaticW := textutil.DisplayWidth(" [: view error]")
			hintTotalW := hintKeyW + hintStaticW

			maxStatusLen := w - rightW - hintTotalW - 1
			if maxStatusLen < 10 {
				maxStatusLen = 10
			}
			truncated := textutil.Truncate(cleanStatus, maxStatusLen)
			renderedLeft = statusStyle.Render(" "+truncated+" [") + hintKey + statusStyle.Render(": view error]")
		} else {
			maxStatusLen := w - rightW - 1
			if maxStatusLen < 10 {
				maxStatusLen = 10
			}
			renderedLeft = statusStyle.Render(" " + textutil.Truncate(cleanStatus, maxStatusLen))
		}
	case len(m.marked) > 0:
		renderedLeft = statusStyle.Render(fmt.Sprintf(" %d marked", len(m.marked)))
	default:
		if s, ok := m.current(); ok {
			badge := ProviderBadge(s.ProviderID)
			maxStatusLen := w - rightW - 1
			if maxStatusLen < 10 {
				maxStatusLen = 10
			}
			plain := fmt.Sprintf(" [%s] %s  %s", badge, s.ID, s.CWD)
			renderedLeft = statusStyle.Render(textutil.Truncate(plain, maxStatusLen))
		} else {
			renderedLeft = statusStyle.Render(fmt.Sprintf(" %d session(s)", total))
		}
	}

	leftW := lipgloss.Width(renderedLeft)
	gap := w - leftW - rightW
	if gap < 0 {
		gap = 0
	}
	line := renderedLeft + statusStyle.Render(strings.Repeat(" ", gap)) + right
	currentWidth := lipgloss.Width(line)
	if currentWidth < w {
		line += statusStyle.Render(strings.Repeat(" ", w-currentWidth))
	}
	return line
}

func (m Model) formatKeyBadge(key string, parentBg string) string {
	return m.theme.KeyStyleWithBg(parentBg).Render(key)
}

func (m Model) formatKeyHint(key, label string, parentBg string, parentStyle lipgloss.Style) string {
	return m.formatKeyBadge(key, parentBg) + parentStyle.Render(": "+label)
}

func (m Model) viewFooter(w int) string {
	footerStyle := m.theme.Style(theme.Footer)
	footerBg := m.theme.BgColor(theme.Footer)

	if m.mode == modeConfirmDelete {
		warnStyle := m.theme.Style(theme.Warning)
		warnBg := m.theme.BgColor(theme.Warning)
		n := len(m.deleteTargets())
		targetStr := fmt.Sprintf(" Delete %d session(s) ", n)
		if m.hasActiveTarget() {
			targetStr = fmt.Sprintf(" Delete %d session(s) (active skipped)? ", n)
		} else {
			targetStr = fmt.Sprintf(" Delete %d session(s)? ", n)
		}
		part1 := warnStyle.Render(targetStr)
		part2 := m.formatKeyBadge("Y", warnBg)
		part3 := warnStyle.Render(" to confirm, any other key to cancel ")
		rendered := part1 + part2 + part3
		currentWidth := lipgloss.Width(rendered)
		if currentWidth < w {
			rendered += warnStyle.Render(strings.Repeat(" ", w-currentWidth))
		}
		return rendered
	}

	if m.mode == modeFilter {
		part1 := footerStyle.Render(" ")
		part2 := m.formatKeyBadge("ESC", footerBg)
		part3 := footerStyle.Render(": clear  ")
		part4 := m.formatKeyBadge("Enter", footerBg)
		part5 := footerStyle.Render(": apply  ")
		part6 := m.formatKeyBadge("↑/↓", footerBg)
		part7 := footerStyle.Render(": nav  ")
		part8 := m.formatKeyBadge("Tab", footerBg)
		part9 := footerStyle.Render(": info  (type to filter) ")
		rendered := part1 + part2 + part3 + part4 + part5 + part6 + part7 + part8 + part9
		currentWidth := lipgloss.Width(rendered)
		if currentWidth < w {
			rendered += footerStyle.Render(strings.Repeat(" ", w-currentWidth))
		}
		return rendered
	}

	if m.mode == modeEditDesc {
		part1 := footerStyle.Render(" ")
		part2 := m.formatKeyBadge("Enter", footerBg)
		part3 := footerStyle.Render(": save   ")
		part4 := m.formatKeyBadge("ESC", footerBg)
		part5 := footerStyle.Render(": cancel ")
		rendered := part1 + part2 + part3 + part4 + part5
		currentWidth := lipgloss.Width(rendered)
		if currentWidth < w {
			rendered += footerStyle.Render(strings.Repeat(" ", w-currentWidth))
		}
		return rendered
	}

	type keyItem struct {
		key, label string
	}
	var items []keyItem
	if m.caps.CanResume {
		items = append(items, keyItem{"Enter", "resume"})
	}
	items = append(items,
		keyItem{"Space", "mark"},
		keyItem{"/", "filter"},
		keyItem{"I", "info"},
		keyItem{"M", "edit"},
	)
	if m.caps.ProvidesPrompts {
		items = append(items, keyItem{"R", "re-summarize"})
	}
	if m.caps.CanDelete {
		items = append(items, keyItem{"D", "delete"})
	}
	items = append(items, keyItem{"Q", "quit"})

	var formattedItems []string
	for _, it := range items {
		formattedItems = append(formattedItems, m.formatKeyHint(it.key, it.label, footerBg, footerStyle))
	}
	hint := footerStyle.Render(" ") + strings.Join(formattedItems, footerStyle.Render("  ")) + footerStyle.Render(" ")
	currentWidth := lipgloss.Width(hint)
	if currentWidth < w {
		hint += footerStyle.Render(strings.Repeat(" ", w-currentWidth))
	}
	return hint
}

// infoBox renders the info popup as a bordered dialog box (no full-screen
// clear); overlayInfo composites it over the list. Mirrors the Python layout.
func (m Model) infoBox() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	s, ok := m.current()
	if !ok {
		return ""
	}

	// Dialog box is centered and sized relative to the screen.
	boxWidth := w - 8
	if boxWidth > 76 {
		boxWidth = 76
	}
	if boxWidth < 20 {
		boxWidth = 20
	}
	// Inner content width accounts for border (2) + padding (2 each side).
	contentWidth := boxWidth - 6
	if contentWidth < 10 {
		contentWidth = 10
	}

	var b strings.Builder
	b.WriteString(m.theme.Style(theme.Title).Render("Session Information"))
	b.WriteString("\n\n")
	writeField(&b, "ID:", s.ID)
	writeField(&b, "Age:", ageOf(s))
	if s.ProviderID != "" {
		writeField(&b, "Provider:", s.ProviderID)
	}
	if s.CWD != "" {
		writeField(&b, "Directory:", s.CWD)
	}
	status := "inactive"
	if s.Locked {
		status = m.theme.Style(theme.Lock).Render("active (locked)")
	}
	writeField(&b, "Status:", status)
	b.WriteString("\nDescription:\n")

	wrappedDesc := textutil.WrapByWidth(s.Description, contentWidth)
	maxDescLines := 8
	if m.height > 24 {
		maxDescLines = m.height - 16
	}
	if maxDescLines < 3 {
		maxDescLines = 3
	}

	// Clamp infoOffset
	offset := m.infoOffset
	if offset > len(wrappedDesc)-maxDescLines {
		offset = len(wrappedDesc) - maxDescLines
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + maxDescLines
	if end > len(wrappedDesc) {
		end = len(wrappedDesc)
	}

	for i := offset; i < end; i++ {
		b.WriteString(wrappedDesc[i] + "\n")
	}

	footerStyle := m.theme.Style(theme.Footer)
	footerBg := m.theme.BgColor(theme.Footer)
	if len(wrappedDesc) > maxDescLines {
		hint := footerStyle.Render(fmt.Sprintf(" [%d-%d/%d]  ", offset+1, end, len(wrappedDesc))) +
			m.formatKeyBadge("↑/↓", footerBg) + footerStyle.Render(": scroll  ") +
			m.formatKeyBadge("ESC", footerBg) + footerStyle.Render(": close ")
		b.WriteString("\n" + hint)
	} else {
		b.WriteString("\n" + footerStyle.Render(" press any key to close "))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 2).
		Width(contentWidth + 2).
		Render(b.String())
}

// errorBox renders the scrollable error details dialog box.
func (m Model) errorBox() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	boxWidth := w - 6
	if boxWidth > 90 {
		boxWidth = 90
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	contentWidth := boxWidth - 6
	if contentWidth < 20 {
		contentWidth = 20
	}

	viewHeight := h - 8
	if viewHeight > 25 {
		viewHeight = 25
	}
	if viewHeight < 5 {
		viewHeight = 5
	}

	var b strings.Builder
	titleText := "⚠ Operation Failure Details"
	if strings.HasPrefix(m.status, "resume failed:") {
		titleText = "⚠ Cannot Resume Session: CLI Not Found"
	} else if strings.HasPrefix(m.status, "summarize failed:") {
		titleText = "⚠ Summarization Failure Details"
	}
	title := m.theme.Style(theme.Warning).Render(titleText)
	b.WriteString(title)
	b.WriteString("\n\n")

	rawErr := m.lastErr
	if rawErr == "" {
		rawErr = "(no error details available)"
	}

	var allLines []string
	for _, rawLine := range strings.Split(rawErr, "\n") {
		wrapped := textutil.WrapByWidth(rawLine, contentWidth)
		if len(wrapped) == 0 {
			allLines = append(allLines, "")
		} else {
			allLines = append(allLines, wrapped...)
		}
	}

	total := len(allLines)
	offset := m.errOffset
	if offset > total-viewHeight {
		offset = total - viewHeight
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + viewHeight
	if end > total {
		end = total
	}

	for i := offset; i < end; i++ {
		b.WriteString(allLines[i] + "\n")
	}

	// Pad blank lines if fewer than viewHeight to keep box stable
	for i := end - offset; i < viewHeight; i++ {
		b.WriteByte('\n')
	}

	footerStyle := m.theme.Style(theme.Footer)
	footerBg := m.theme.BgColor(theme.Footer)
	var hint string
	if total > viewHeight {
		hint = footerStyle.Render(fmt.Sprintf(" [Lines %d-%d of %d]  ", offset+1, end, total)) +
			m.formatKeyBadge("↑/↓/PgUp/PgDn", footerBg) + footerStyle.Render(": scroll  ") +
			m.formatKeyBadge("ESC", footerBg) + footerStyle.Render("/") +
			m.formatKeyBadge("Q", footerBg) + footerStyle.Render("/") +
			m.formatKeyBadge("E", footerBg) + footerStyle.Render(": close ")
	} else {
		hint = footerStyle.Render(" press ") +
			m.formatKeyBadge("ESC", footerBg) + footerStyle.Render(", ") +
			m.formatKeyBadge("Q", footerBg) + footerStyle.Render(", or ") +
			m.formatKeyBadge("E", footerBg) + footerStyle.Render(" to close ")
	}
	b.WriteString("\n" + hint)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("1")). // Red border for error
		Padding(0, 2).
		Width(contentWidth + 2).
		Render(b.String())
}

// overlayError composites the error dialog centered over base frame.
func (m Model) overlayError(base string) string {
	box := m.errorBox()
	if box == "" {
		return base
	}
	return compositeBox(base, box, m.width)
}

// overlayInfo composites the info box centered over the base frame, leaving
// the surrounding list visible (no full-screen clear).
func (m Model) overlayInfo(base string) string {
	box := m.infoBox()
	if box == "" {
		return base
	}
	return compositeBox(base, box, m.width)
}

// editDescBox renders the modal dialog box for manual description editing.
func (m Model) editDescBox() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	boxWidth := w - 8
	if boxWidth > 76 {
		boxWidth = 76
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	contentWidth := boxWidth - 6
	if contentWidth < 20 {
		contentWidth = 20
	}

	// Update textinput width to fit cleanly inside box
	m.editInput.Width = contentWidth - textutil.DisplayWidth(m.editInput.Prompt) - 2
	if m.editInput.Width < 10 {
		m.editInput.Width = 10
	}

	var b strings.Builder
	b.WriteString(m.theme.Style(theme.Title).Render("Edit Session Description"))
	b.WriteString("\n\n")

	for _, s := range m.sessions {
		if s.ID == m.editSessionID {
			badge := ProviderBadge(s.ProviderID)
			writeField(&b, "Session:", fmt.Sprintf("[%s] %s", badge, s.ID))
			break
		}
	}
	b.WriteString("\n")
	b.WriteString(m.editInput.View())
	b.WriteString("\n\n")
	footerStyle := m.theme.Style(theme.Footer)
	footerBg := m.theme.BgColor(theme.Footer)
	editHint := footerStyle.Render(" ") +
		m.formatKeyBadge("Enter", footerBg) + footerStyle.Render(": save   ") +
		m.formatKeyBadge("ESC", footerBg) + footerStyle.Render(": cancel ")
	b.WriteString(editHint)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 2).
		Width(contentWidth + 2).
		Render(b.String())
}

// overlayEditDesc composites the description edit box centered over the base frame.
func (m Model) overlayEditDesc(base string) string {
	box := m.editDescBox()
	if box == "" {
		return base
	}
	return compositeBox(base, box, m.width)
}

func compositeBox(base, box string, width int) string {
	baseLines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")

	w := width
	if w <= 0 {
		w = 80
	}
	boxW := lipgloss.Width(box)
	startCol := (w - boxW) / 2
	if startCol < 0 {
		startCol = 0
	}
	startRow := (len(baseLines) - len(boxLines)) / 2
	if startRow < 1 {
		startRow = 1 // keep header visible
	}

	for i, bl := range boxLines {
		row := startRow + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = overlayLine(baseLines[row], bl, startCol, w)
	}
	return strings.Join(baseLines, "\n")
}

// overlayLine places overlay onto base starting at display column startCol,
// preserving base content on either side. Operates on display columns and is
// ANSI-aware only for the overlay (base cells under the overlay are replaced).
func overlayLine(base, overlay string, startCol, width int) string {
	basePlain := []rune(stripANSIView(base))
	// Pad base to full width so slicing is safe.
	for textutil.DisplayWidth(string(basePlain)) < width {
		basePlain = append(basePlain, ' ')
	}
	overlayW := lipgloss.Width(overlay)
	left := runesUpToCol(basePlain, startCol)
	rightStart := startCol + overlayW
	right := runesFromCol(basePlain, rightStart)
	return left + overlay + right
}

// borderColor is the dialog border color (cyan, matching the cwd accent).
const borderColor = "6"

// runesUpToCol returns the prefix of runes covering the first `col` display cols.
func runesUpToCol(runes []rune, col int) string {
	used := 0
	var out []rune
	for _, r := range runes {
		cw := textutil.CharWidth(r)
		if used+cw > col {
			break
		}
		out = append(out, r)
		used += cw
	}
	// pad to exactly col
	for used < col {
		out = append(out, ' ')
		used++
	}
	return string(out)
}

// runesFromCol returns the suffix of runes starting at display column `col`.
func runesFromCol(runes []rune, col int) string {
	used := 0
	var out []rune
	started := false
	for _, r := range runes {
		cw := textutil.CharWidth(r)
		if used >= col {
			started = true
		}
		if started {
			out = append(out, r)
		}
		used += cw
	}
	return string(out)
}

// stripANSIView removes ANSI escape sequences (view-local helper).
func stripANSIView(s string) string {
	var b []rune
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b = append(b, r)
	}
	return string(b)
}

func writeField(b *strings.Builder, label, value string) {
	b.WriteString(fmt.Sprintf("%-11s %s\n", label, value))
}

// center centers s within width w using spaces (display-width aware).
func center(s string, w int) string {
	sw := textutil.DisplayWidth(s)
	if sw >= w {
		return textutil.Truncate(s, w)
	}
	left := (w - sw) / 2
	return strings.Repeat(" ", left) + s
}

// leftPad right-justifies s to n display columns.
func leftPad(s string, n int) string {
	pad := n - textutil.DisplayWidth(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

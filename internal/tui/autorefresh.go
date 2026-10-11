package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Auto-refresh is toggled per tab with "a". Only the visible screen
// refreshes, and only the data it shows; open detail views follow along.

// autoInterval is how often a tab refreshes when auto-refresh is on.
const autoInterval = 30 * time.Minute

// autoTickEvery is how often due refreshes are checked.
const autoTickEvery = 5 * time.Second

type autoTickMsg struct{}

func autoTick() tea.Cmd {
	return tea.Tick(autoTickEvery, func(time.Time) tea.Msg { return autoTickMsg{} })
}

// onAutoTick reloads the visible data sets that are older than the
// interval, if the visible tab's auto-refresh is on.
func (m *Model) onAutoTick() tea.Cmd {
	if !m.auto[m.tab] {
		return nil
	}
	now := m.now()
	var cmds []tea.Cmd
	for _, ds := range m.visibleData() {
		for _, i := range m.inScope() {
			if st := m.state(ds, i); st.loaded && !st.loading && now.Sub(st.fetchedAt) >= autoInterval {
				cmds = append(cmds, m.loadSet(ds, i, true))
			}
		}
	}
	return tea.Batch(cmds...)
}

// toggleAuto switches auto-refresh of the visible tab; switching it on
// refreshes right away.
func (m *Model) toggleAuto() tea.Cmd {
	m.auto[m.tab] = !m.auto[m.tab]
	if m.auto[m.tab] {
		return m.refreshVisible()
	}
	return nil
}

// autoLabel describes the visible tab's auto-refresh state.
func (m Model) autoLabel() string {
	if m.auto[m.tab] {
		return "a auto " + fmtDuration(autoInterval)
	}
	return "a auto off"
}

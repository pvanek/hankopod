// Package tui is the Bubble Tea terminal front end over internal/core.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/pvanek/hankopod/internal/core"
)

// Fetcher is the subset of core.Service the UI needs (fakeable in tests).
type Fetcher interface {
	Projects() []core.Project
	Health(ctx context.Context, i int, now time.Time) core.Health
	Users(ctx context.Context, i int, now time.Time) core.UserList
	Events(ctx context.Context, i int, now time.Time) core.EventList
	Sessions(ctx context.Context, i int, now time.Time) core.SessionStats
	Stats(ctx context.Context, i int, now time.Time, g core.Granularity) core.Stats
}

type tab int

const (
	tabOverview tab = iota
	tabAnalytics
	tabSessions
	tabActivity
	tabCount
)

var tabNames = [tabCount]string{"Overview", "Analytics", "Sessions", "Activity"}

type Model struct {
	svc      Fetcher
	now      func() time.Time
	projects []core.Project

	width, height int
	tab           tab
	scope         int // 0 = all projects, i = projects[i-1]

	// Per-project data sets, loaded lazily (see load.go). The slices are
	// shared between model copies; Bubble Tea calls Update from one
	// goroutine, so writing through them is safe.
	health   []resource[core.Health]
	users    []resource[core.UserList]
	events   []resource[core.EventList]
	sessions []resource[core.SessionStats]
	stats    [core.GranularityCount][]resource[core.Stats]

	gran core.Granularity // periods of the Analytics tab

	auto [tabCount]bool // auto-refresh per tab

	table   table.Model
	items   []any // item behind each table row
	detail  *detailView
	help    *helpView // over everything else when open
	spinner spinner.Model
}

func New(svc Fetcher) Model {
	t := table.New(table.WithFocused(true))
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true)
	s.Selected = s.Selected.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("57"))
	t.SetStyles(s)

	projects := svc.Projects()
	m := Model{
		svc:      svc,
		now:      time.Now,
		projects: projects,
		health:   make([]resource[core.Health], len(projects)),
		users:    make([]resource[core.UserList], len(projects)),
		events:   make([]resource[core.EventList], len(projects)),
		sessions: make([]resource[core.SessionStats], len(projects)),
		gran:     core.Monthly,
		table:    t,
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	for g := range m.stats {
		m.stats[g] = make([]resource[core.Stats], len(projects))
	}
	m.auto[tabOverview] = true
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.ensureVisible(), autoTick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var follow tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		if m.help != nil {
			return m, m.handleHelpKey(msg)
		}
		// After any key, load what the (possibly new) screen shows.
		if m.detail != nil {
			cmd := m.handleDetailKey(msg)
			return m, tea.Batch(cmd, m.ensureVisible())
		}
		if cmd, handled := m.handleKey(msg.String()); handled {
			m.rebuildTable()
			return m, tea.Batch(cmd, m.ensureVisible())
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case loadedMsg[core.Health]:
		m.health[msg.i] = resource[core.Health]{loaded: true, data: msg.data}
	case loadedMsg[core.UserList]:
		m.users[msg.i] = resource[core.UserList]{loaded: true, data: msg.data}
	case loadedMsg[core.EventList]:
		m.events[msg.i] = resource[core.EventList]{loaded: true, data: msg.data}
	case loadedMsg[core.SessionStats]:
		m.sessions[msg.i] = resource[core.SessionStats]{loaded: true, data: msg.data}
	case loadedMsg[core.Stats]:
		m.stats[msg.data.Granularity][msg.i] = resource[core.Stats]{loaded: true, data: msg.data}
	case autoTickMsg:
		follow = tea.Batch(m.onAutoTick(), autoTick())
	default:
		return m, nil
	}
	m.rebuildTable()
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, tea.Batch(cmd, follow)
}

func (m *Model) handleKey(k string) (tea.Cmd, bool) {
	switch k {
	case "q", "ctrl+c":
		return tea.Quit, true
	case "tab", "right", "l":
		m.setTab((m.tab + 1) % tabCount)
	case "shift+tab", "left", "h":
		m.setTab((m.tab + tabCount - 1) % tabCount)
	case "0", "1", "2", "3":
		m.setTab(tab(k[0] - '0'))
	case "p":
		m.setScope((m.scope + 1) % (len(m.projects) + 1))
	case "P":
		n := len(m.projects) + 1
		m.setScope((m.scope + n - 1) % n)
	case "enter":
		m.openDetail()
	case "r":
		return m.refreshVisible(), true
	case "a":
		return m.toggleAuto(), true
	case "f1", "?":
		m.openHelp()
	case "d", "w", "m", "y":
		if m.tab != tabAnalytics {
			return nil, false
		}
		m.gran = core.Granularity(strings.Index("dwmy", k))
		m.table.SetCursor(0)
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) setTab(t tab) {
	m.tab = t
	m.table.SetCursor(0)
}

func (m *Model) setScope(s int) {
	m.scope = s
	m.table.SetCursor(0)
}

// inScope returns the indexes of the projects in the current scope.
func (m Model) inScope() []int {
	if m.scope > 0 && m.scope <= len(m.projects) {
		return []int{m.scope - 1}
	}
	out := make([]int, len(m.projects))
	for i := range out {
		out[i] = i
	}
	return out
}

func (m Model) scopeName() string {
	if m.scope > 0 && m.scope <= len(m.projects) {
		return m.projects[m.scope-1].Name
	}
	return fmt.Sprintf("All projects (%d)", len(m.projects))
}

// currentState reports loading and errors of the visible data.
func (m Model) currentState() (loading bool, err error) {
	var errs []error
	for _, st := range m.visibleStates() {
		loading = loading || st.loading
		errs = append(errs, st.errs...)
	}
	return loading, errors.Join(errs...)
}

var (
	activeTab   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("57")).Padding(0, 1)
	inactiveTab = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	dim         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	warn        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	good        = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	scopeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	tileStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	tileValue   = lipgloss.NewStyle().Bold(true)
)

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "hankopod"
	return v
}

func (m Model) render() string {
	if m.help != nil {
		return m.renderHelp()
	}
	if m.detail != nil {
		return m.renderDetail()
	}
	return m.header() + "\n" + m.table.View() + "\n" +
		m.statusBar("F1 help · ←/→ tab · p/P project · ↑/↓ move · enter detail · r refresh")
}

// statusBar renders the bottom line: the given key hints, the auto-refresh
// state and last update, and "q quit" at the right edge. Hints are cut
// first when the screen is narrow.
func (m Model) statusBar(hints string) string {
	const right = "q quit"
	width := max(m.width, 20)
	state := m.autoLabel()
	if t := m.lastFetch(); !t.IsZero() {
		if updated := state + " · updated " + t.Local().Format("15:04:05"); lipgloss.Width(updated)+len(right)+8 <= width {
			state = updated
		}
	}
	room := width - lipgloss.Width(state) - len(right) - 4 // " · " and a space before right
	left := state
	if room >= 4 {
		left = truncate(hints, room) + " · " + state
	}
	gap := max(width-lipgloss.Width(left)-len(right), 1)
	return dim.Render(left + strings.Repeat(" ", gap) + right)
}

// header is everything above the table: tabs, scope, summary and errors.
func (m Model) header() string {
	var b strings.Builder
	b.WriteString(m.tabsLine() + "\n" + m.scopeLine())
	switch m.tab {
	case tabOverview:
		b.WriteString("\n" + m.tiles())
	case tabAnalytics:
		b.WriteString("\n" + m.analyticsNote())
	case tabSessions:
		b.WriteString("\n" + m.sessionsNote())
	}
	if _, err := m.currentState(); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " · ")
		b.WriteString("\n" + errStyle.Width(max(m.width, 20)).Render("⚠ "+msg))
	}
	return b.String()
}

// tabsLine renders the tab bar; on narrow screens inactive tabs show only
// their number.
func (m Model) tabsLine() string {
	render := func(short bool) string {
		tabs := make([]string, tabCount)
		for i, name := range tabNames {
			style, label := inactiveTab, fmt.Sprintf("%d %s", i, name)
			if tab(i) == m.tab {
				style = activeTab
			} else if short {
				label = fmt.Sprint(i)
			}
			tabs[i] = style.Render(label)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	}
	if line := render(false); lipgloss.Width(line) <= m.width {
		return line
	}
	return render(true)
}

func (m Model) scopeLine() string {
	scope := "Project: " + scopeStyle.Render(m.scopeName())
	if loading, _ := m.currentState(); loading {
		scope += "  " + m.spinner.View() + dim.Render(" loading…")
	}
	return scope
}

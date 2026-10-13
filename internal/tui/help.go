package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The help screen lists every key. F1 (or ?) opens it from any screen.

// keyHelp is the content of the help screen: section titles (no keys) and
// key descriptions.
var keyHelp = []struct{ keys, desc string }{
	{"", "Navigation"},
	{"← → · tab shift+tab · h l", "switch tab"},
	{"0 – 3", "go to tab by number"},
	{"p / P", "next / previous project (all projects first)"},
	{"↑ ↓ · j k", "move in the table or scroll the detail"},
	{"pgup pgdown · home end", "page or jump in the table"},
	{"enter", "open the detail of the selected row"},
	{"esc · ← · h · backspace", "back from the detail"},
	{"", "Actions"},
	{"r", "reload the visible screen"},
	{"a", "toggle auto-refresh (every 30 min) of the current tab"},
	{"F1 · ?", "show or close this help"},
	{"q · ctrl+c", "quit (closes the help first)"},
}

const helpKeysWidth = 26

var helpKeyStyle = lipgloss.NewStyle().Bold(true).Width(helpKeysWidth)

func (m *Model) openHelp() {
	m.help = &helpView{vp: viewport.New()}
	m.syncHelp()
}

type helpView struct {
	vp viewport.Model
}

// syncHelp re-renders the help for the current screen size.
func (m *Model) syncHelp() {
	if m.help == nil {
		return
	}
	m.help.vp.SetWidth(max(m.width, 20))
	m.help.vp.SetHeight(max(m.height-3, 3)) // tabs, title, footer
	m.help.vp.SetContent(m.helpBody())
}

func (m Model) helpBody() string {
	descWidth := max(m.width-helpKeysWidth-1, 10)
	var b strings.Builder
	for _, k := range keyHelp {
		if k.keys == "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sectionStyle.Render(k.desc) + "\n")
			continue
		}
		desc := lipgloss.NewStyle().Width(descWidth).Render(k.desc)
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, helpKeyStyle.Render(k.keys), " ", desc) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// handleHelpKey closes the help or scrolls it.
func (m *Model) handleHelpKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "f1", "?", "q", "enter", "backspace":
		m.help = nil
		return nil
	case "ctrl+c":
		return tea.Quit
	}
	var cmd tea.Cmd
	m.help.vp, cmd = m.help.vp.Update(msg)
	return cmd
}

func (m Model) renderHelp() string {
	return strings.Join([]string{
		m.tabsLine(),
		activeTab.Render("Keys"),
		m.help.vp.View(),
		m.statusBar("esc close · ↑/↓ scroll"),
	}, "\n")
}

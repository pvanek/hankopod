package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/pvanek/hankopod/internal/core"
)

const labelWidth = 18

var (
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(labelWidth)
)

// detailView is the full-screen detail of one table row.
type detailView struct {
	item any // project index (int), core.User, core.Session or core.Event
	vp   viewport.Model
}

func (m *Model) openDetail() {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.items) {
		return
	}
	m.detail = &detailView{item: m.items[i], vp: viewport.New()}
	m.syncDetail()
}

// syncDetail re-resolves the shown item after a data refresh and re-renders
// the viewport for the current size. An item that disappeared (an expired
// session, say) keeps showing its last known state.
func (m *Model) syncDetail() {
	if m.detail == nil {
		return
	}
	if key := itemKey(m.detail.item); key != "" {
		for _, it := range m.items {
			if itemKey(it) == key {
				m.detail.item = it
				break
			}
		}
	}
	m.detail.vp.SetWidth(max(m.width, 20))
	m.detail.vp.SetHeight(max(m.height-4, 3)) // tabs, scope, title, footer
	m.detail.vp.SetContent(m.detailBody(m.detail.item))
}

// handleDetailKey handles keys while a detail is open. Keys that switch the
// tab or project close the detail and are then handled as usual.
func (m *Model) handleDetailKey(msg tea.KeyPressMsg) tea.Cmd {
	switch k := msg.String(); k {
	case "esc", "backspace", "left", "h":
		m.detail = nil
		m.rebuildTable()
		return nil
	case "q", "ctrl+c", "r", "a":
		cmd, _ := m.handleKey(k)
		return cmd
	case "tab", "shift+tab", "0", "1", "2", "3", "p", "P":
		m.detail = nil
		cmd, _ := m.handleKey(k)
		m.rebuildTable()
		return cmd
	}
	var cmd tea.Cmd
	m.detail.vp, cmd = m.detail.vp.Update(msg)
	return cmd
}

func (m Model) renderDetail() string {
	return strings.Join([]string{
		m.tabsLine(),
		m.scopeLine(),
		activeTab.Render(truncate(m.detailTitle(m.detail.item), max(m.width-2, 10))),
		m.detail.vp.View(),
		m.statusBar("esc back · ↑/↓ scroll · r refresh"),
	}, "\n")
}

// itemKey identifies an item across refreshes.
func itemKey(it any) string {
	switch v := it.(type) {
	case int:
		return fmt.Sprintf("project/%d", v)
	case core.User:
		return "user/" + v.Project + "/" + v.ID
	case core.Session:
		return "session/" + v.Project + "/" + v.ID
	case core.Event:
		return "event/" + v.Project + "/" + v.ID
	}
	return ""
}

func (m Model) detailTitle(it any) string {
	switch v := it.(type) {
	case int:
		return "Project " + m.projects[v].Name
	case core.User:
		return "User " + v.Label()
	case core.Session:
		return "Session of " + v.User
	case core.Event:
		return "Event " + v.Type
	}
	return ""
}

// sheet accumulates labelled lines, wrapping values to the screen width.
type sheet struct {
	width int
	b     strings.Builder
}

func (s *sheet) section(title string) {
	if s.b.Len() > 0 {
		s.b.WriteString("\n")
	}
	s.b.WriteString(sectionStyle.Render(title) + "\n")
}

func (s *sheet) field(label, value string) {
	if value == "" {
		value = dim.Render("—")
	}
	wrapped := lipgloss.NewStyle().Width(max(s.width-labelWidth-1, 10)).Render(value)
	for i, line := range strings.Split(wrapped, "\n") {
		l := ""
		if i == 0 {
			l = label
		}
		s.b.WriteString(labelStyle.Render(l) + " " + line + "\n")
	}
}

// list writes one line per value under label, or "none".
func (s *sheet) list(label string, values []string) {
	if len(values) == 0 {
		s.field(label, dim.Render("none"))
		return
	}
	for i, v := range values {
		if i > 0 {
			label = ""
		}
		s.field(label, v)
	}
}

func (s *sheet) errField(label string, err error) {
	if err != nil {
		s.field(label, errStyle.Render(err.Error()))
	}
}

func (m Model) detailBody(it any) string {
	s := &sheet{width: max(m.width, 30)}
	switch v := it.(type) {
	case int:
		m.projectDetail(s, v)
	case core.User:
		m.userDetail(s, v)
	case core.Session:
		m.sessionDetail(s, v)
	case core.Event:
		m.eventDetail(s, v)
	}
	return strings.TrimRight(s.b.String(), "\n")
}

// detailListLimit caps the related items listed in a detail.
const detailListLimit = 10

func (m Model) projectDetail(s *sheet, i int) {
	p, h, ul, se, ev := m.projects[i], m.health[i], m.users[i], m.sessions[i], m.events[i]
	now := m.now()

	s.section("Service")
	s.field("Admin API", p.AdminURL)
	if p.StatusURL != "" {
		s.field("Status page", p.StatusURL)
	}
	if h.loaded {
		d := h.data
		status := good.Render(d.Status.String())
		if !d.Status.Up {
			status = errStyle.Render(d.Status.String())
		}
		s.field("Status", status)
		s.field("Latency", fmtLatency(d.Status.Latency))
		s.errField("Error", d.Status.Err)
		s.field("Checked", fmtTime(d.FetchedAt))
	} else {
		s.field("Status", loadingText(h.loading))
	}

	s.section("Users")
	if h.loaded {
		mau := num(h.data.MAU)
		if h.data.MAU >= 0 && !h.data.MAUExact {
			mau = fmtCount(h.data.MAU, false)
		}
		s.field("MAU "+core.MonthStart(now).Format("Jan"), mau+dim.Render(" (since "+core.MonthStart(now).Format("Jan 2")+" UTC)"))
	}
	if ul.loaded {
		d := ul.data
		s.field("Total", num(d.Total))
		if d.Err == nil {
			for _, w := range []struct {
				label string
				d     time.Duration
			}{{"New 24h", 24 * time.Hour}, {"New 7d", 7 * 24 * time.Hour}, {"New 30d", core.RecentWindow}} {
				s.field(w.label, fmtCount(newSince(d, now, w.d)))
			}
			s.field("Last signup", fmtTimeAgo(d.LastSignup(), now))
		}
		s.errField("Error", d.Err)
		var newest []string
		for _, u := range d.Recent[:min(len(d.Recent), detailListLimit)] {
			newest = append(newest, fmt.Sprintf("%s  %s", fmtTime(u.CreatedAt), u.Label()))
		}
		s.list("Newest", newest)
	} else {
		s.field("Total", loadingText(ul.loading))
	}

	s.section("Sessions")
	switch {
	case se.loaded:
		st := se.data
		s.field("Active", fmtCount(len(st.Active), st.Complete()))
		s.field("Users with session", fmtCount(st.ActiveUsers(), st.Complete()))
		s.field("Users scanned", fmt.Sprintf("%d of %d", st.UsersScanned, st.UsersTotal))
		if st.Failed > 0 {
			s.field("Failed", warn.Render(fmt.Sprint(st.Failed)))
		}
		s.field("Scanned", fmtTime(st.FetchedAt))
		s.errField("Error", st.Err)
	default:
		s.field("Active", loadingText(se.loading))
	}

	s.section("Activity, last 24h")
	if h.loaded {
		s.field("Logins", num(h.data.Logins))
		failures := num(h.data.LoginFailures)
		if h.data.LoginFailures > 0 {
			failures = warn.Render(failures)
		}
		s.field("Failed logins", failures)
		s.errField("Error", h.data.Err)
	}
	if !ev.loaded {
		s.field("Recent failures", loadingText(ev.loading))
		return
	}
	s.errField("Error", ev.data.Err)
	var failed []string
	for _, e := range ev.data.Events {
		if e.Failed() && len(failed) < detailListLimit {
			failed = append(failed, fmt.Sprintf("%s  %s  %s", fmtTime(e.CreatedAt), e.Type, actor(e)))
		}
	}
	s.list("Recent failures", failed)
}

// loadingText stands in for data that is loading or not loaded yet.
func loadingText(loading bool) string {
	if loading {
		return dim.Render("loading…")
	}
	return dim.Render("not loaded")
}

func (m Model) userDetail(s *sheet, u core.User) {
	now := m.now()
	s.section("User")
	s.field("ID", u.ID)
	s.field("Project", u.Project)
	s.field("Username", u.Username)
	s.field("Registered", fmtTimeAgo(u.CreatedAt, now))
	s.field("Updated", fmtTime(u.UpdatedAt))

	var emails []string
	for _, e := range u.Emails {
		var tags []string
		if e.Primary {
			tags = append(tags, "primary")
		}
		if e.Verified {
			tags = append(tags, good.Render("verified"))
		} else {
			tags = append(tags, warn.Render("unverified"))
		}
		emails = append(emails, e.Address+"  "+strings.Join(tags, ", "))
	}
	s.list("Emails", emails)

	s.section("Passkeys")
	if len(u.Passkeys) == 0 {
		s.field("", dim.Render("none"))
	}
	for _, pk := range u.Passkeys {
		name := pk.Name
		if name == "" {
			name = pk.ID
		}
		if pk.MFAOnly {
			name += dim.Render(" (security key, MFA)")
		}
		last := "never used"
		if !pk.LastUsed.IsZero() {
			last = "used " + ago(pk.LastUsed, now)
		}
		s.field(name, fmt.Sprintf("created %s · %s", fmtTime(pk.CreatedAt), last))
	}

	pi := m.projectIndex(u.Project)
	s.section("Active sessions")
	// The session scan is expensive, so a user detail does not start it.
	if se := m.sessions[pi]; !se.loaded {
		hint := "not loaded (open the Sessions tab to scan)"
		if se.loading {
			hint = "loading…"
		}
		s.field("", dim.Render(hint))
	} else {
		var sessions []string
		for _, se := range m.sessionsOf(u.Project, u.ID) {
			sessions = append(sessions, fmt.Sprintf("%s  %s  %s", ago(se.LastUsed, now), se.IP, se.UserAgent))
		}
		s.list("", sessions)
	}

	s.section("Recent activity")
	if ev := m.events[pi]; !ev.loaded {
		s.field("", loadingText(ev.loading))
	} else {
		s.list("", m.eventsOf(u.Project, u.ID, u.Email))
	}
}

// projectIndex returns the index of the project named name.
func (m Model) projectIndex(name string) int {
	for i, p := range m.projects {
		if p.Name == name {
			return i
		}
	}
	return 0
}

func (m Model) sessionDetail(s *sheet, se core.Session) {
	now := m.now()
	s.section("Session")
	s.field("ID", se.ID)
	s.field("Project", se.Project)
	s.field("User", se.User)
	s.field("User ID", se.UserID)
	s.field("Created", fmtTimeAgo(se.CreatedAt, now))
	s.field("Last used", fmtTimeAgo(se.LastUsed, now))
	expires := ""
	if !se.ExpiresAt.IsZero() {
		expires = fmtTime(se.ExpiresAt) + dim.Render(" (in "+fmtAge(se.ExpiresAt.Sub(now))+")")
	}
	s.field("Expires", expires)

	s.section("Client")
	s.field("IP", se.IP)
	s.field("User agent", se.UserAgent)

	s.section("Other sessions of the user")
	var others []string
	for _, o := range m.sessionsOf(se.Project, se.UserID) {
		if o.ID != se.ID {
			others = append(others, fmt.Sprintf("%s  %s  %s", ago(o.LastUsed, now), o.IP, o.UserAgent))
		}
	}
	s.list("", others)
}

func (m Model) eventDetail(s *sheet, e core.Event) {
	s.section("Event")
	s.field("ID", e.ID)
	s.field("Project", e.Project)
	typ := e.Type
	if e.Failed() {
		typ = errStyle.Render(typ)
	}
	s.field("Type", typ)
	s.field("Time", fmtTimeAgo(e.CreatedAt, m.now()))
	if e.Error != "" {
		s.field("Error", errStyle.Render(e.Error))
	}

	s.section("Actor")
	s.field("Email", e.Email)
	s.field("User ID", e.UserID)

	s.section("Request")
	s.field("IP", e.IP)
	s.field("User agent", e.UserAgent)
	s.field("Request ID", e.RequestID)
}

// sessionsOf returns the known active sessions of a user.
func (m Model) sessionsOf(project, userID string) []core.Session {
	var out []core.Session
	for i, p := range m.projects {
		if p.Name != project {
			continue
		}
		for _, se := range m.sessions[i].data.Active {
			if se.UserID == userID {
				out = append(out, se)
			}
		}
	}
	return out
}

// eventsOf lists the fetched audit logs of a user.
func (m Model) eventsOf(project, userID, email string) []string {
	var out []string
	for i, p := range m.projects {
		if p.Name != project {
			continue
		}
		for _, e := range m.events[i].data.Events {
			if len(out) == detailListLimit {
				break
			}
			if (userID != "" && e.UserID == userID) || (email != "" && e.Email == email) {
				typ := e.Type
				if e.Failed() {
					typ = errStyle.Render(typ)
				}
				out = append(out, fmtTime(e.CreatedAt)+"  "+typ)
			}
		}
	}
	return out
}

func actor(e core.Event) string {
	if e.Email != "" {
		return e.Email
	}
	return e.UserID
}

func fmtTimeAgo(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmtTime(t) + dim.Render(" ("+ago(t, now)+")")
}

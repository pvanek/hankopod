package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pvanek/hankopod/internal/core"
)

// col is a column with a minimum width, a share of any leftover width and
// an alignment: numbers right, text left.
type col struct {
	title  string
	min    int
	weight int
	align  align
}

type align bool

const (
	left  align = false
	right align = true
)

// rebuildTable recomputes columns and rows for the active tab and scope.
func (m *Model) rebuildTable() {
	cols, rows, items := m.tableData()
	m.items = items

	// Clear rows first: the table indexes rows by column, so a shorter
	// column set with old rows would render out of range. SetRows(nil)
	// resets the cursor, so restore it afterwards.
	cursor := m.table.Cursor()
	m.table.SetRows(nil)
	laid := layout(cols, m.width)
	m.table.SetColumns(laid)
	m.table.SetRows(alignRows(cols, laid, rows))
	m.table.SetCursor(min(cursor, max(len(rows)-1, 0)))

	used := lipgloss.Height(m.header()) + 2 // + footer, table header border
	m.table.SetWidth(max(m.width, 20))
	m.table.SetHeight(max(m.height-used, 3))
	m.syncDetail()
	m.syncHelp()
}

func layout(cols []col, width int) []table.Column {
	// Each cell gets 2 chars of padding from the default styles.
	avail := width - 2*len(cols)
	total, weights := 0, 0
	for _, c := range cols {
		total += c.min
		weights += c.weight
	}
	extra := max(avail-total, 0)
	out := make([]table.Column, len(cols))
	for i, c := range cols {
		w := c.min
		switch {
		case total > avail:
			// Too narrow (e.g. Termux in portrait): shrink proportionally.
			w = max(3, c.min*max(avail, 0)/total)
		case weights > 0:
			w += extra * c.weight / weights
		}
		out[i] = table.Column{Title: alignCell(c.title, w, c.align), Width: w}
	}
	return out
}

// alignRows right-aligns the cells of right-aligned columns; the table
// itself only aligns left.
func alignRows(cols []col, laid []table.Column, rows []table.Row) []table.Row {
	out := make([]table.Row, len(rows))
	for r, row := range rows {
		out[r] = make(table.Row, len(row))
		for i, v := range row {
			if i < len(cols) && i < len(laid) {
				v = alignCell(v, laid[i].Width, cols[i].align)
			}
			out[r][i] = v
		}
	}
	return out
}

// alignCell pads text on the left to width when right-aligned. Text too
// wide is left as is for the table to shorten.
func alignCell(s string, width int, a align) string {
	if a == right {
		if w := ansi.StringWidth(s); w < width {
			return strings.Repeat(" ", width-w) + s
		}
	}
	return s
}

func (m Model) tableData() ([]col, []table.Row, []any) {
	switch m.tab {
	case tabUsers:
		return m.usersTable()
	case tabSessions:
		return m.sessionsTable()
	case tabActivity:
		return m.activityTable()
	default:
		return m.overviewTable()
	}
}

// --- overview ---

// scopeTotals sums the per-project figures of the scope.
type scopeTotals struct {
	up, checked            int
	users, mau             count
	new24h, new7d, new30d  count
	sessions, sessionUsers count
	logins, failures       count
}

func (m Model) totals() scopeTotals {
	var t scopeTotals
	now := m.now()
	for _, i := range m.inScope() {
		h, u, se := m.health[i], m.users[i], m.sessions[i]
		if h.loaded {
			t.checked++
			if h.data.Status.Up {
				t.up++
			}
		}
		t.mau.add(h.loaded, h.data.MAU, h.data.MAUExact)
		t.logins.add(h.loaded, h.data.Logins, true)
		t.failures.add(h.loaded, h.data.LoginFailures, true)
		t.users.add(u.loaded, u.data.Total, true)
		usersOK := u.loaded && u.data.Err == nil
		t.new24h.addSince(usersOK, u.data, now, 24*time.Hour)
		t.new7d.addSince(usersOK, u.data, now, 7*24*time.Hour)
		t.new30d.addSince(usersOK, u.data, now, core.RecentWindow)
		ok := se.loaded && se.data.UsersScanned > 0
		t.sessions.add(ok, len(se.data.Active), se.data.Complete())
		t.sessionUsers.add(ok, se.data.ActiveUsers(), se.data.Complete())
	}
	return t
}

func newSince(l core.UserList, now time.Time, d time.Duration) (int, bool) {
	return l.NewSince(now.Add(-d))
}

// count is a sum that remembers whether parts were unknown or partial.
type count struct {
	n       int
	known   bool // at least one part is known
	unknown bool // some part is unknown
	atLeast bool // some part is a lower bound
}

func (c *count) add(loaded bool, n int, exact bool) {
	if !loaded || n < 0 {
		c.unknown = true
		return
	}
	c.known = true
	c.n += n
	c.atLeast = c.atLeast || !exact
}

func (c *count) addSince(ok bool, l core.UserList, now time.Time, d time.Duration) {
	n, exact := newSince(l, now, d)
	c.add(ok, n, exact)
}

func (c count) String() string {
	switch {
	case !c.known:
		return "–"
	case c.atLeast || c.unknown:
		return fmt.Sprintf("≥%d", c.n)
	}
	return fmt.Sprint(c.n)
}

func (m Model) tiles() string {
	t := m.totals()
	status := fmt.Sprintf("%d/%d up", t.up, len(m.inScope()))
	switch {
	case t.checked < len(m.inScope()):
		// Still loading: leave uncoloured.
	case t.up == t.checked:
		status = good.Render(status)
	default:
		status = errStyle.Render(status)
	}
	logins := t.logins.String()
	if t.failures.known && t.failures.n > 0 {
		logins += warn.Render(fmt.Sprintf(" %s failed", t.failures))
	}
	tiles := []struct{ title, value string }{
		{"Services", status},
		{"Users", t.users.String()},
		{"MAU " + core.MonthStart(m.now()).Format("Jan"), t.mau.String()},
		{"New 24h / 7d / 30d", t.new24h.String() + " / " + t.new7d.String() + " / " + t.new30d.String()},
		{"Active sessions", fmt.Sprintf("%s (%s users)", t.sessions, t.sessionUsers)},
		{"Logins 24h", logins},
	}
	rendered := make([]string, len(tiles))
	compact := make([]string, len(tiles))
	for i, tl := range tiles {
		rendered[i] = tileStyle.Render(dim.Render(tl.title) + "\n" + tileValue.Render(tl.value))
		compact[i] = dim.Render(tl.title+" ") + tileValue.Render(tl.value)
	}
	head := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if lipgloss.Width(head) > m.width {
		head = lipgloss.NewStyle().Width(max(m.width, 20)).Render(strings.Join(compact, dim.Render(" · ")))
	}
	return head
}

func (m Model) overviewTable() ([]col, []table.Row, []any) {
	cols := []col{
		{"Project", 10, 3, left}, {"Status", 8, 1, left}, {"Latency", 7, 0, right}, {"Users", 6, 0, right},
		{"MAU", 5, 0, right}, {"New 24h", 7, 0, right}, {"New 7d", 6, 0, right}, {"New 30d", 7, 0, right},
		{"Last signup", 11, 1, left}, {"Sessions", 8, 0, right}, {"Logins 24h", 10, 0, right}, {"Failed", 6, 0, right},
	}
	const (
		cStatus = 1 + iota
		cLatency
		cUsers
		cMAU
		cNew24h
		cNew7d
		cNew30d
		cLastSignup
		cSessions
		cLogins
		cFailed
	)
	now := m.now()
	var (
		rows  []table.Row
		items []any
	)
	for _, i := range m.inScope() {
		h, u, se := m.health[i], m.users[i], m.sessions[i]
		row := make(table.Row, len(cols))
		row[0], row[cStatus] = m.projects[i].Name, "…"
		if h.loaded {
			d := h.data
			row[cStatus], row[cLatency] = d.Status.String(), fmtLatency(d.Status.Latency)
			row[cMAU] = num(d.MAU)
			if d.MAU >= 0 && !d.MAUExact {
				row[cMAU] = fmtCount(d.MAU, false)
			}
			row[cLogins], row[cFailed] = num(d.Logins), num(d.LoginFailures)
		}
		if u.loaded {
			d := u.data
			row[cUsers] = num(d.Total)
			if d.Err == nil {
				row[cNew24h] = fmtCount(newSince(d, now, 24*time.Hour))
				row[cNew7d] = fmtCount(newSince(d, now, 7*24*time.Hour))
				row[cNew30d] = fmtCount(newSince(d, now, core.RecentWindow))
				row[cLastSignup] = ago(d.LastSignup(), now)
			}
		}
		if se.loaded && se.data.UsersScanned > 0 {
			row[cSessions] = fmtCount(len(se.data.Active), se.data.Complete())
		} else if se.loading {
			row[cSessions] = "…"
		}
		rows = append(rows, row)
		items = append(items, i)
	}
	if m.width < narrowWidth {
		cols, rows = pick(cols, rows, 0, cStatus, cUsers, cMAU, cNew7d, cSessions, cLogins)
	}
	return cols, rows, items
}

// narrowWidth is below which the Overview drops its secondary columns.
const narrowWidth = 100

// pick keeps the given columns of cols and rows.
func pick(cols []col, rows []table.Row, keep ...int) ([]col, []table.Row) {
	outCols := make([]col, len(keep))
	for j, k := range keep {
		outCols[j] = cols[k]
	}
	outRows := make([]table.Row, len(rows))
	for i, r := range rows {
		outRows[i] = make(table.Row, len(keep))
		for j, k := range keep {
			outRows[i][j] = r[k]
		}
	}
	return outCols, outRows
}

// --- users ---

func (m Model) usersTable() ([]col, []table.Row, []any) {
	cols := []col{
		{"User", 16, 4, left}, {"Project", 8, 1, left}, {"Registered", 16, 0, left}, {"Verified", 8, 0, left},
		{"Passkeys", 8, 0, right}, {"Last passkey use", 16, 0, left},
	}
	var users []core.User
	for _, i := range m.inScope() {
		users = append(users, m.users[i].data.Recent...)
	}
	sort.SliceStable(users, func(a, b int) bool { return users[a].CreatedAt.After(users[b].CreatedAt) })
	var (
		rows  []table.Row
		items []any
	)
	for _, u := range users {
		verified := ""
		if u.Email != "" {
			verified = yesNo(u.Verified)
		}
		rows = append(rows, table.Row{
			u.Label(), u.Project, fmtTime(u.CreatedAt), verified, fmt.Sprint(len(u.Passkeys)), fmtTime(u.LastPasskeyUse),
		})
		items = append(items, u)
	}
	return cols, rows, items
}

// --- sessions ---

func (m Model) sessionsNote() string {
	var parts []string
	for _, i := range m.inScope() {
		se := m.sessions[i]
		if !se.loaded {
			continue
		}
		s := fmt.Sprintf("%s: %d active, %d of %d users scanned", m.projects[i].Name,
			len(se.data.Active), se.data.UsersScanned, se.data.UsersTotal)
		if se.data.Failed > 0 {
			s += fmt.Sprintf(", %d failed", se.data.Failed)
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return dim.Render("scanning sessions…")
	}
	return dim.Width(max(m.width, 20)).Render(strings.Join(parts, " · "))
}

func (m Model) sessionsTable() ([]col, []table.Row, []any) {
	cols := []col{
		{"User", 14, 3, left}, {"Project", 8, 1, left}, {"Last used", 9, 0, left}, {"Created", 16, 0, left},
		{"Expires", 9, 0, left}, {"IP", 10, 1, left}, {"User agent", 10, 3, left},
	}
	now := m.now()
	var sessions []core.Session
	for _, i := range m.inScope() {
		sessions = append(sessions, m.sessions[i].data.Active...)
	}
	sort.SliceStable(sessions, func(a, b int) bool { return sessions[a].LastUsed.After(sessions[b].LastUsed) })
	var (
		rows  []table.Row
		items []any
	)
	for _, s := range sessions {
		expires := "–"
		if !s.ExpiresAt.IsZero() {
			expires = "in " + fmtAge(s.ExpiresAt.Sub(now))
		}
		rows = append(rows, table.Row{
			s.User, s.Project, ago(s.LastUsed, now), fmtTime(s.CreatedAt), expires, s.IP, s.UserAgent,
		})
		items = append(items, s)
	}
	return cols, rows, items
}

// --- activity ---

func (m Model) activityTable() ([]col, []table.Row, []any) {
	cols := []col{
		{"Time", 16, 0, left}, {"Project", 8, 1, left}, {"Event", 16, 2, left}, {"Actor", 12, 3, left},
		{"IP", 10, 1, left}, {"Error", 8, 3, left},
	}
	var events []core.Event
	for _, i := range m.inScope() {
		events = append(events, m.events[i].data.Events...)
	}
	sort.SliceStable(events, func(a, b int) bool { return events[a].CreatedAt.After(events[b].CreatedAt) })
	var (
		rows  []table.Row
		items []any
	)
	for _, e := range events {
		typ := e.Type
		if e.Failed() {
			typ = "✗ " + typ
		}
		rows = append(rows, table.Row{fmtTime(e.CreatedAt), e.Project, typ, actor(e), e.IP, e.Error})
		items = append(items, e)
	}
	return cols, rows, items
}

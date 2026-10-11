package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pvanek/hankopod/internal/core"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

type fakeFetcher struct{}

func (fakeFetcher) Projects() []core.Project {
	return []core.Project{{Name: "shop"}, {Name: "blog"}}
}

var errDown = errors.New("hanko: 503 Service Unavailable")

func (fakeFetcher) Health(_ context.Context, i int, _ time.Time) core.Health {
	if i == 1 {
		return core.Health{Project: "blog", FetchedAt: now, MAU: -1, Logins: -1, LoginFailures: -1,
			Status: core.Status{Err: errDown}}
	}
	return core.Health{Project: "shop", FetchedAt: now, MAU: 87, MAUExact: true, Logins: 17, LoginFailures: 2,
		Status: core.Status{Up: true, Latency: 85 * time.Millisecond}}
}

func (fakeFetcher) Users(_ context.Context, i int, _ time.Time) core.UserList {
	if i == 1 {
		return core.UserList{Project: "blog", FetchedAt: now, Total: -1, Err: errDown}
	}
	return core.UserList{
		Project: "shop", FetchedAt: now, Total: 120, Complete: true,
		Recent: []core.User{
			{ID: "u1", Project: "shop", Email: "new@x.io", Verified: true, CreatedAt: now.Add(-2 * time.Hour),
				Emails:   []core.Email{{Address: "new@x.io", Verified: true, Primary: true}, {Address: "alt@x.io"}},
				Passkeys: []core.Passkey{{Name: "iPhone", CreatedAt: now.Add(-2 * time.Hour), LastUsed: now.Add(-5 * time.Minute)}}},
			{ID: "u2", Project: "shop", Email: "old@x.io", CreatedAt: now.Add(-3 * 24 * time.Hour)},
			{ID: "u3", Project: "shop", Username: "ancient", CreatedAt: now.Add(-40 * 24 * time.Hour)},
		},
	}
}

func (fakeFetcher) Events(_ context.Context, i int, _ time.Time) core.EventList {
	if i == 1 {
		return core.EventList{Project: "blog", FetchedAt: now, Err: errDown}
	}
	return core.EventList{Project: "shop", FetchedAt: now, Events: []core.Event{
		{ID: "e1", Project: "shop", Type: "login_failure", Email: "old@x.io", CreatedAt: now.Add(-time.Minute)},
		{ID: "e2", Project: "shop", Type: "user_created", Email: "new@x.io", CreatedAt: now.Add(-2 * time.Hour)},
	}}
}

func (fakeFetcher) Sessions(_ context.Context, i int, _ time.Time) core.SessionStats {
	if i == 1 {
		return core.SessionStats{Project: "blog", FetchedAt: now, Err: errors.New("hanko: 503")}
	}
	return core.SessionStats{
		Project: "shop", FetchedAt: now, UsersScanned: 120, UsersTotal: 120,
		Active: []core.Session{
			{ID: "s1", Project: "shop", UserID: "u1", User: "new@x.io", IP: "10.0.0.1", LastUsed: now.Add(-5 * time.Minute), ExpiresAt: now.Add(time.Hour)},
			{ID: "s2", Project: "shop", UserID: "u1", User: "new@x.io", IP: "10.0.0.2", LastUsed: now.Add(-time.Hour)},
		},
	}
}

// fresh returns a model with nothing loaded and a window size set.
func fresh(t *testing.T, width int) Model {
	t.Helper()
	m := New(fakeFetcher{})
	m.now = func() time.Time { return now }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return tm.(Model)
}

// loaded returns a model with every data set of every project fetched.
func loaded(t *testing.T, width int) Model {
	t.Helper()
	var tm tea.Model = fresh(t, width)
	f, ctx := fakeFetcher{}, context.Background()
	for i := range f.Projects() {
		tm, _ = tm.Update(loadedMsg[core.Health]{i: i, data: f.Health(ctx, i, now)})
		tm, _ = tm.Update(loadedMsg[core.UserList]{i: i, data: f.Users(ctx, i, now)})
		tm, _ = tm.Update(loadedMsg[core.EventList]{i: i, data: f.Events(ctx, i, now)})
		tm, _ = tm.Update(loadedMsg[core.SessionStats]{i: i, data: f.Sessions(ctx, i, now)})
	}
	return tm.(Model)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	var tm tea.Model = m
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		tm, _ = tm.Update(msg)
	}
	return tm.(Model)
}

func assertContains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func TestOverview(t *testing.T) {
	m := loaded(t, 160)
	out := m.render()
	assertContains(t, out,
		"1/2 up", "≥120", "MAU Oct", "≥87", "≥1 / ≥2 / ≥2", "MAU", "≥2 (≥1 users)", "17", "2 failed",
		"shop", "85ms", "2h ago", "blog", "DOWN",
		"blog status: hanko: 503",
	)
}

func TestScope(t *testing.T) {
	m := loaded(t, 160)
	m = press(t, m, "p") // all -> shop
	out := m.render()
	assertContains(t, out, "Project: ", "shop", "1/1 up", "120", "87", "1 / 2 / 2", "2 (1 users)")
	if strings.Contains(out, "blog") {
		t.Errorf("scoped view still shows blog:\n%s", out)
	}
	m = press(t, m, "p") // shop -> blog
	assertContains(t, m.render(), "blog")
	m = press(t, m, "p") // back to all
	assertContains(t, m.render(), "All projects (2)")
}

func TestTabs(t *testing.T) {
	m := loaded(t, 160)
	assertContains(t, press(t, m, "1").render(), "new@x.io", "old@x.io", "ancient")
	assertContains(t, press(t, m, "2").render(), "10.0.0.1", "in 1h", "shop: 2 active, 120 of 120 users scanned")
	assertContains(t, press(t, m, "3").render(), "✗ login_failure", "user_created")
}

func TestNarrowFits(t *testing.T) {
	for tb := range tabCount {
		m := press(t, loaded(t, 50), string(rune('0'+tb)))
		for _, line := range strings.Split(m.render(), "\n") {
			if w := lipgloss.Width(line); w > 50 {
				t.Errorf("tab %d: line is %d wide: %q", tb, w, line)
			}
		}
	}
}

func TestDetail(t *testing.T) {
	m := loaded(t, 160)
	for _, c := range []struct {
		keys []string
		want []string
	}{
		{[]string{"enter"}, []string{"Project shop", "Admin API", "85ms", "Newest", "new@x.io",
			"Users scanned", "120 of 120", "MAU Oct", "87", "since Oct 1 UTC", "Recent failures", "login_failure  old@x.io"}},
		{[]string{"down", "enter"}, []string{"Project blog", "DOWN", "hanko: 503 Service Unavailable"}},
		{[]string{"1", "enter"}, []string{"User new@x.io", "alt@x.io", "unverified", "iPhone", "used 5m ago",
			"10.0.0.1", "10.0.0.2", "user_created"}},
		{[]string{"2", "enter"}, []string{"Session of new@x.io", "10.0.0.1", "Other sessions", "10.0.0.2"}},
		{[]string{"3", "enter"}, []string{"Event login_failure", "old@x.io", "Request ID"}},
	} {
		d := press(t, m, c.keys...)
		if d.detail == nil {
			t.Fatalf("%v: no detail open", c.keys)
		}
		out := d.render()
		assertContains(t, out, append(c.want, "esc back")...)

		back := press(t, d, "esc")
		if back.detail != nil {
			t.Fatalf("%v: esc did not close the detail", c.keys)
		}
		assertContains(t, back.render(), "enter detail")
		if back.tab != d.tab {
			t.Errorf("%v: esc changed tab from %d to %d", c.keys, d.tab, back.tab)
		}
	}
}

func TestDetailSurvivesRefreshAndTabSwitchCloses(t *testing.T) {
	m := press(t, loaded(t, 160), "1", "enter")
	var tm tea.Model = m
	f := fakeFetcher{}
	tm, _ = tm.Update(loadedMsg[core.UserList]{i: 0, data: f.Users(context.Background(), 0, now)})
	m = tm.(Model)
	if m.detail == nil {
		t.Fatal("refresh closed the detail")
	}
	assertContains(t, m.render(), "User new@x.io")

	m = press(t, m, "3")
	if m.detail != nil || m.tab != tabActivity {
		t.Errorf("tab key: detail=%v tab=%d", m.detail != nil, m.tab)
	}
}

func TestDetailNarrowFits(t *testing.T) {
	for tb := range tabCount {
		m := press(t, loaded(t, 50), string(rune('0'+tb)), "enter")
		for _, line := range strings.Split(m.render(), "\n") {
			if w := lipgloss.Width(line); w > 50 {
				t.Errorf("tab %d detail: line is %d wide: %q", tb, w, line)
			}
		}
	}
}

func anyLoading[T any](rs []resource[T]) bool {
	for _, r := range rs {
		if r.loading {
			return true
		}
	}
	return false
}

// loadingSets reports which data sets have a fetch running, as "h u e s"
// with "-" for idle ones, per project: e.g. "hu-s|----".
func loadingSets(m Model) string {
	var parts []string
	for i := range m.projects {
		b := []byte("----")
		for j, l := range []bool{m.health[i].loading, m.users[i].loading, m.events[i].loading, m.sessions[i].loading} {
			if l {
				b[j] = "hues"[j]
			}
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "|")
}

func TestLazyStart(t *testing.T) {
	m := fresh(t, 160)
	m.Init()
	// Overview: health, users and sessions of all projects; no activity.
	if got := loadingSets(m); got != "hu-s|hu-s" {
		t.Errorf("after start loading %q, want hu-s|hu-s", got)
	}
}

func TestLazyTabSwitch(t *testing.T) {
	m := press(t, fresh(t, 160), "3")
	if got := loadingSets(m); got != "--e-|--e-" {
		t.Errorf("Activity loads %q, want only activity", got)
	}
	// Once loaded, coming back loads nothing new.
	m = loaded(t, 160)
	for _, k := range []string{"1", "2", "3", "0"} {
		if m = press(t, m, k); loadingSets(m) != "----|----" {
			t.Errorf("tab %s reloaded loaded data: %q", k, loadingSets(m))
		}
	}
}

func TestLazyScope(t *testing.T) {
	m := press(t, fresh(t, 160), "p") // shop only
	if got := loadingSets(m); got != "hu-s|----" {
		t.Errorf("scoped to shop loads %q", got)
	}
	m = press(t, m, "p", "p") // blog, then all: blog loads once
	if got := loadingSets(m); got != "hu-s|hu-s" {
		t.Errorf("after all projects loading %q", got)
	}
}

func TestRefreshOnlyVisible(t *testing.T) {
	m := press(t, loaded(t, 160), "3", "r")
	if got := loadingSets(m); got != "--e-|--e-" {
		t.Errorf("r on Activity reloads %q", got)
	}
	m = press(t, loaded(t, 160), "p", "r") // Overview, shop only
	if got := loadingSets(m); got != "hu-s|----" {
		t.Errorf("r on scoped Overview reloads %q", got)
	}
}

func TestLazyDetail(t *testing.T) {
	// A user detail loads the activity it lists, but not the session scan.
	m := press(t, fresh(t, 160), "1")
	var tm tea.Model = m
	f := fakeFetcher{}
	for i := range f.Projects() {
		tm, _ = tm.Update(loadedMsg[core.UserList]{i: i, data: f.Users(context.Background(), i, now)})
	}
	m = press(t, tm.(Model), "enter")
	if got := loadingSets(m); got != "--e-|--e-" {
		t.Errorf("user detail loads %q", got)
	}
	assertContains(t, m.render(), "User new@x.io", "open the Sessions tab")
}

func TestAutoRefreshPerTab(t *testing.T) {
	m := loaded(t, 160)
	if !m.auto[tabOverview] {
		t.Fatal("Overview auto-refresh should start on")
	}
	for _, tb := range []tab{tabUsers, tabSessions, tabActivity} {
		if m.auto[tb] {
			t.Errorf("tab %d auto-refresh on by default", tb)
		}
	}
	assertContains(t, m.render(), "a auto 30m")

	// Overview: not due at 29m; due at 31m, reloading only what it shows.
	m.now = func() time.Time { return now.Add(29 * time.Minute) }
	if m.onAutoTick(); loadingSets(m) != "----|----" {
		t.Errorf("refreshed before the 30m interval: %q", loadingSets(m))
	}
	m.now = func() time.Time { return now.Add(31 * time.Minute) }
	if m.onAutoTick(); loadingSets(m) != "hu-s|hu-s" {
		t.Errorf("Overview refresh after 30m loads %q", loadingSets(m))
	}

	// Users: off, so a due tick does nothing.
	m = press(t, loaded(t, 160), "1")
	m.now = func() time.Time { return now.Add(time.Hour) }
	if m.onAutoTick(); anyLoading(m.users) {
		t.Error("users refreshed with auto-refresh off")
	}
	assertContains(t, m.render(), "a auto off")

	// Toggling on refreshes immediately, and only what the tab shows.
	cmd, _ := m.handleKey("a")
	if cmd == nil || !m.auto[tabUsers] || loadingSets(m) != "-u--|-u--" {
		t.Fatalf("toggle on: cmd=%v auto=%v loading=%q", cmd != nil, m.auto[tabUsers], loadingSets(m))
	}
	assertContains(t, m.render(), "a auto 30m")

	// Toggling off leaves the other tabs alone.
	m.handleKey("a")
	if m.auto[tabUsers] || !m.auto[tabOverview] {
		t.Errorf("toggle off affected wrong tabs: users=%v overview=%v", m.auto[tabUsers], m.auto[tabOverview])
	}
}

func TestAutoRefreshOnlyVisibleTab(t *testing.T) {
	m := press(t, loaded(t, 160), "2") // sessions, auto off; overview still on
	m.now = func() time.Time { return now.Add(time.Hour) }
	if m.onAutoTick(); loadingSets(m) != "----|----" {
		t.Errorf("hidden Overview refreshed while viewing Sessions: %q", loadingSets(m))
	}
	// Sessions on: only the session scan runs.
	m.handleKey("a")
	if got := loadingSets(m); got != "---s|---s" {
		t.Errorf("sessions toggle loads %q", got)
	}
}

func TestAutoToggleInDetail(t *testing.T) {
	m := press(t, loaded(t, 160), "3", "enter", "a")
	if !m.auto[tabActivity] || m.detail == nil {
		t.Errorf("a in detail: auto=%v detail open=%v", m.auto[tabActivity], m.detail != nil)
	}
}

func TestStatusBar(t *testing.T) {
	for _, width := range []int{160, 60} {
		m := loaded(t, width)
		lines := strings.Split(m.render(), "\n")
		bar := ansi.Strip(lines[len(lines)-1])
		if lipgloss.Width(bar) != width || !strings.HasSuffix(strings.TrimRight(bar, " "), "q quit") {
			t.Errorf("width %d: bar %q (width %d) should end with q quit at the edge", width, bar, lipgloss.Width(bar))
		}
		if !strings.Contains(bar, "a auto 30m") {
			t.Errorf("width %d: bar lacks auto state: %q", width, bar)
		}
	}
	m := press(t, loaded(t, 100), "enter")
	lines := strings.Split(m.render(), "\n")
	if bar := ansi.Strip(lines[len(lines)-1]); !strings.HasPrefix(bar, "esc back") || !strings.HasSuffix(bar, "q quit") {
		t.Errorf("detail bar = %q", bar)
	}
}

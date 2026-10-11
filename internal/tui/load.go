package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pvanek/hankopod/internal/core"
)

// Data is loaded lazily: each screen loads only the data sets it shows,
// for the projects in scope, the first time they are needed. "r" and
// auto-refresh reload the visible screen only.

// dataset is one kind of per-project data.
type dataset int

const (
	dsHealth   dataset = iota // status, MAU, login counts
	dsUsers                   // user count and newest users
	dsEvents                  // latest audit logs
	dsSessions                // session scan, one request per user
)

const (
	fetchTimeout    = 30 * time.Second
	sessionsTimeout = 3 * time.Minute
)

// tabData lists the data sets each tab shows.
var tabData = [tabCount][]dataset{
	tabOverview: {dsHealth, dsUsers, dsSessions},
	tabUsers:    {dsUsers},
	tabSessions: {dsSessions},
	tabActivity: {dsEvents},
}

// resource is the load state of one project's data set.
type resource[T any] struct {
	loading bool
	loaded  bool
	data    T
}

// loadedMsg carries one project's data set back into Update.
type loadedMsg[T any] struct {
	i    int
	data T
}

type fetchFunc[T any] func(ctx context.Context, i int, now time.Time) T

// load starts fetching r unless a fetch is already running.
func load[T any](r *resource[T], i int, fetch fetchFunc[T], timeout time.Duration, now time.Time) tea.Cmd {
	if r.loading {
		return nil
	}
	r.loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return loadedMsg[T]{i: i, data: fetch(ctx, i, now)}
	}
}

// loadSet fetches data set ds of project i; unless force, only if it was
// never loaded.
func (m *Model) loadSet(ds dataset, i int, force bool) tea.Cmd {
	now := m.now()
	switch ds {
	case dsHealth:
		if force || !m.health[i].loaded {
			return load(&m.health[i], i, m.svc.Health, fetchTimeout, now)
		}
	case dsUsers:
		if force || !m.users[i].loaded {
			return load(&m.users[i], i, m.svc.Users, fetchTimeout, now)
		}
	case dsEvents:
		if force || !m.events[i].loaded {
			return load(&m.events[i], i, m.svc.Events, fetchTimeout, now)
		}
	case dsSessions:
		if force || !m.sessions[i].loaded {
			return load(&m.sessions[i], i, m.svc.Sessions, sessionsTimeout, now)
		}
	}
	return nil
}

// visibleData lists the data sets the visible screen shows: the tab's,
// plus the activity that project and user details list.
func (m Model) visibleData() []dataset {
	ds := tabData[m.tab]
	if m.detail != nil {
		switch m.detail.item.(type) {
		case int, core.User:
			if !slices.Contains(ds, dsEvents) {
				ds = append(slices.Clone(ds), dsEvents)
			}
		}
	}
	return ds
}

// ensureVisible loads what the visible screen shows and was never loaded.
func (m *Model) ensureVisible() tea.Cmd { return m.loadVisible(false) }

// refreshVisible reloads everything the visible screen shows.
func (m *Model) refreshVisible() tea.Cmd { return m.loadVisible(true) }

func (m *Model) loadVisible(force bool) tea.Cmd {
	var cmds []tea.Cmd
	for _, ds := range m.visibleData() {
		for _, i := range m.inScope() {
			cmds = append(cmds, m.loadSet(ds, i, force))
		}
	}
	return tea.Batch(cmds...)
}

// setState is the load state of one data set of one project.
type setState struct {
	loading, loaded bool
	fetchedAt       time.Time
	errs            []error // labelled errors
}

func (m Model) state(ds dataset, i int) setState {
	label := func(what string, err error) []error {
		if err == nil {
			return nil
		}
		return []error{fmt.Errorf("%s %s: %w", m.projects[i].Name, what, err)}
	}
	switch ds {
	case dsHealth:
		r := m.health[i]
		return setState{r.loading, r.loaded, r.data.FetchedAt,
			append(label("status", r.data.Status.Err), label("activity counts", r.data.Err)...)}
	case dsUsers:
		r := m.users[i]
		return setState{r.loading, r.loaded, r.data.FetchedAt, label("users", r.data.Err)}
	case dsEvents:
		r := m.events[i]
		return setState{r.loading, r.loaded, r.data.FetchedAt, label("activity", r.data.Err)}
	case dsSessions:
		r := m.sessions[i]
		return setState{r.loading, r.loaded, r.data.FetchedAt, label("sessions", r.data.Err)}
	}
	return setState{}
}

// visibleStates returns the states of the visible data sets in scope.
func (m Model) visibleStates() []setState {
	var out []setState
	for _, ds := range m.visibleData() {
		for _, i := range m.inScope() {
			out = append(out, m.state(ds, i))
		}
	}
	return out
}

// lastFetch is when the oldest visible data was fetched, so "updated"
// never overstates freshness.
func (m Model) lastFetch() time.Time {
	var t time.Time
	for _, s := range m.visibleStates() {
		if s.loaded && (t.IsZero() || s.fetchedAt.Before(t)) {
			t = s.fetchedAt
		}
	}
	return t
}

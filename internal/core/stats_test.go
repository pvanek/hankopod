package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStatsHanko serves the users of fakeHanko and audit logs filtered
// like Hanko's: per day d going back days, a login_success of user
// u<d%5> and an anonymous login_failure, both at now-d days-2h.
func fakeStatsHanko(t *testing.T, users, days int, userPages *atomic.Int32) *httptest.Server {
	t.Helper()
	type log struct {
		ID        string    `json:"id"`
		Type      string    `json:"type"`
		Actor     string    `json:"actor_user_id,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}
	var logs []log // newest first
	for d := range days {
		at := now.Add(-time.Duration(d)*24*time.Hour - 2*time.Hour)
		logs = append(logs,
			log{ID: "s" + strconv.Itoa(d), Type: "login_success", Actor: "u" + strconv.Itoa(d%5), CreatedAt: at},
			log{ID: "f" + strconv.Itoa(d), Type: "login_failure", CreatedAt: at})
	}
	base := fakeHanko(t, users)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/audit_logs" {
			if r.URL.Path == "/admin/users" {
				userPages.Add(1)
			}
			base.Config.Handler.ServeHTTP(w, r)
			return
		}
		q := r.URL.Query()
		var start, end time.Time
		if v := q.Get("start_time"); v != "" {
			start, _ = time.Parse(time.RFC3339, v)
		}
		if v := q.Get("end_time"); v != "" {
			end, _ = time.Parse(time.RFC3339, v)
		}
		var match []log
		for _, l := range logs {
			if slices.Contains(q["type"], l.Type) && !l.CreatedAt.Before(start) && (end.IsZero() || l.CreatedAt.Before(end)) {
				match = append(match, l)
			}
		}
		page, _ := strconv.Atoi(q.Get("page"))
		per, _ := strconv.Atoi(q.Get("per_page"))
		page = max(page, 1)
		lo, hi := min((page-1)*per, len(match)), min(page*per, len(match))
		w.Header().Set("X-Total-Count", strconv.Itoa(len(match)))
		_ = json.NewEncoder(w).Encode(match[lo:hi])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStatsMonthly(t *testing.T) {
	var userPages atomic.Int32
	// 2500 users, one a day: 25 pages, of which the search reads only some.
	srv := fakeStatsHanko(t, 2500, 60, &userPages)
	svc := NewService([]Project{{Name: "shop", AdminURL: srv.URL + "/admin", APIKey: "key"}}, "test", 100)
	st := svc.Stats(context.Background(), 0, now, Monthly)
	if st.Err != nil {
		t.Fatal(st.Err)
	}
	if len(st.Periods) != 12 || !st.Periods[0].Start.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) ||
		!st.Periods[11].Start.Equal(time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("periods %v … %v", st.Periods[0].Start, st.Periods[len(st.Periods)-1].Start)
	}
	// Users are created at now-1h-i days: Oct 1–11 holds i 0..10.
	oct, sep := st.Periods[0], st.Periods[1]
	if oct.Registered != 11 || oct.Users != 2500 || sep.Registered != 30 || sep.Users != 2489 {
		t.Errorf("oct %d/%d, sep %d/%d", oct.Registered, oct.Users, sep.Registered, sep.Users)
	}
	if n := userPages.Load(); n > 12 {
		t.Errorf("read %d user pages", n)
	}
	// Logs at now-2h-d days, d < 60: Oct holds d 0..10, Sep d 11..40.
	if oct.Active != 5 || !oct.ActiveExact || oct.Logins != 11 || oct.LoginFailures != 11 {
		t.Errorf("oct active %d exact=%v logins %d failures %d", oct.Active, oct.ActiveExact, oct.Logins, oct.LoginFailures)
	}
	if sep.Logins != 30 || st.Periods[2].Logins != 19 || st.Periods[3].Logins != 0 || st.Periods[3].Active != 0 {
		t.Errorf("sep %d aug %d jul %d/%d", sep.Logins, st.Periods[2].Logins, st.Periods[3].Logins, st.Periods[3].Active)
	}
}

func TestStatsActiveCutOff(t *testing.T) {
	defer func(n int) { maxStatsPages = n }(maxStatsPages)
	maxStatsPages = 1
	// 150 days of logins; one page of 100 reaches back to d 99 (early July).
	srv := fakeStatsHanko(t, 10, 150, new(atomic.Int32))
	svc := NewService([]Project{{Name: "shop", AdminURL: srv.URL + "/admin", APIKey: "key"}}, "test", 100)
	st := svc.Stats(context.Background(), 0, now, Monthly)
	if st.Err != nil {
		t.Fatal(st.Err)
	}
	for k, want := range []struct {
		active int
		exact  bool
	}{{5, true}, {5, true}, {5, true}, {5, false}, {-1, false}} {
		if p := st.Periods[k]; p.Active != want.active || p.ActiveExact != want.exact {
			t.Errorf("%s: active %d exact=%v, want %d %v", p.Start.Format("Jan"), p.Active, p.ActiveExact, want.active, want.exact)
		}
	}
}

func TestPeriodStarts(t *testing.T) {
	// Sunday Oct 11 2026: its week started on Monday Oct 5.
	for _, c := range []struct {
		g          Granularity
		first, sec string
	}{
		{Daily, "2026-10-11", "2026-10-10"},
		{Weekly, "2026-10-05", "2026-09-28"},
		{Monthly, "2026-10-01", "2026-09-01"},
		{Yearly, "2026-01-01", "2025-01-01"},
	} {
		s := c.g.PeriodStarts(now)
		if got := s[0].Format(time.DateOnly) + " " + s[1].Format(time.DateOnly); got != c.first+" "+c.sec {
			t.Errorf("%s: %s", c.g, got)
		}
		if !c.g.Next(s[1]).Equal(s[0]) {
			t.Errorf("%s: Next(%s) = %s", c.g, s[1], c.g.Next(s[1]))
		}
	}
}

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// fakeHanko serves users created one per day going back n days, each with
// one session (expired for every third user).
func fakeHanko(t *testing.T, n int) *httptest.Server {
	t.Helper()
	type user struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		Emails    []any     `json:"emails"`
	}
	users := make([]user, n) // newest first
	for i := range users {
		users[i] = user{
			ID:        fmt.Sprintf("u%d", i),
			CreatedAt: now.Add(-time.Duration(i)*24*time.Hour - time.Hour),
			Emails:    []any{map[string]any{"address": fmt.Sprintf("u%d@x.io", i), "is_primary": true, "is_verified": true}},
		}
	}
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer key" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"Unauthorized"}`))
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /admin/{$}", auth(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	// Public status page, unauthenticated (where Hanko Cloud serves it).
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>API is running</html>"))
	})
	mux.HandleFunc("GET /admin/users", auth(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		list := users
		if r.URL.Query().Get("sort_direction") == "asc" {
			list = make([]user, n)
			for i := range users {
				list[i] = users[n-1-i]
			}
		}
		lo, hi := min((page-1)*per, n), min(page*per, n)
		w.Header().Set("X-Total-Count", strconv.Itoa(n))
		_ = json.NewEncoder(w).Encode(list[lo:hi])
	}))
	mux.HandleFunc("GET /admin/users/{id}/sessions", auth(func(w http.ResponseWriter, r *http.Request) {
		var i int
		fmt.Sscanf(r.PathValue("id"), "u%d", &i)
		exp := now.Add(time.Hour)
		if i%3 == 0 {
			exp = now.Add(-time.Hour)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "s" + strconv.Itoa(i), "user_id": r.PathValue("id"),
			"created_at": now, "updated_at": now, "last_used": now.Add(-time.Duration(i) * time.Minute), "expires_at": exp,
		}})
	}))
	mux.HandleFunc("GET /admin/audit_logs", auth(func(w http.ResponseWriter, r *http.Request) {
		if types := r.URL.Query()["type"]; len(types) > 1 {
			// MAU query: u0 twice, u1 once, one log without an actor.
			_, _ = w.Write([]byte(`[{"id":"m1","type":"login_success","actor_user_id":"u0"},
				{"id":"m2","type":"user_created","actor_user_id":"u1"},
				{"id":"m3","type":"login_success","actor_user_id":"u0"},
				{"id":"m4","type":"token_exchange_succeeded"}]`))
			return
		}
		total := map[string]string{"login_success": "42", "login_failure": "3", "": "2"}[r.URL.Query().Get("type")]
		w.Header().Set("X-Total-Count", total)
		_, _ = w.Write([]byte(`[{"id":"a1","type":"login_failure","created_at":"2026-10-11T11:00:00Z"},
			{"id":"a2","type":"user_created","actor_email":"u0@x.io","created_at":"2026-10-11T10:00:00Z"}]`))
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHealthUsersEvents(t *testing.T) {
	srv := fakeHanko(t, 250)
	svc := NewService([]Project{{Name: "shop", AdminURL: srv.URL + "/admin", APIKey: "key"}}, "test", 100)
	ctx := context.Background()

	h := svc.Health(ctx, 0, now)
	if !h.Status.Up || h.Status.Err != nil || h.Status.String() != "up" {
		t.Errorf("status = %+v", h.Status)
	}
	if h.MAU != 2 || !h.MAUExact {
		t.Errorf("MAU = %d exact=%v", h.MAU, h.MAUExact)
	}
	if h.Logins != 42 || h.LoginFailures != 3 || h.Err != nil {
		t.Errorf("logins=%d failures=%d err=%v", h.Logins, h.LoginFailures, h.Err)
	}

	u := svc.Users(ctx, 0, now)
	if u.Err != nil || u.Total != 250 {
		t.Fatalf("users total=%d err=%v", u.Total, u.Err)
	}
	// Window is 30 days: the first page of 100 covers it.
	if !u.Complete || len(u.Recent) != 100 {
		t.Errorf("recent = %d complete=%v", len(u.Recent), u.Complete)
	}
	for _, c := range []struct {
		d    time.Duration
		want int
	}{{24 * time.Hour, 1}, {7 * 24 * time.Hour, 7}, {RecentWindow, 30}} {
		if n, exact := u.NewSince(now.Add(-c.d)); n != c.want || !exact {
			t.Errorf("NewSince(%s) = %d, %v; want %d", c.d, n, exact, c.want)
		}
	}
	if got := u.LastSignup(); !got.Equal(now.Add(-time.Hour)) {
		t.Errorf("last signup = %s", got)
	}

	e := svc.Events(ctx, 0, now)
	if e.Err != nil || len(e.Events) != 2 || !e.Events[0].Failed() || e.Events[1].Failed() {
		t.Errorf("events = %+v err=%v", e.Events, e.Err)
	}
}

func TestBadKey(t *testing.T) {
	srv := fakeHanko(t, 5)
	svc := NewService([]Project{{Name: "shop", AdminURL: srv.URL + "/admin", APIKey: "wrong"}}, "test", 100)
	ctx := context.Background()
	h, u, e := svc.Health(ctx, 0, now), svc.Users(ctx, 0, now), svc.Events(ctx, 0, now)
	if h.Status.Up || !h.Status.Unauthorized || h.Err == nil || u.Err == nil || e.Err == nil {
		t.Errorf("expected errors, got %+v / %+v / %+v", h, u, e)
	}
	if u.Total != -1 || h.Logins != -1 || h.MAU != -1 {
		t.Errorf("unknown counts should be -1: users=%d logins=%d mau=%d", u.Total, h.Logins, h.MAU)
	}
}

func TestSessions(t *testing.T) {
	srv := fakeHanko(t, 150)
	svc := NewService([]Project{{Name: "shop", AdminURL: srv.URL + "/admin", APIKey: "key"}}, "test", 120)
	st := svc.Sessions(context.Background(), 0, now)
	if st.Err != nil {
		t.Fatal(st.Err)
	}
	if st.UsersScanned != 120 || st.UsersTotal != 150 || st.Complete() {
		t.Errorf("scanned %d of %d, complete=%v", st.UsersScanned, st.UsersTotal, st.Complete())
	}
	// Oldest 120 users are u149..u30; a third of them have expired sessions.
	if len(st.Active) != 80 || st.ActiveUsers() != 80 {
		t.Errorf("active = %d users = %d", len(st.Active), st.ActiveUsers())
	}
	if st.Active[0].User != "u31@x.io" {
		t.Errorf("most recently used first, got %s", st.Active[0].User)
	}
}

func TestHealthPublicStatus(t *testing.T) {
	srv := fakeHanko(t, 5)
	// Like Hanko Cloud: no status page under /admin.
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(cloud.Close)

	for _, c := range []struct {
		key, want string
	}{{"key", "up"}, {"wrong", "AUTH"}} {
		svc := NewService([]Project{{Name: "shop", AdminURL: cloud.URL + "/admin", StatusURL: cloud.URL + "/", APIKey: c.key}}, "test", 100)
		h := svc.Health(context.Background(), 0, now)
		if got := h.Status.String(); got != c.want || !h.Status.Up || h.Status.Err != nil {
			t.Errorf("key %q: status %s %+v, want %s", c.key, got, h.Status, c.want)
		}
	}
	// Without the status URL, the 404 makes the project DOWN.
	svc := NewService([]Project{{Name: "shop", AdminURL: cloud.URL + "/admin", APIKey: "key"}}, "test", 100)
	if h := svc.Health(context.Background(), 0, now); h.Status.String() != "DOWN" {
		t.Errorf("admin root status = %s", h.Status)
	}
}

func TestMonthStart(t *testing.T) {
	// 00:30 on Nov 1 in Prague is still October in UTC.
	prague := time.FixedZone("CET", 3600)
	got := MonthStart(time.Date(2026, 11, 1, 0, 30, 0, 0, prague))
	if want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("MonthStart = %s, want %s", got, want)
	}
}

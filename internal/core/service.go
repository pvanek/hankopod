package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pvanek/hankopod/internal/hanko"
)

const (
	// RecentWindow is how far back new registrations are counted.
	RecentWindow = 30 * 24 * time.Hour
	// ActivityWindow is the window of the login counters.
	ActivityWindow = 24 * time.Hour

	pageSize = 100
	// maxRecentPages bounds the users fetched to count registrations.
	maxRecentPages = 20
	// eventCount is how many of the latest audit logs are shown.
	eventCount = 100
	// maxMAUPages bounds the audit logs read to count monthly active users.
	maxMAUPages = 50
	// sessionWorkers is how many session requests run at once per project.
	sessionWorkers = 8
)

// MAUEventTypes are the audit logs that mean a user got a session. Hanko
// bills users who authenticated in the calendar month; a signup logs
// user_created or thirdparty_signup_succeeded and often no login_success.
var MAUEventTypes = []string{
	"login_success",
	"user_created",
	"thirdparty_signup_succeeded",
	"thirdparty_signin_succeeded",
	"token_exchange_succeeded",
}

// MonthStart is the start of t's calendar month in UTC.
func MonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Service reads all configured projects.
type Service struct {
	projects         []Project
	clients          []*hanko.Client
	sessionScanLimit int
}

func NewService(projects []Project, userAgent string, sessionScanLimit int) *Service {
	s := &Service{projects: projects, sessionScanLimit: sessionScanLimit}
	for _, p := range projects {
		c := hanko.New(p.AdminURL, p.APIKey, userAgent, nil)
		if p.StatusURL != "" {
			c.SetStatusURL(p.StatusURL)
		}
		s.clients = append(s.clients, c)
	}
	return s
}

func (s *Service) Projects() []Project { return s.projects }

// Health checks the status of project i and counts MAU and the last
// day's logins, concurrently. Each data set of the service is fetched on
// its own, so a UI loads only what it shows.
func (s *Service) Health(ctx context.Context, i int, now time.Time) Health {
	c := s.clients[i]
	h := Health{Project: s.projects[i].Name, FetchedAt: now, MAU: -1, Logins: -1, LoginFailures: -1}

	var wg sync.WaitGroup
	wg.Go(func() {
		lat, err := c.Status(ctx)
		h.Status = Status{Up: err == nil, Latency: lat, Err: err}
	})
	var loginErr, failErr, mauErr error
	wg.Go(func() { h.MAU, h.MAUExact, mauErr = monthlyActiveUsers(ctx, c, MonthStart(now)) })
	since := now.Add(-ActivityWindow)
	wg.Go(func() { h.Logins, loginErr = countEvents(ctx, c, since, "login_success") })
	wg.Go(func() { h.LoginFailures, failErr = countEvents(ctx, c, since, "login_failure") })
	wg.Wait()
	// The audit log calls usually fail alike; report one.
	for _, err := range []error{mauErr, loginErr, failErr} {
		if h.Err == nil {
			h.Err = err
		}
	}
	// The public status page does not check the API key; the audit logs do.
	h.Status.Unauthorized = unauthorized(h.Status.Err) || unauthorized(h.Err)
	return h
}

// Users counts the users of project i and fetches the newest ones, back
// to RecentWindow.
func (s *Service) Users(ctx context.Context, i int, now time.Time) UserList {
	l := UserList{Project: s.projects[i].Name, FetchedAt: now}
	l.Recent, l.Total, l.Complete, l.Err = recentUsers(ctx, s.clients[i], l.Project, now.Add(-RecentWindow))
	return l
}

// Events fetches the latest audit logs of project i.
func (s *Service) Events(ctx context.Context, i int, now time.Time) EventList {
	l := EventList{Project: s.projects[i].Name, FetchedAt: now}
	logs, _, err := s.clients[i].AuditLogs(ctx, hanko.AuditQuery{PerPage: eventCount})
	l.Err = err
	for _, lg := range logs {
		l.Events = append(l.Events, toEvent(l.Project, lg))
	}
	return l
}

// recentUsers pages through users newest first until it passes since.
func recentUsers(ctx context.Context, c *hanko.Client, project string, since time.Time) ([]User, int, bool, error) {
	var out []User
	total := -1
	for page := 1; page <= maxRecentPages; page++ {
		users, t, err := c.Users(ctx, page, pageSize, true)
		if err != nil {
			return out, total, false, err
		}
		if page == 1 {
			total = t
		}
		for _, u := range users {
			out = append(out, toUser(project, u))
		}
		if len(users) < pageSize || (len(users) > 0 && users[len(users)-1].CreatedAt.Before(since)) {
			return out, total, true, nil
		}
	}
	return out, total, false, nil
}

func unauthorized(err error) bool {
	var apiErr *hanko.APIError
	return errors.As(err, &apiErr) && apiErr.Unauthorized()
}

// monthlyActiveUsers counts the distinct actors of MAUEventTypes logs
// since start.
func monthlyActiveUsers(ctx context.Context, c *hanko.Client, start time.Time) (int, bool, error) {
	users := map[string]bool{}
	for page := 1; page <= maxMAUPages; page++ {
		logs, _, err := c.AuditLogs(ctx, hanko.AuditQuery{Page: page, PerPage: pageSize, Start: start, Types: MAUEventTypes})
		if err != nil {
			return -1, false, err
		}
		for _, l := range logs {
			if l.ActorUserID != "" {
				users[l.ActorUserID] = true
			}
		}
		if len(logs) < pageSize {
			return len(users), true, nil
		}
	}
	return len(users), false, nil
}

func countEvents(ctx context.Context, c *hanko.Client, since time.Time, typ string) (int, error) {
	logs, total, err := c.AuditLogs(ctx, hanko.AuditQuery{PerPage: 1, Start: since, Types: []string{typ}})
	if err != nil {
		return -1, err
	}
	if total < 0 {
		return len(logs), nil
	}
	return total, nil
}

// Sessions lists the active sessions of project i. Hanko only lists
// sessions per user, so this asks up to the scan limit of users, oldest
// accounts first.
func (s *Service) Sessions(ctx context.Context, i int, now time.Time) SessionStats {
	c, name := s.clients[i], s.projects[i].Name
	st := SessionStats{Project: name, FetchedAt: now}

	var users []User
	for page := 1; len(users) < s.sessionScanLimit; page++ {
		batch, total, err := c.Users(ctx, page, pageSize, false)
		if err != nil {
			st.Err = err
			return st
		}
		if page == 1 {
			st.UsersTotal = total
		}
		for _, u := range batch {
			users = append(users, toUser(name, u))
		}
		if len(batch) < pageSize {
			break
		}
	}
	if len(users) > s.sessionScanLimit {
		users = users[:s.sessionScanLimit]
	}
	if st.UsersTotal < len(users) {
		st.UsersTotal = len(users)
	}
	st.UsersScanned = len(users)

	var (
		mu       sync.Mutex
		firstErr error
		jobs     = make(chan User)
		wg       sync.WaitGroup
	)
	for range sessionWorkers {
		wg.Go(func() {
			for u := range jobs {
				sessions, err := c.Sessions(ctx, u.ID)
				mu.Lock()
				if err != nil {
					st.Failed++
					if firstErr == nil {
						firstErr = fmt.Errorf("sessions of %s: %w", u.Label(), err)
					}
				}
				for _, se := range sessions {
					if se.ExpiresAt != nil && !se.ExpiresAt.After(now) {
						continue
					}
					st.Active = append(st.Active, toSession(name, u, se))
				}
				mu.Unlock()
			}
		})
	}
	for _, u := range users {
		if ctx.Err() != nil {
			break
		}
		jobs <- u
	}
	close(jobs)
	wg.Wait()
	if st.Failed > 0 {
		st.Err = firstErr
	}
	sort.Slice(st.Active, func(a, b int) bool { return st.Active[a].LastUsed.After(st.Active[b].LastUsed) })
	return st
}

func toUser(project string, u hanko.User) User {
	out := User{ID: u.ID, Project: project, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
	for _, e := range u.Emails {
		if out.Email == "" || e.IsPrimary {
			out.Email, out.Verified = e.Address, e.IsVerified
		}
		out.Emails = append(out.Emails, Email{Address: e.Address, Verified: e.IsVerified, Primary: e.IsPrimary})
	}
	if u.Username != nil {
		out.Username = u.Username.Username
	}
	for _, w := range u.WebauthnCredentials {
		pk := Passkey{ID: w.ID, Name: w.Name, CreatedAt: w.CreatedAt, MFAOnly: w.MFAOnly}
		if w.LastUsedAt != nil {
			pk.LastUsed = *w.LastUsedAt
			if pk.LastUsed.After(out.LastPasskeyUse) {
				out.LastPasskeyUse = pk.LastUsed
			}
		}
		out.Passkeys = append(out.Passkeys, pk)
	}
	return out
}

func toSession(project string, u User, s hanko.Session) Session {
	out := Session{
		ID: s.ID, Project: project, UserID: u.ID, User: u.Label(),
		IP: s.IPAddress, UserAgent: s.UserAgent, CreatedAt: s.CreatedAt, LastUsed: s.LastUsed,
	}
	if s.ExpiresAt != nil {
		out.ExpiresAt = *s.ExpiresAt
	}
	return out
}

func toEvent(project string, l hanko.AuditLog) Event {
	return Event{
		ID: l.ID, Project: project, Type: l.Type, Error: l.Error, Email: l.ActorEmail,
		UserID: l.ActorUserID, IP: l.SourceIP, UserAgent: l.UserAgent, RequestID: l.HTTPRequestID,
		CreatedAt: l.CreatedAt,
	}
}

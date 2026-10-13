package core

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/pvanek/hankopod/internal/hanko"
)

// Granularity is the length of the periods of Stats.
type Granularity int

const (
	Daily Granularity = iota
	Weekly
	Monthly
	Yearly
	GranularityCount
)

// periodCounts is how many periods Stats covers, per granularity.
var periodCounts = [GranularityCount]int{Daily: 30, Weekly: 26, Monthly: 12, Yearly: 5}

// maxStatsPages bounds the audit logs read to count active users; a
// variable for tests.
var maxStatsPages = 200

func (g Granularity) String() string {
	return [GranularityCount]string{"day", "week", "month", "year"}[g]
}

// Start is the start of the period containing t, in UTC. Weeks start on
// Monday.
func (g Granularity) Start(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	switch g {
	case Weekly:
		return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	case Monthly:
		return MonthStart(t)
	case Yearly:
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return day
}

// add moves the period start t by n periods.
func (g Granularity) add(t time.Time, n int) time.Time {
	switch g {
	case Weekly:
		return t.AddDate(0, 0, 7*n)
	case Monthly:
		return t.AddDate(0, n, 0)
	case Yearly:
		return t.AddDate(n, 0, 0)
	}
	return t.AddDate(0, 0, n)
}

// Next is the start of the period after the one starting at start.
func (g Granularity) Next(start time.Time) time.Time { return g.add(start, 1) }

// PeriodStarts are the starts of the periods Stats covers at now, newest
// first.
func (g Granularity) PeriodStarts(now time.Time) []time.Time {
	cur := g.Start(now)
	out := make([]time.Time, periodCounts[g])
	for k := range out {
		out[k] = g.add(cur, -k)
	}
	return out
}

// Period holds the figures of one period. Counts are -1 when unknown.
type Period struct {
	// End is the next period's start; for the current period, the fetch
	// time.
	Start, End time.Time
	// Registered counts the users created in the period, Users the users
	// at its end (both from the user list, so deleted users are missing).
	Registered, Users int
	// Active counts the distinct users with a MAUEventTypes audit log in
	// the period: daily, weekly, monthly or yearly active users.
	// ActiveExact is false when it is a lower bound.
	Active      int
	ActiveExact bool
	// Logins and LoginFailures count login_success and login_failure logs.
	Logins, LoginFailures int
}

// Stats is the user analytics of one project, period by period.
type Stats struct {
	Project     string
	FetchedAt   time.Time
	Granularity Granularity
	Periods     []Period // newest first
	Err         error
}

// Stats computes the analytics of project i for the last periods of g.
// Registrations come from the user list, activity from the audit logs.
func (s *Service) Stats(ctx context.Context, i int, now time.Time, g Granularity) Stats {
	c := s.clients[i]
	st := Stats{Project: s.projects[i].Name, FetchedAt: now, Granularity: g}
	for _, start := range g.PeriodStarts(now) {
		end := now
		if len(st.Periods) > 0 {
			end = st.Periods[len(st.Periods)-1].Start
		}
		st.Periods = append(st.Periods, Period{Start: start, End: end,
			Registered: -1, Users: -1, Active: -1, Logins: -1, LoginFailures: -1})
	}

	// Each part fills its own copy of the periods; merged below.
	var (
		wg     sync.WaitGroup
		errs   = make([]error, 3)
		reg    = slices.Clone(st.Periods)
		active = slices.Clone(st.Periods)
		logins = slices.Clone(st.Periods)
	)
	wg.Go(func() { errs[0] = registrations(ctx, c, reg) })
	wg.Go(func() { errs[1] = activeUsers(ctx, c, active) })
	wg.Go(func() { errs[2] = loginCounts(ctx, c, logins) })
	wg.Wait()
	for k := range st.Periods {
		p := &st.Periods[k]
		p.Registered, p.Users = reg[k].Registered, reg[k].Users
		p.Active, p.ActiveExact = active[k].Active, active[k].ActiveExact
		p.Logins, p.LoginFailures = logins[k].Logins, logins[k].LoginFailures
	}
	for _, err := range errs {
		if st.Err == nil {
			st.Err = err
		}
	}
	return st
}

// registrations fills Registered and Users from the users created at or
// after each period boundary.
func registrations(ctx context.Context, c *hanko.Client, periods []Period) error {
	p, err := newUserPager(ctx, c)
	if err != nil {
		return err
	}
	after := func(t time.Time) (int, error) { return p.createdSince(t) }
	endCount, err := after(periods[0].End)
	if err != nil {
		return err
	}
	for k := range periods {
		startCount, err := after(periods[k].Start)
		if err != nil {
			return err
		}
		periods[k].Registered = startCount - endCount
		periods[k].Users = p.total - endCount
		endCount = startCount
	}
	return nil
}

// userPager finds how many users were created since a time by binary
// search over the pages of users sorted newest first, so it reads only a
// few pages even of a large project. Pages are cached across searches.
type userPager struct {
	ctx   context.Context
	c     *hanko.Client
	total int
	pages map[int][]hanko.User
}

func newUserPager(ctx context.Context, c *hanko.Client) (*userPager, error) {
	p := &userPager{ctx: ctx, c: c, pages: map[int][]hanko.User{}}
	users, total, err := c.Users(ctx, 1, pageSize, true)
	if err != nil {
		return nil, err
	}
	p.pages[1], p.total = users, max(total, len(users))
	return p, nil
}

func (p *userPager) page(n int) ([]hanko.User, error) {
	if users, ok := p.pages[n]; ok {
		return users, nil
	}
	users, _, err := p.c.Users(p.ctx, n, pageSize, true)
	if err != nil {
		return nil, err
	}
	p.pages[n] = users
	return users, nil
}

// createdSince counts the users created at or after t.
func (p *userPager) createdSince(t time.Time) (int, error) {
	pages := (p.total + pageSize - 1) / pageSize
	// Find the first page whose oldest user is older than t.
	lo, hi := 1, pages+1
	for lo < hi {
		mid := (lo + hi) / 2
		users, err := p.page(mid)
		if err != nil {
			return 0, err
		}
		if len(users) == 0 || users[len(users)-1].CreatedAt.Before(t) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo > pages {
		return p.total, nil
	}
	users, err := p.page(lo)
	if err != nil {
		return 0, err
	}
	n := 0
	for n < len(users) && !users[n].CreatedAt.Before(t) {
		n++
	}
	return (lo-1)*pageSize + n, nil
}

// activeUsers fills Active from the MAUEventTypes logs since the oldest
// period's start, reading up to maxStatsPages pages concurrently. When the
// logs are cut off, periods partly covered are lower bounds and older ones
// unknown.
func activeUsers(ctx context.Context, c *hanko.Client, periods []Period) error {
	q := hanko.AuditQuery{PerPage: pageSize, Start: periods[len(periods)-1].Start, Types: MAUEventTypes}
	q.Page = 1
	first, total, err := c.AuditLogs(ctx, q)
	if err != nil {
		return err
	}
	pages := 1
	if total > len(first) {
		pages = (total + pageSize - 1) / pageSize
	}
	// Without a total count only the first page is read.
	complete := pages <= maxStatsPages && (total >= 0 || len(first) < pageSize)
	pages = min(pages, maxStatsPages)

	results := make([][]hanko.AuditLog, pages)
	results[0] = first
	var firstErr error
	var mu sync.Mutex
	parallel(pages-1, func(j int) {
		pq := q
		pq.Page = j + 2
		logs, _, err := c.AuditLogs(ctx, pq)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		results[j+1] = logs
	})
	if firstErr != nil {
		return firstErr
	}

	// The logs are newest first, so those read reach back to the oldest one.
	oldest := periods[0].End
	actors := make([]map[string]bool, len(periods))
	for k := range actors {
		actors[k] = map[string]bool{}
	}
	for _, logs := range results {
		for _, l := range logs {
			if l.CreatedAt.Before(oldest) {
				oldest = l.CreatedAt
			}
			if l.ActorUserID == "" {
				continue
			}
			for k, p := range periods {
				if !l.CreatedAt.Before(p.Start) && (k == 0 || l.CreatedAt.Before(p.End)) {
					actors[k][l.ActorUserID] = true
					break
				}
			}
		}
	}
	for k := range periods {
		switch p := &periods[k]; {
		case complete || p.Start.After(oldest):
			p.Active, p.ActiveExact = len(actors[k]), true
		case p.End.After(oldest):
			p.Active = len(actors[k])
		}
	}
	return nil
}

// loginCounts fills Logins and LoginFailures with one count query per
// period and type.
func loginCounts(ctx context.Context, c *hanko.Client, periods []Period) error {
	types := []string{"login_success", "login_failure"}
	var (
		mu       sync.Mutex
		firstErr error
	)
	parallel(len(periods)*len(types), func(j int) {
		p, typ := &periods[j/len(types)], types[j%len(types)]
		q := hanko.AuditQuery{PerPage: 1, Start: p.Start, End: p.End, Types: []string{typ}}
		logs, total, err := c.AuditLogs(ctx, q)
		if total < 0 {
			total = len(logs)
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case typ == "login_success":
			p.Logins = total
		default:
			p.LoginFailures = total
		}
	})
	return firstErr
}

// parallel calls f(0) … f(n-1) on up to sessionWorkers goroutines.
func parallel(n int, f func(int)) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(n, sessionWorkers) {
		wg.Go(func() {
			for j := range jobs {
				f(j)
			}
		})
	}
	for j := range n {
		jobs <- j
	}
	close(jobs)
	wg.Wait()
}

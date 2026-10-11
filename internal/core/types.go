// Package core is the UI-agnostic service over the Hanko Admin API. It
// fetches one snapshot per project and exposes plain domain types.
package core

import "time"

// AllProjects is the scope that selects every project.
const AllProjects = ""

// Project is one configured Hanko project.
type Project struct {
	Name     string
	AdminURL string
	// StatusURL is the public status page; empty means the Admin API root.
	StatusURL string
	APIKey    string
}

// Status is the result of the Admin API status check.
type Status struct {
	Up bool
	// Unauthorized means the Admin API rejected the API key.
	Unauthorized bool
	Latency      time.Duration
	Err          error
}

// String is "up", "DOWN" or "AUTH" (bad API key).
func (s Status) String() string {
	switch {
	case s.Unauthorized:
		return "AUTH"
	case s.Up:
		return "up"
	}
	return "DOWN"
}

type User struct {
	ID        string
	Project   string
	Email     string // primary email, or the first one
	Verified  bool
	Emails    []Email
	Username  string
	CreatedAt time.Time
	UpdatedAt time.Time
	Passkeys  []Passkey
	// LastPasskeyUse is the latest use of any of the user's passkeys.
	LastPasskeyUse time.Time
}

type Email struct {
	Address  string
	Verified bool
	Primary  bool
}

// Passkey is a WebAuthn credential.
type Passkey struct {
	ID        string
	Name      string
	CreatedAt time.Time
	LastUsed  time.Time // zero if never used
	MFAOnly   bool      // security key used as a second factor only
}

// Label is the most readable identifier of the user.
func (u User) Label() string {
	switch {
	case u.Email != "":
		return u.Email
	case u.Username != "":
		return u.Username
	}
	return u.ID
}

type Session struct {
	ID        string
	Project   string
	UserID    string
	User      string // label of the user
	IP        string
	UserAgent string
	CreatedAt time.Time
	LastUsed  time.Time
	ExpiresAt time.Time // zero when unknown
}

// Event is one audit log entry.
type Event struct {
	ID        string
	Project   string
	Type      string
	Error     string
	Email     string
	UserID    string
	IP        string
	UserAgent string
	RequestID string
	CreatedAt time.Time
}

// Failed reports whether the event records a failure.
func (e Event) Failed() bool {
	return e.Error != "" || hasSuffix(e.Type, "_failed") || hasSuffix(e.Type, "_failure")
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// Health is the status of one project and its headline activity counts.
type Health struct {
	Project   string
	FetchedAt time.Time
	Status    Status

	// MAU counts the distinct users with a session-creating audit log
	// (see MAUEventTypes) since the start of the calendar month (UTC), -1
	// if unknown. MAUExact is false when it is a lower bound.
	MAU      int
	MAUExact bool

	// Logins and LoginFailures count login_success and login_failure audit
	// logs within the last 24 hours, -1 if unknown.
	Logins, LoginFailures int
	// Err is the first failure of the audit log queries behind the counts.
	Err error
}

// UserList is the user count and newest users of one project.
type UserList struct {
	Project   string
	FetchedAt time.Time
	// Total is the number of users, -1 if unknown.
	Total int
	// Recent holds the newest users, newest first: at least one page, and
	// every user created within RecentWindow unless Complete is false.
	Recent   []User
	Complete bool
	Err      error
}

// NewSince counts the users created after t. exact is false when the
// fetched users do not reach back to t, so n is only a lower bound.
func (l UserList) NewSince(t time.Time) (n int, exact bool) {
	for _, u := range l.Recent {
		if u.CreatedAt.After(t) {
			n++
		}
	}
	if l.Complete {
		return n, true
	}
	if len(l.Recent) == 0 {
		return 0, false
	}
	return n, !l.Recent[len(l.Recent)-1].CreatedAt.After(t)
}

// LastSignup is the creation time of the newest user, zero if none.
func (l UserList) LastSignup() time.Time {
	if len(l.Recent) == 0 {
		return time.Time{}
	}
	return l.Recent[0].CreatedAt
}

// EventList is the latest audit logs of one project, newest first.
type EventList struct {
	Project   string
	FetchedAt time.Time
	Events    []Event
	Err       error
}

// SessionStats is the result of the (expensive) session scan of a project.
type SessionStats struct {
	Project   string
	FetchedAt time.Time
	// Active holds the unexpired sessions, most recently used first.
	Active []Session
	// UsersScanned of UsersTotal users were asked for their sessions.
	UsersScanned, UsersTotal int
	// Failed counts users whose sessions could not be read.
	Failed int
	Err    error
}

// Complete reports whether every user was scanned successfully.
func (s SessionStats) Complete() bool {
	return s.Err == nil && s.Failed == 0 && s.UsersScanned >= s.UsersTotal
}

// ActiveUsers counts the users with at least one active session.
func (s SessionStats) ActiveUsers() int {
	seen := map[string]bool{}
	for _, se := range s.Active {
		seen[se.UserID] = true
	}
	return len(seen)
}

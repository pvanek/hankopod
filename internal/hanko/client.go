// Package hanko is a minimal read-only client for the Hanko Admin API
// (https://docs.hanko.io/api-reference/admin). It exposes the wire types
// as returned by the API; internal/core turns them into domain types.
package hanko

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CloudAdminURL is the Admin API base URL of a Hanko Cloud project.
func CloudAdminURL(tenantID string) string {
	return CloudPublicURL(tenantID) + "/admin"
}

// CloudPublicURL is the public API URL of a Hanko Cloud project. Its root
// serves the status page; Hanko Cloud does not serve it under /admin.
func CloudPublicURL(tenantID string) string {
	return "https://" + tenantID + ".hanko.io"
}

// Client talks to one Hanko project's Admin API.
type Client struct {
	base      string
	statusURL string
	apiKey    string
	userAgent string
	http      *http.Client
}

// New returns a client for the Admin API at baseURL. apiKey may be empty
// for self-hosted instances protected by other means.
func New(baseURL, apiKey, userAgent string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, userAgent: userAgent, http: hc}
}

// SetStatusURL makes Status check url (a public, unauthenticated status
// page) instead of the Admin API root.
func (c *Client) SetStatusURL(url string) { c.statusURL = url }

// APIError is a non-2xx response.
type APIError struct {
	Status  int
	Message string
}

// Unauthorized reports whether the API key was rejected.
func (e *APIError) Unauthorized() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Unauthorized() {
		msg += " (check the project's API key)"
	}
	return fmt.Sprintf("hanko: %d %s", e.Status, msg)
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return c.do(ctx, u, true)
}

// do sends a GET to u, with the API key when auth is set.
func (c *Client) do(ctx context.Context, u string, auth bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if auth && c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// Drop the full URL (with query) from transport errors.
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, fmt.Errorf("%s: %w", req.URL.Host, ue.Err)
		}
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		apiErr := &APIError{Status: resp.StatusCode}
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &e) == nil {
			apiErr.Message = e.Message
		}
		return nil, apiErr
	}
	return resp, nil
}

// getList decodes a JSON array response and its X-Total-Count header
// (-1 when the header is missing).
func getList[T any](ctx context.Context, c *Client, path string, q url.Values) ([]T, int, error) {
	resp, err := c.get(ctx, path, q)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out []T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("decoding %s: %w", path, err)
	}
	total := -1
	if h := resp.Header.Get("X-Total-Count"); h != "" {
		if n, err := strconv.Atoi(h); err == nil {
			total = n
		}
	}
	return out, total, nil
}

// Status calls the status page, which fails when the project's database
// is unreachable, and reports how long the call took.
func (c *Client) Status(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	var (
		resp *http.Response
		err  error
	)
	if c.statusURL != "" {
		resp, err = c.do(ctx, c.statusURL, false)
	} else {
		resp, err = c.get(ctx, "/", nil)
	}
	if err != nil {
		return time.Since(start), err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return time.Since(start), nil
}

// Users returns one page of users sorted by creation time and the total
// number of users.
func (c *Client) Users(ctx context.Context, page, perPage int, newestFirst bool) ([]User, int, error) {
	dir := "asc"
	if newestFirst {
		dir = "desc"
	}
	q := url.Values{
		"page":           {strconv.Itoa(page)},
		"per_page":       {strconv.Itoa(perPage)},
		"sort_direction": {dir},
	}
	return getList[User](ctx, c, "/users", q)
}

// Sessions returns the active sessions of a user.
func (c *Client) Sessions(ctx context.Context, userID string) ([]Session, error) {
	s, _, err := getList[Session](ctx, c, "/users/"+url.PathEscape(userID)+"/sessions", nil)
	return s, err
}

// AuditQuery filters audit logs. Zero values are not sent.
type AuditQuery struct {
	Page, PerPage int
	Start, End    time.Time
	Types         []string
}

// AuditLogs returns one page of audit logs, newest first, and the total
// number of logs matching the query.
func (c *Client) AuditLogs(ctx context.Context, aq AuditQuery) ([]AuditLog, int, error) {
	q := url.Values{}
	if aq.Page > 0 {
		q.Set("page", strconv.Itoa(aq.Page))
	}
	if aq.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(aq.PerPage))
	}
	if !aq.Start.IsZero() {
		q.Set("start_time", aq.Start.UTC().Format(time.RFC3339))
	}
	if !aq.End.IsZero() {
		q.Set("end_time", aq.End.UTC().Format(time.RFC3339))
	}
	for _, t := range aq.Types {
		q.Add("type", t)
	}
	return getList[AuditLog](ctx, c, "/audit_logs", q)
}

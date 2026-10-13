# hankopod

A terminal admin console for your [Hanko](https://hanko.io) projects: service
status, user counts, new registrations, active sessions and login activity,
across all projects at once. Runs on Linux and in Termux on Android.

> Unofficial project, not affiliated with or endorsed by Hanko.

## Tabs

| Tab | Data |
|---|---|
| Overview | Per project: status and latency, users, monthly active users (MAU), new registrations in the last 24h / 7d / 30d, last signup, active sessions, logins and failed logins in the last 24h. Totals for the scope on top |
| Users | Newest users (at least the last 30 days): email or username, registration time, email verified, passkeys, last passkey use |
| Sessions | Active sessions: user, last used, created, expiry, IP, user agent |
| Activity | Latest 100 audit log entries per project; failures are marked `✗` |

Press `enter` on any row for its detail, `esc` to go back:

| Row | Detail |
|---|---|
| Project | Admin API URL, status and error, user and registration figures, newest users, session scan results, recent failed logins |
| User | IDs, all emails (primary, verified), passkeys with last use, the user's active sessions and recent audit log entries |
| Session | Times, expiry, IP, full user agent, the user's other sessions |
| Event | Type, error, actor, IP, user agent, request ID |

Switch between all projects and a single one with `p` / `P`.

Data loads lazily: a screen loads only what it shows, for the projects in
view, the first time you open it. Nothing else is fetched at start-up.

| Screen | Loads |
|---|---|
| Overview | status, MAU and login counts; users; session scan |
| Users | users |
| Sessions | session scan (one request per user) |
| Activity | latest audit logs |
| Project / user detail | also the latest audit logs. A user detail never starts the session scan; it says so when the scan has not run |

`r` reloads the visible screen only. Auto-refresh is set per tab with `a` and
reloads the visible screen's data once it is 30 minutes old. It is on by
default only for the Overview. The bottom bar shows the state (`a auto 30m` /
`a auto off`) and when the oldest data on screen was fetched.

## Install

Download a binary from the GitHub release (Linux, Android/Termux, Windows,
macOS); or:

```sh
go install github.com/pvanek/hankopod/cmd/hankopod@latest
```

In Termux: `pkg install golang`, then the same command.

## Configuration

`~/.config/hankopod/config.json` (or `$HANKOPOD_CONFIG`, or `-config PATH`):

```json
{
  "projects": [
    { "name": "shop",  "tenant_id": "<tenant uuid>", "api_key": "<admin api key>" },
    { "name": "blog",  "tenant_id": "<tenant uuid>", "api_key_env": "HANKO_BLOG_KEY" },
    { "name": "local", "admin_url": "http://localhost:8001", "api_key": "" }
  ],
  "session_scan_limit": 500
}
```

- `tenant_id` gives the Hanko Cloud Admin API at `https://<tenant_id>.hanko.io/admin`.
  Use `admin_url` instead for self-hosted Hanko, or to override. `status_url`
  optionally points the status check elsewhere (e.g. a self-hosted public API).
- Create an API key in the Hanko Cloud console under the project's Admin API settings.
  Keep the file private (`chmod 600`); hankopod warns if it is not.
  `api_key_env` reads the key from an environment variable instead.

hankopod only reads (`GET`) and never changes anything. A section whose call
fails shows the error and the rest keep working; a rejected API key shows the
project as `AUTH` instead of `up` / `DOWN`.

## How the numbers are made

| Figure | Source |
|---|---|
| Status | The status page (fails when the project's database is unreachable) and its latency. For Hanko Cloud that is the public API root `https://<tenant_id>.hanko.io/`, since Cloud does not serve it under `/admin`; otherwise the Admin API root. Override with `status_url`. A rejected API key shows as `AUTH` |
| Users | `X-Total-Count` of `GET /users` |
| MAU | Distinct users with a `login_success`, `user_created`, `thirdparty_signup_succeeded`, `thirdparty_signin_succeeded` or `token_exchange_succeeded` audit log since the 1st of the month (UTC). This follows Hanko's billing definition (users who authenticated in the calendar month), but it is derived from audit logs, not read from Hanko billing. Signups are included because they often log no `login_success`. Reads up to 5000 logs per project; beyond that it shows `≥N` |
| New 24h / 7d / 30d, last signup | `GET /users` newest first, paged back 30 days (up to 2000 users; beyond that counts show as `≥N`) |
| Logins / failed 24h | `X-Total-Count` of `GET /audit_logs` with type `login_success` / `login_failure` |
| Sessions | Hanko lists sessions only per user, so hankopod calls `GET /users/{id}/sessions` for up to `session_scan_limit` users per project (oldest accounts first) and counts the unexpired ones. If not every user was scanned, the count shows as `≥N` |

## One-shot summary

```sh
hankopod status              # table of all projects, errors on stderr
hankopod -sessions status    # include the session scan
```

## Keys

| Key | Action |
|---|---|
| `F1` / `?` | show all keys |
| `←` `→` / `tab` / `0`–`3` | switch tab |
| `p` / `P` | next / previous project |
| `↑` `↓` | move in table / scroll detail |
| `enter` | open detail of the selected row |
| `esc` / `←` | back from detail |
| `r` | reload the visible screen |
| `a` | toggle auto-refresh (every 30 min) of the current tab; on by default only for Overview |
| `q` | quit |

## Architecture

```
cmd/hankopod        entry point, flags, status command
internal/config     config file loading and validation
internal/hanko      minimal read-only Admin API client (wire types)
internal/core       UI-agnostic service: per-project snapshots and session scans
internal/tui        Bubble Tea front end
internal/platform   OS workarounds (DNS on Android)
```

Same layout and libraries as [scwpod](https://github.com/pvanek/scwpod).

## Development

```sh
make            # build for this machine into bin/
make test       # go vet + tests
make help       # list all targets
```

Cross-compiled, static binaries go to `dist/`:

| Target | Output |
|---|---|
| `make linux` | `hankopod-linux-amd64`, `hankopod-linux-arm64` |
| `make termux` | `hankopod-termux-arm64` (Android) |
| `make windows` | `hankopod-windows-amd64.exe`, `hankopod-windows-arm64.exe` |
| `make osx` | `hankopod-macos-amd64`, `hankopod-macos-arm64` |
| `make dist` | all of the above plus `SHA256SUMS` |

The version comes from `git describe`, so tag releases (`git tag v0.1.0`).

### Releasing

Pushing a version tag runs `.github/workflows/release.yml`: tests, `make dist`
and a GitHub Release with every binary and `SHA256SUMS` attached.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Tags with a suffix (`v0.2.0-rc1`) are published as pre-releases.

On GitLab, `.gitlab-ci.yml` does the same: every pipeline tests and keeps the
cross-compiled binaries as job artifacts for a week, and a `v*` tag uploads
them to the project's Generic Package Registry and creates a GitLab Release
linking to them. The release job needs GitLab 18 or newer; on older
instances use the `registry.gitlab.com/gitlab-org/release-cli:latest` image.

From a GitHub release in Termux:

```sh
curl -LO https://github.com/pvanek/hankopod/releases/latest/download/hankopod-termux-arm64
chmod +x hankopod-termux-arm64 && mv hankopod-termux-arm64 $PREFIX/bin/hankopod
```

## License

MIT

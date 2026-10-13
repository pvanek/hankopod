# hankopod

A terminal admin console for your [Hanko](https://hanko.io) projects: service
status, user counts, new registrations, active sessions and login activity,
across all projects at once. Runs on Linux and in Termux on Android.

> Unofficial project, not affiliated with or endorsed by Hanko.

## Tabs

| Tab | Data |
|---|---|
| Overview | Per project: status and latency, users, monthly active users (MAU), new registrations in the last 24h / 7d / 30d, last signup, active sessions, logins and failed logins in the last 24h. Totals for the scope on top |
| Analytics | Per day, week, month or year (`d` / `w` / `m` / `y`, monthly by default): registered users, users at the period's end, active users (DAU / WAU / MAU / YAU), their share of all users, logins and failed logins. The last 30 days, 26 weeks, 12 months or 5 years, in UTC |
| Sessions | Active sessions: user, last used, created, expiry, IP, user agent |
| Activity | Latest 100 audit log entries per project; failures are marked `✗` |

Press `enter` on any row for its detail, `esc` to go back:

| Row | Detail |
|---|---|
| Project | Admin API URL, status and error, user and registration figures, newest users, session scan results, recent failed logins |
| Period | Its figures for the scope and for each project |
| Session | Times, expiry, IP, full user agent, the user's other sessions |
| Event | Type, error, actor, IP, user agent, request ID |

Switch between all projects and a single one with `p` / `P`.

Data loads lazily: a screen loads only what it shows, for the projects in
view, the first time you open it. Nothing else is fetched at start-up.

| Screen | Loads |
|---|---|
| Overview | status, MAU and login counts; users; session scan |
| Analytics | analytics of the selected granularity (each granularity loads on first use) |
| Sessions | session scan (one request per user) |
| Activity | latest audit logs |
| Project detail | also the latest audit logs |

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

hankopod reads one JSON file. It looks for it in this order:

1. `-config PATH`
2. `$HANKOPOD_CONFIG`
3. `hankopod/config.json` in the user configuration directory: `~/.config` on
   Linux and Termux, `~/Library/Application Support` on macOS, `%AppData%` on
   Windows

```json
{
  "projects": [
    { "name": "shop", "tenant_id": "<tenant uuid>", "api_key": "<admin api key>" },
    { "name": "blog", "tenant_id": "<tenant uuid>", "api_key_env": "HANKO_BLOG_KEY" },
    { "name": "local", "admin_url": "http://localhost:8001", "status_url": "http://localhost:8000/" }
  ],
  "session_scan_limit": 500
}
```

### Top level

| Attribute | Required | Description |
|---|---|---|
| `projects` | yes | List of projects to watch, at least one. They appear in this order |
| `session_scan_limit` | no | How many users per project the session scan asks for their sessions (one request per user). Default `500`; `0` or less also means the default. Projects with more users show the session counts as `≥N` |

### Project

| Attribute | Required | Description |
|---|---|---|
| `name` | one of `name`, `tenant_id` | Name shown in the UI; must be unique. Defaults to `tenant_id` |
| `tenant_id` | one of `tenant_id`, `admin_url` | Hanko Cloud project (tenant) ID. Sets the Admin API to `https://<tenant_id>.hanko.io/admin` and the status page to `https://<tenant_id>.hanko.io/` |
| `admin_url` | one of `tenant_id`, `admin_url` | Admin API base URL, without a trailing `/users`. Required for self-hosted Hanko; with `tenant_id` it overrides the Cloud URL |
| `status_url` | no | Page whose response is the project's status and latency. It is called without the API key, so it should be public. Default: the public API root for `tenant_id`, otherwise the Admin API root (`admin_url` + `/`, with the API key) |
| `api_key` | no | Admin API key, sent as `Authorization: Bearer <key>`. Leave it out when the Admin API needs none; then no `Authorization` header is sent |
| `api_key_env` | no | Name of an environment variable holding the API key. Takes precedence over `api_key`; hankopod refuses to start if the variable is empty |

For Hanko Cloud, create an API key in the Hanko Cloud console under the
project's Admin API settings. The file holds API keys, so keep it private
(`chmod 600`); hankopod warns if other users can read it.

### Self-hosted Hanko

A self-hosted Hanko backend runs two servers: the public API (port `8000` by
default, the one your frontend talks to) and the Admin API (port `8001` by
default). Point `admin_url` at the root of the Admin API server, with no
`/admin` suffix (that prefix exists only on Hanko Cloud), and leave out
`tenant_id`:

```json
{
  "projects": [
    {
      "name": "prod",
      "admin_url": "http://localhost:8001",
      "status_url": "https://auth.example.com/"
    }
  ]
}
```

- **API key.** The self-hosted Admin API has no API key of its own: Hanko
  expects you to keep it off the internet. Leave out `api_key`. If you put the
  Admin API behind a reverse proxy that checks a bearer token, set that token
  as `api_key` (or `api_key_env`).
- **Reaching the Admin API.** Do not expose port `8001` publicly. From another
  machine, use an SSH tunnel and keep `admin_url` at `http://localhost:8001`:

  ```sh
  ssh -N -L 8001:localhost:8001 user@hanko-host
  ```

  In Kubernetes, `kubectl port-forward svc/<hanko-service> 8001:8001` does the
  same.
- **Status.** Without `status_url`, the status comes from the Admin API root.
  Set `status_url` to the public API root (e.g. `https://auth.example.com/`)
  to check the side your users reach.
- **Audit logs.** MAU, login counts, Analytics active users and logins, and
  the Activity tab come from Hanko's audit logs. Hanko only prints them to
  stdout by default; store them in the database so hankopod can read them:

  ```yaml
  audit_log:
    storage:
      enabled: true
    retention: 720h   # how long logs are kept; the default is 30 days
  ```

  Without storage those figures stay at zero. Logs older than `retention` are
  deleted, so Analytics shows active users and logins only that far back.
  Raise it (e.g. `8760h` for a year) for the weekly, monthly and yearly
  views. Registrations and user counts come from the user list and are not
  limited by it.

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
| Analytics: registered, users | `GET /users` newest first. hankopod binary-searches the pages for each period boundary, so it reads only a few pages even of a large project. Deleted users are not counted |
| Analytics: active users | Distinct users with an MAU audit log (as above) in the period. Reads up to 20000 logs since the start of the oldest period; beyond that, the oldest periods show as `≥N` or `–`. Audit logs Hanko no longer keeps are not counted |
| Analytics: logins, failed | `X-Total-Count` of `GET /audit_logs` per period with type `login_success` / `login_failure` |
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
| `d` / `w` / `m` / `y` | Analytics per day / week / month / year |
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

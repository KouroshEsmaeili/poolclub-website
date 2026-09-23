# Pool Club Website

A Go web application for managing a fictional swimming club. It includes server-rendered pages, authentication, wallet operations, bookings, memberships, classes, event registration, and live ranking data.

The repository is self-contained for local development: tracked demo configuration lives in `data/`, database schema changes live in `migrations/`, and the UI is rendered with Go `html/template` templates.

## Stack

- Go 1.27.1
- `net/http` and `html/template`
- GORM
- SQLite via `github.com/ncruces/go-sqlite3`
- bcrypt authentication with legacy Werkzeug-scrypt verification for migrated accounts
- Plain JavaScript and compiled CSS

## Run locally

From the repository root, initialize the database and start the server:

```sh
go run ./cmd/migrate
go run ./cmd/server
```

Open `http://localhost:8080`.

The server intentionally does not run migrations automatically. Run `go run ./cmd/migrate` whenever new tracked migrations are added. Applied migration versions are recorded in `schema_migrations`, so the command is safe to run repeatedly.

## Configuration

Configuration is read directly from environment variables. `.env.example` documents the supported values; the application does not load `.env` files itself.

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | `poolclub.db` | SQLite database path or connection string |
| `SESSION_TTL` | `24h` | In-memory login-session lifetime in Go duration syntax |
| `SESSION_COOKIE_SECURE` | `false` | Set to `true` when serving over HTTPS |

PowerShell example:

```powershell
$env:DATABASE_URL = Join-Path $env:TEMP "poolclub-demo.db"
go run ./cmd/migrate
go run ./cmd/server
```

## Repository structure

```text
cmd/
  migrate/        Database migration command
  server/         HTTP server entry point
data/             Fictional demo configuration
internal/
  auth/           Authentication and sessions
  booking/        Booking rules and endpoints
  classes/        Class catalogue and enrollment
  config/         Runtime configuration
  database/       Database connection setup
  events/         Event catalogue and registration
  httpapi/        HTTP route composition
  info/           Read-only information and rankings
  membership/     Membership plans and lifecycle
  model/          Persistent models
  user/           User persistence
  wallet/         Wallet operations
  web/            Browser handlers and embedded Go templates
migrations/       Tracked SQL migrations
static/           Browser CSS and JavaScript
```

## Demo data

The files in `data/` are fictional and intended only for development and portfolio demonstration. No private production configuration or company data is required to run the project.

## Tests

Run the complete validation suite with:

```sh
go fmt ./...
go vet ./...
go test ./... -count=1
git diff --check
```

The tests cover domain behavior, HTTP APIs, browser flows, migration idempotency, schema compatibility, and fresh-clone startup using temporary databases.

## Notes

- Login sessions and flash messages are stored in process memory, so they are cleared when the server restarts.
- The stylesheet in `static/css/styles.min.css` is the checked-in compiled asset. The obsolete Node/PostCSS pipeline was removed because its source stylesheet was not present and therefore was not reproducible.

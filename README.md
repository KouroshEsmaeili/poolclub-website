# Pool Club Website

This repository contains an incremental Go migration of the Pool Club application. The Flask application remains in the repository as a behavior reference during the migration.

## Run the Go application

Prerequisites:

- the Go version declared in `go.mod`;
- a working directory at the repository root.

Initialize a local SQLite database, then start the server:

```sh
go run ./cmd/migrate
go run ./cmd/server
```

Open `http://localhost:8080`. The tracked files in `data/` contain fictional demo content, so no private configuration is needed for a fresh-clone preview.

The server intentionally does not create or migrate tables. Run `go run ./cmd/migrate` whenever tracked migrations need to be applied. The command records completed versions in `schema_migrations` and is safe to run repeatedly.

## Configuration

Configuration is read directly from environment variables. Copying `.env.example` is optional; the application does not load `.env` files itself.

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | `poolclub.db` | SQLite database path or connection string |
| `SESSION_TTL` | `24h` | In-memory login-session lifetime in Go duration syntax |
| `SESSION_COOKIE_SECURE` | `false` | Set to `true` when serving over HTTPS |

For an isolated local run on Bash:

```sh
export DATABASE_URL=/tmp/poolclub-demo.db
go run ./cmd/migrate
go run ./cmd/server
```

For PowerShell:

```powershell
$env:DATABASE_URL = Join-Path $env:TEMP "poolclub-demo.db"
go run ./cmd/migrate
go run ./cmd/server
```

The default session store is process memory, so active sessions are cleared when the server restarts.

## Frontend assets

The browser UI is rendered by Go templates in `internal/web/templates/`. The existing compiled stylesheet is tracked at `app/static/css/styles.min.css`; the old PostCSS/Node pipeline was removed because its source stylesheet was not present in the repository and therefore could not reproduce the checked-in CSS.

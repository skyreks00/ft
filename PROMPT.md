# Project: `ft` — a fast terminal client for the 42 Intra (Go + Bubble Tea)

## Context
I'm a student at 42 (Belgium campus). I want to do everything from the terminal, quickly: link a folder to a project, open/register a project, submit, request corrections, manage slots, events, friends, etc. There is no existing code: this is a greenfield project written from scratch in Go.

**Distribution goal:** other 42 students must be able to install `ft` and just run `ft login` (browser login). They must NOT create their own 42 API app or handle any client ID/secret. All users go through MY single registered 42 app.

## Stack
- Go (latest stable)
- Bubble Tea + Bubbles + Lip Gloss for the TUI (Elm architecture: Model/Update/View)
- Cobra for the direct CLI commands
- golang.org/x/oauth2 for auth
- godotenv (or equivalent) to load the `.env` file (developer use only)
- A tiny stateless token broker (Cloudflare Worker in TypeScript, free tier) that holds the client secret — see "Auth architecture"
- TOML or YAML for config
- Single static binary, `go install`-able, Makefile for build

## Two modes, one shared core
1. Direct commands for speed/scripts: `ft status`, `ft submit`...
2. Running `ft` alone opens the full-screen TUI.
Both must use the same API client package (`internal/api`).

## 42 API facts (VERIFY against https://api.intra.42.fr/apidoc — do not guess endpoints or params)
- Base URL: https://api.intra.42.fr
- OAuth2 authorization_code flow: /oauth/authorize and /oauth/token
- My app is already created on the intra. Redirect URIs (one per line): http://localhost:8080/callback, http://localhost:8081/callback, http://localhost:8082/callback
- Scopes enabled on my app: public, projects (the "Manage teams, slots and all projects related stuff" scope). Check in the docs whether other scopes are needed for a feature and tell me.
- Access tokens last ~2h → implement refresh token handling
- Rate limit: ~2 requests/second, ~1200/hour → client-side rate limiter, retry with backoff on 429, and a small on-disk/in-memory cache (TTL per resource)
- Pagination (page[number], page[size]) must be handled

## Auth architecture (IMPORTANT: end users never touch the secret)
The 42 API requires `client_secret` for the code→token exchange and for token refresh (server-side only, per the official docs). I did not find any PKCE / secret-less flow in the docs — VERIFY this; if PKCE is supported for my app type, prefer it and drop the broker. Otherwise:

- The CLI embeds only the **public** `client_id` and the broker URL (compile-time defaults via `-ldflags`, overridable with `FT_CLIENT_ID` / `FT_BROKER_URL`). It NEVER contains or sees the client secret.
- The **broker** (`broker/`, Cloudflare Worker) holds `FT_CLIENT_SECRET` as a Worker secret and exposes only:
  - `POST /token` `{code, redirect_uri}` → calls 42 `/oauth/token` with `grant_type=authorization_code` + secret, returns the token JSON
  - `POST /refresh` `{refresh_token}` → same with `grant_type=refresh_token`
  - Nothing else. No storage, no database, never log tokens/codes, CORS closed, basic abuse protection (size limits, per-IP rate limit).
- All actual API calls (`/v2/...`) go directly from the user's machine to https://api.intra.42.fr with the user's own access token.
- Each user's tokens are stored locally in `~/.config/ft/` (0600). Never in the repo.

### Flow for end users
`ft login`: start a local HTTP server on :8080 (fallback ports 8081, 8082 — all three redirect URIs are registered on my app), open the browser to the 42 authorize URL (with `state`, and the scopes I enabled), receive the code, check `state`, send the code to the broker, store the tokens. Refresh transparently through the broker. `ft logout` removes the tokens.

### Developer configuration (me only)
`./.env` in the CLI repo (loaded with godotenv, gitignored): `FT_CLIENT_ID`, `FT_BROKER_URL`, `FT_REDIRECT_URI`, optional `FT_LOGIN`. Provide `.env.example`.
`broker/.dev.vars` (gitignored) holds `FT_CLIENT_SECRET` for local `wrangler dev`; production secret is set with `wrangler secret put FT_CLIENT_SECRET`.
Load order for the CLI: environment variables > `./.env` > `~/.config/ft/.env` > compile-time defaults.
Never print or log secrets or tokens.

### Known constraints to handle
- The 42 rate limit (~2 req/s, ~1200 req/h) may be counted per application. With many users on one app this can saturate: keep the client-side cache aggressive, avoid polling, and tell me what the docs say.
- Document in the README how to deploy the broker and how to build release binaries (goreleaser, Linux/macOS, amd64/arm64) with the ldflags set.

## Folder ↔ project link
`ft link <project-slug>` writes a `.ft.toml` in the current directory (project id, slug, team id if known). All other commands use it as context when run inside that folder. Suggest fuzzy search if the slug is ambiguous.

## Commands (CLI)
- `ft login` / `ft logout`
- `ft me` — profile, level, wallet, correction points, location
- `ft link <slug>` — link current folder to a project
- `ft status` — project state, team, deadline, repo URL, evaluations
- `ft open` — register to the linked project / create the team
- `ft submit` — get the team's repo URL via API and `git push` to it
- `ft correct` — open slots to be corrected for the linked project
- `ft slots ls | add <start> <end> | rm <id>`
- `ft eval ls` — corrections I have to do / that are upcoming
- `ft events ls | join <id> | leave <id>`
- `ft user <login>` — profile, level, current location
- `ft friends add|rm|ls` — see "Friends" below
- Global flags: `--json` for machine-readable output, `--no-cache`

## Friends
I don't think the public API exposes the intra "friends" feature (verify). Implement it locally: a `friends.json` in the config dir with logins; show their status (online/location via /users/:login/locations), and notify when a friend logs in if possible.

## TUI design (vim / lazygit / Claude Code feel)
Layout: left column (Projects list, Slots), right main panel (details of the selected item), bottom status bar with mode + login + level.

```
┌ Projects ──────┬ libft ──────────────────────────┐
│ ▸ libft   ✓    │ Status: in progress  Deadline 3d│
│   gnl     ●    │ Team: swilmus   Repo: ...       │
│   printf       │ Corrections: 1/3 done           │
├ Slots ─────────┤                                 │
│ 14:00 free     │ [o]pen [s]ubmit [c]orrect       │
│ 16:00 booked   │                                 │
└────────────────┴─────────────────────────────────┘
 NORMAL  swilmus  Lvl 4.2               : for command
```

Vim-style modes and keys:
- NORMAL: `j/k` move, `h/l` or `Tab` switch panel, `Enter` open, `/` search, `:` command palette, `?` help, `q` quit
- Actions: `o` open project, `s` submit, `c` request correction, `e` events view, `f` friends view, `r` refresh
- COMMAND mode (`:`): same commands as the CLI (`:link libft`, `:slot 14:00 16:00`, `:friend add xyz`, `:q`)
- Async API calls via tea.Cmd, with spinners and non-blocking UI; error messages shown in the status bar
- Lip Gloss theme, adapts to terminal size (tea.WindowSizeMsg), works over SSH and in tmux

## Project structure (suggestion)
```
broker/            # Cloudflare Worker (token exchange + refresh only)
cmd/ft/main.go
internal/api/      # HTTP client, auth, refresh, rate limit, cache, pagination
internal/config/   # .env loading, config, .ft.toml, friends.json
internal/commands/ # Cobra commands
internal/tui/      # models, views, keymap, command palette
```

## Rules for you
- Before coding, read the API docs and list the exact endpoints you'll use for each feature; tell me which ones may be restricted by role (slots, corrections) so we can test them early.
- Work in phases and stop after each so I can test:
  1. Broker (local `wrangler dev`) + API client + `.env` loading + `ft login` + `ft me`
  2. `ft link` + `ft status`
  3. `ft open`, `ft submit`, `ft correct`
  4. slots, events, eval, user
  5. TUI (projects panel, details, status bar, `:` palette)
  6. friends, polish, README, install instructions
- Write idiomatic Go, small packages, tests for the API client (mock server) and the config parsing.
- Ask me before making any irreversible choice (module name, license, etc.).
- Never print or log tokens or secrets.

Start with phase 1.

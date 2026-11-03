# ft

A fast terminal client for the 42 intra. Work in progress. Full install and
release docs will come later.

## How auth works

- `ft login` opens the 42 authorize page and listens on `localhost:8080`
  (falling back to `:8081`, then `:8082`). It checks `state`, uses PKCE (S256),
  and sends the code to the **token broker**.
- The broker (`broker/`, a Cloudflare Worker) is the only place that holds the
  42 client secret. It exposes only `POST /token` and `POST /refresh`.
- Every `/v2/...` call goes straight from your machine to `api.intra.42.fr`
  with your own token, which is stored in `~/.config/ft/token.json` (0600).

## Development

```sh
# 1. CLI config (public values only)
cp .env.example .env               # fill in FT_CLIENT_ID

# 2. Broker config (secret, gitignored)
cp broker/.dev.vars.example broker/.dev.vars   # FT_CLIENT_ID + FT_CLIENT_SECRET
make broker-install
make broker-dev                    # http://localhost:8787

# 3. CLI
make build
./ft login
./ft me
./ft api /v2/me                    # raw request, for probing endpoints
```

## Usage

`ft` alone opens the full-screen interface (`?` for keys, `:` for commands).

```sh
ft projects              # the holy graph circle by circle (--after, --all, --upcoming)
ft link libft            # clone your team's repo into ./libft and link it
ft status                # state, team, repo, evaluations
ft open                  # register (falls back to the intra page)
ft submit                # git push HEAD to the team's vogsphere repo
ft correct               # book an evaluation (opens the intra)
ft slots                 # your slots; add 14:00 16:00 | add "tomorrow 9:00" +2h | rm <id>
ft events                # campus events; join <id> | leave <id> | --mine
ft eval                  # upcoming evaluations, both sides; -H: past ones with comments
ft eval clone [id]       # clone the repo of the team you evaluate (./eval-<project>-<login>)
ft logtime [login]       # time on campus per day and week (-w weeks)
ft user <login>          # profile, level, location
```

Every command takes `--json` and `--no-cache`.

While `ft` (the TUI) runs, it reminds you 15 minutes before an evaluation
(given or received) or an event you joined, in the status bar and as a
desktop notification. `FT_REMIND=30m` changes the delay, `FT_REMIND=off`
turns reminders off, `FT_NOTIFY=off` keeps them out of the desktop. Short forms: `ft e` (events),
`ft ev` (eval), `ft l` (link), `ft sl` (slots), `ft u` (user), `ft o`, `ft s`,
`ft c` (open, submit, correct), `ft p` (projects), `ft st` (status).

Circles: the API does not expose the holy graph to students, so ft ships it
per campus in `internal/api/circles/` (42 Belgium 42cursus for now). See
`internal/api/circles/README.md` to add or refresh one; elsewhere, upcoming
projects are sorted by XP.

### Shell completion

Projects, slot ids and event ids complete with Tab.

```sh
# zsh with oh-my-zsh
ft completion zsh > ~/.oh-my-zsh/custom/completions/_ft
# plain zsh (a folder listed in $fpath, before compinit runs)
ft completion zsh > "${fpath[1]}/_ft"
# bash
ft completion bash > ~/.local/share/bash-completion/completions/ft
# fish
ft completion fish > ~/.config/fish/completions/ft.fish
```

Then open a new shell (if nothing completes with zsh: `rm -f ~/.zcompdump* && exec zsh`).

Config precedence: environment > `./.env` > `~/.config/ft/.env` > values baked
in with `make build CLIENT_ID=... BROKER_URL=...`.

`make test` runs the Go tests and the broker tests (`node --test`), then
type-checks the broker.

## Token broker (maintainer)

The broker is a Cloudflare Worker (`broker/`, free tier) that holds the 42
app secret; it only exchanges login codes and refreshes tokens.

```sh
cd broker
npx wrangler login                                   # once
npx wrangler deploy                                  # FT_CLIENT_ID is in wrangler.toml
npx wrangler secret put FT_CLIENT_SECRET             # paste the 42 app secret
curl -X POST -H 'Content-Type: application/json' \
  -d '{"refresh_token":"x"}' https://<worker>/refresh # expect invalid_grant
```

Then build ft with its URL: `make install BROKER_URL=https://<worker>` (or set
`FT_BROKER_URL` in `.env`).

**The 42 app secret expires** (see `secret_valid_until` on the app page):
before that, generate a new one on the intra and run `wrangler secret put
FT_CLIENT_SECRET` again, or every login and refresh fails with
`invalid_client`.

## License

MIT

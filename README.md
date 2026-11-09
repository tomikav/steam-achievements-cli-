<div align="center">

<img src="docs/banner.svg" alt="steam-achievements-cli" width="640"/>

See how close you are to 100%ing your Steam library, straight from the terminal.

[![Release](https://img.shields.io/github/v/release/tomikav/steam-achievements-cli-?style=flat-square&color=c678dd)](https://github.com/tomikav/steam-achievements-cli-/releases/latest)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-windows%20%7C%20macOS%20%7C%20linux-black?style=flat-square)](https://github.com/tomikav/steam-achievements-cli-/releases/latest)
[![License](https://img.shields.io/github/license/tomikav/steam-achievements-cli-?style=flat-square)](LICENSE)

</div>

---

Most Steam libraries are a graveyard of 40% completions. This tool reads your
library through the Steam Web API and answers one question: how close am I to
finishing every game I own?

Running `steam` with no arguments lists every game with at least half of its
achievements unlocked, ranked and color-coded:

```text
$ steam

       GAME                                 PROGRESS               COMPLETE    ACHIEVED  PLAYTIME   LAST PLAYED
   01. Bardcore Rhythm Saga                 [######################] 100.0% DONE  96/96    853.3 h    2025-01-18
   02. Neon Overdrive                       [######################] 100.0% DONE  62/62    686.7 h    2025-01-22
   03. Void Cartographer                    [####################..]  91.7%       55/60    463.3 h    2025-01-21
    4. Pixel Farmer Deluxe                  [####################..]  91.4%       32/35    143.3 h    2024-11-19

  15 of 22 games with >= 50% achievements  |  source: api.steampowered.com
```

## Why

Achievement pages on the Steam client are one game at a time. There is no
library-wide view of what is almost done, what is hopeless, and where your
time actually went. This puts all of it in one table, in your terminal, with
no setup beyond a free API key.

## Install

Download a binary from the [latest release](https://github.com/tomikav/steam-achievements-cli-/releases/latest):

```powershell
# Windows (PowerShell)
irm https://github.com/tomikav/steam-achievements-cli-/releases/latest/download/steam-achievements-cli_windows_amd64.zip -OutFile steam.zip
Expand-Archive steam.zip -Force
```

```bash
# Linux / macOS
curl -fsSL https://github.com/tomikav/steam-achievements-cli-/releases/latest/download/steam-achievements-cli_linux_amd64.tar.gz | tar -xz
sudo mv steam /usr/local/bin/
```

Or with Go:

```bash
go install github.com/tomikav/steam-achievements-cli-@latest
```

Or from source:

```bash
git clone https://github.com/tomikav/steam-achievements-cli-.git
cd steam-achievements-cli-
go build -o steam .
```

## Setup

Two things, once:

1. A free Web API key from [steamcommunity.com/dev/apikey](https://steamcommunity.com/dev/apikey)
2. Your SteamID64 from [steamid.io](https://steamid.io), or just your custom URL name (the tool resolves it)

```bash
steam config set key YOUR_API_KEY
steam config set id 76561198XXXXXXXXX
```

Credentials are stored in your OS config directory (`%AppData%/steam-achievements-cli/`
on Windows, `~/.config/steam-achievements-cli/` elsewhere). Environment variables
`STEAM_API_KEY` and `STEAM_ID` work too and take precedence.

To try it without configuring anything:

```bash
steam list --demo
```

## Commands

| Command | What it does |
|---|---|
| `steam` | every game with at least 50% achievements completed |
| `steam list` | the same view with full filter access |
| `steam perfect` | only 100% complete games |
| `steam game <appid\|name>` | detailed achievement checklist with global unlock rates |
| `steam stats` | library-wide statistics |
| `steam config` | show, set or clear credentials |
| `steam help` | all commands and flags |

## Filters

| Flag | Example | Effect |
|---|---|---|
| `--min N` | `--min 80` | games at 80% completion or higher |
| `--max N` | `--max 95` | cap the range |
| `--search TEXT` | `--search knight` | name contains, case-insensitive |
| `--sort MODE` | `--sort playtime` | `completion`, `name`, `playtime` or `count` |
| `--top N` | `--top 5` | first N results after sorting |
| `--format FMT` | `--format json` | `pretty`, `plain` or `json` |
| `--all` | | include games without an achievement system (as 0%) |
| `--no-cache` | | bypass the local cache |
| `--demo` | | fictional data, no API key needed |

Some combinations worth trying:

```bash
steam                      # default view: everything at 50% or more
steam list --min 90        # the almost-done shelf
steam list --max 60 --sort playtime   # big time sinks that are still far from done
steam list --search souls  # anything Souls-like
steam game 250900 --all    # every achievement of one game
steam list --format json | jq '.[0]'
```

## Themes

Four built-in themes, all single-hue and emoji-free:

| Theme | Look |
|---|---|
| `hacker` | green phosphor terminal (default) |
| `gamer` | deep crimson |
| `steam` | steel blue, close to the client's brand |
| `mono` | pure grayscale, zero colour |

```bash
steam config set theme mono   # persist it
steam --theme gamer list      # or override per run
```

## Performance and privacy

The first run fetches achievement counts for your whole library with 8 parallel
workers, which takes a second or two per hundred games. Results are cached
locally for 24 hours; after that, re-runs are instant.

Your API key stays on your machine. It is written to a user-only config file and
sent only to `api.steampowered.com`.

## FAQ

**Is the API key safe?**
It is stored locally with user-only permissions and only ever sent to Steam's official API.

**Why does the repo name end with a dash?**
A typo at creation time. It stayed, and `go install` works fine with it.

**A game shows no percentage.**
Games without an achievement system are skipped unless you pass `--all`.

**Does it work on private profiles?**
Yes, for your own profile. Your key grants access to your own data.

## Roadmap

- Recent completion velocity (`steam streak`)
- Rarity ranking: your statistically rarest unlocks
- Side-by-side comparison with a friend's library
- Shell completions (bash, zsh, fish, pwsh)

## Contributing

PRs are welcome. `go build ./... && go vet ./...` must pass before submitting.

## License

[MIT](LICENSE)

If you find it useful, a star helps other people find it.

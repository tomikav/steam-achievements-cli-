<p align="center">
  <img src="docs/banner.png" alt="steam-achievements-cli" width="480">
</p>

<p align="center">
  Your Steam achievements in the terminal: progress for every game, achievement lists with icons,<br>
  friends online, and a game view that launches and closes games.
</p>

<p align="center">
  <a href="https://github.com/tomikav/steam-achievements-cli-/releases/latest"><img src="https://img.shields.io/github/v/release/tomikav/steam-achievements-cli-?style=flat-square&label=release" alt="release"></a>
  <a href="https://github.com/tomikav/steam-achievements-cli-/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/tomikav/steam-achievements-cli-/ci.yml?branch=main&style=flat-square&label=build" alt="build"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tomikav/steam-achievements-cli-?style=flat-square" alt="license"></a>
</p>

<p align="center">
  <img src="docs/home.png" alt="Home screen" width="900">
</p>

---

## Install

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tomikav/steam-achievements-cli-/main/install.ps1 | iex
```

**macOS / Linux**:

```bash
curl -fsSL https://raw.githubusercontent.com/tomikav/steam-achievements-cli-/main/install.sh | sh
```

**With Go** (1.26+):

```bash
go install github.com/tomikav/steam-achievements-cli-/cmd/steam@latest
```

The scripts download the latest release, check its SHA-256 checksum and put the
`steam` command on your PATH. No admin rights needed. Open a new terminal when
they finish. On Linux the command is called `steam-ach`, so it does not clash
with the Steam client.

You can also grab a zip from the [releases page](https://github.com/tomikav/steam-achievements-cli-/releases/latest).

## Set up (2 minutes, once)

**1. Get an API key.** Go to [steamcommunity.com/dev/apikey](https://steamcommunity.com/dev/apikey),
type any domain (`localhost` works) and copy the key.

**2. Save the key and your Steam ID.** Your ID is the 17-digit number at
[steamid.io](https://steamid.io); your custom profile name also works.

```bash
steam config set key YOUR_API_KEY
steam config set id  YOUR_STEAM_ID
```

**3. Make your game details public.** In Steam open
[Edit Profile > Privacy Settings](https://steamcommunity.com/my/edit/settings) and set:

| Setting | Value | Why |
|---|---|---|
| My profile | Public | required |
| Game details | **Public** | required, this is where achievements live |
| Friends List | Public | only for the friends view |

"Friends only" is not enough: the API key does not log in as you, so Steam
treats it like a stranger. Changes can take a few minutes to apply.

**Check that everything works:**

```bash
steam doctor
```

Then just run `steam`.

## What it does

### Home

Run `steam`. You land on your library at a glance: what you played last, the
games closest to 100%, and which friends are online and what they are playing.
`enter` opens anything, `p` jumps straight to launching a game.

### Library

Press `l`. Every game you own, including the ones without achievements (demos,
dedicated servers and soundtracks are left out). Tabs split it into in
progress, almost done (75%+), perfect, not started and no achievements. Type
`/` to search, `s` to change the sort.

<p align="center"><img src="docs/browse.png" alt="Library" width="900"></p>

### Game view

Press `enter` on a game: store description, developer, genres, Metacritic
score, install size, your progress and the full achievement list with icons
and descriptions. Unlocked achievements show in colour (gold frame when fewer
than 5% of players have them, silver under 15%); locked ones are greyed out.

<p align="center"><img src="docs/game.png" alt="Game view" width="900"></p>

Press `p` to launch the game and `c` twice to close it. While it runs the view
re-reads your achievements every 45 seconds and shows new unlocks as they
happen.

### Friends

Press `f` for a grid of your friends with their avatar, status and the game
they are playing. `enter` opens their profile.

<p align="center"><img src="docs/friends.png" alt="Friends" width="900"></p>

### Table and stats for scripts

`steam list` prints a table, `steam stats` a dashboard. Both work without the
interactive view and support `--format json` or `--format plain`.

<p align="center"><img src="docs/list.png" alt="steam list" width="900"></p>

Game and achievement icons appear in terminals with image support (Sixel):
Windows Terminal 1.22+, WezTerm, iTerm2 3.5+, foot. Elsewhere everything works,
just without pictures.

## Keys

On the home screen: arrows move between and inside the cards, `enter` opens,
`l` library, `f` friends, `d` stats.

| Key | Library | Game view |
|---|---|---|
| `↑` `↓` | move | browse achievements |
| `←` `→` | switch tab | all / locked / unlocked |
| `enter` | open game | full-screen achievement list |
| `/` | search | |
| `s` / `r` | sort / reverse | sort |
| `p` | open game view | launch the game |
| `c` `c` | | close the game |
| `o` | achievement page in browser | store page |
| `f` | friends | open game folder |
| `d` | dashboard | |
| `t` | change colour theme | change colour theme |
| `?` | all keys | all keys |
| `esc` / `q` | home / quit | back / quit |

Mouse wheel and clicks work too.

## Commands

| Command | Does |
|---|---|
| `steam` | opens the browser (prints the table when piped) |
| `steam list` | table of games at 50% or more (`--min`, `--max`, `--search`, `--sort`, `--top`) |
| `steam perfect` | only 100% games |
| `steam game <name or id>` | achievement checklist for one game |
| `steam stats` | library dashboard |
| `steam doctor` | checks key, Steam ID and privacy settings |
| `steam config` | shows or changes your settings |

Add `--demo` to any command to try it with made-up data, no account needed.

```bash
steam list --min 90                  # almost finished games
steam list --sort playtime --top 10  # where your hours went
steam list --format json             # for scripts
steam --theme synthwave              # themes: hacker, gamer, steam, synthwave, sunset, frost, mono
```

## Problems

Run `steam doctor` first. It tells you exactly what is wrong and how to fix it.

| You see | Fix |
|---|---|
| `Steam rejected your API key` | Create a new key and run `steam config set key ...`. If you set a `STEAM_API_KEY` variable, it overrides the saved key. |
| `Your game details are private` | Set Game details to Public (step 3) and wait a few minutes. |
| `your friends list is private` | Set Friends List to Public, then press `R` in the friends view. |
| A game is skipped with HTTP 500 | Steam fails for that game; it is retried the next day. |
| No icons | Your terminal has no image support, or press `i` to toggle them. |

## Privacy

The tool only reads data. Your key is stored in a file only your user can read
and is sent only to Steam. Results are cached on your machine so later runs
are fast. No telemetry.

## Build from source

```bash
git clone https://github.com/tomikav/steam-achievements-cli-.git
cd steam-achievements-cli-
go build -o steam ./cmd/steam
go test ./...
```

## License

[MIT](LICENSE)

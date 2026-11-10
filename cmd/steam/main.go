// Command steam shows how close you are to 100%ing your Steam library,
// straight from the terminal.
//
//	steam                              every game with 50% or more achievements
//	steam list --min 80 --sort playtime
//	steam game hollow-knight
//	steam stats
//	steam doctor                       diagnose credentials and privacy settings
//	steam config set key XXXXXXX
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/api"
	"github.com/tomikav/steam-achievements-cli-/internal/cache"
	"github.com/tomikav/steam-achievements-cli-/internal/config"
	"github.com/tomikav/steam-achievements-cli-/internal/demo"
	"github.com/tomikav/steam-achievements-cli-/internal/icons"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
	"github.com/tomikav/steam-achievements-cli-/internal/ui"
)

// version is set at release time via -ldflags "-X main.version=...".
var version = "dev"

const defaultThreshold = 50.0

const (
	urlAPIKey  = "https://steamcommunity.com/dev/apikey"
	urlPrivacy = "https://steamcommunity.com/my/edit/settings"
)

func init() {
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = strings.TrimPrefix(bi.Main.Version, "v")
		}
	}
	api.UserAgent = "steam-achievements-cli/" + version
}

// listOptions collects every filter shared by list-like commands.
type listOptions struct {
	min     float64
	max     float64
	search  string
	sortBy  string
	top     int
	format  string
	demo    bool
	noCache bool
	showAll bool
	noIcons bool
	// gamesOnly loads the owned games without downloading achievement
	// counts (cached counts are still applied).
	gamesOnly bool
}

// extractTheme pulls the global --theme flag out of the args.
func extractTheme(args []string) ([]string, string) {
	var out []string
	theme := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--theme" && i+1 < len(args):
			theme = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--theme="):
			theme = strings.TrimPrefix(args[i], "--theme=")
		default:
			out = append(out, args[i])
		}
	}
	return out, theme
}

func main() {
	args, themeFlag := extractTheme(os.Args[1:])
	if themeFlag == "" {
		if cfg, err := config.Load(); err == nil {
			themeFlag = cfg.Theme
		}
	}
	if themeFlag != "" {
		if err := ui.SetTheme(themeFlag); err != nil {
			ui.Errorf("%v", err)
			os.Exit(2)
		}
	}
	cmd := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	case "list":
		runList(args, defaultThreshold)
	case "perfect":
		runList(args, 100)
	case "game":
		runGame(args)
	case "stats":
		runStats(args)
	case "doctor":
		runDoctor()
	case "config":
		runConfig(args)
	case "version", "--version", "-v":
		fmt.Printf("steam-achievements-cli %s\n", version)
	case "help", "--help", "-h":
		usage(os.Stdout, 0)
	default:
		ui.Errorf("unknown command %q", cmd)
		ui.Fix("Run 'steam help' to see every command.")
		os.Exit(2)
	}
}

// --- errors -------------------------------------------------------------------

type errNoCredentials struct{ missing string }

func (e errNoCredentials) Error() string { return e.missing + " not configured" }

// fail prints an error with a concrete fix and exits.
func fail(err error, cfg config.Config) {
	if e, ok := err.(errNoCredentials); ok {
		ui.Errorf("Steam %s.", e.Error())
		ui.Fix("1. Get a free Web API key:  %s", urlAPIKey)
		ui.Fix("2. Find your SteamID64:     https://steamid.io (a custom URL name also works)")
		ui.Fix("3. Save both once:")
		ui.Fix("     steam config set key YOUR_API_KEY")
		ui.Fix("     steam config set id  YOUR_STEAMID64")
		ui.Fix("Or try it without an account: steam list --demo")
		os.Exit(1)
	}

	switch api.KindOf(err) {
	case api.KindInvalidKey:
		ui.Errorf("Steam kept refusing the API key (HTTP 403, retried 4 times).")
		ui.Fix("Key in use: %s, from %s.", config.MaskKey(cfg.APIKey), cfg.KeySource)
		ui.Fix("If this key worked recently, Steam is throttling you: wait a few minutes and retry.")
		if strings.HasPrefix(cfg.KeySource, "env ") {
			ui.Fix("The environment variable overrides config.json. If it holds an old key, remove it:")
			ui.Fix("  [Environment]::SetEnvironmentVariable('%s', $null, 'User')   (PowerShell, then reopen the terminal)", config.EnvAPIKey)
		}
		ui.Fix("If the key was revoked, create a new one at %s and run:", urlAPIKey)
		ui.Fix("  steam config set key NEW_KEY")
	case api.KindPrivateProfile:
		ui.Errorf("Your Steam profile is not public, so the Web API cannot read your library.")
		ui.Fix("Friends-only counts as private: the API key does not sign in as you.")
		ui.Fix("Open %s and set:", urlPrivacy)
		ui.Fix("  My profile:   Public")
		ui.Fix("  Game details: Public")
		ui.Fix("Changes can take a few minutes to reach the API. Check with: steam doctor")
	case api.KindPrivateGames:
		ui.Errorf("Your game details are private, so Steam hides your library and achievements.")
		ui.Fix("Open %s and set 'Game details' to Public.", urlPrivacy)
		ui.Fix("Changes can take a few minutes to reach the API. Check with: steam doctor")
	case api.KindBadSteamID:
		ui.Errorf("%v.", err)
		ui.Fix("SteamID in use: %s, from %s.", cfg.SteamID, cfg.IDSource)
		ui.Fix("Set the 17-digit SteamID64 or your custom URL name: steam config set id VALUE")
	case api.KindRateLimited:
		ui.Errorf("Steam is rate limiting requests.")
		ui.Fix("Wait a few minutes and try again. Results already read are cached, so the next run is lighter.")
	case api.KindNetwork:
		ui.Errorf("%v", err)
		ui.Fix("Check your internet connection, VPN or proxy and try again.")
	default:
		ui.Errorf("%v", err)
		ui.Fix("Run 'steam doctor' for a full diagnosis.")
	}
	os.Exit(1)
}

// --- steam list ------------------------------------------------------------------

func runList(args []string, threshold float64) {
	defer flushWarnings()
	o := &listOptions{min: threshold, sortBy: "completion", format: "pretty"}
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.Float64Var(&o.min, "min", threshold, "minimum completion percent")
	fs.Float64Var(&o.max, "max", 0, "maximum completion percent (0 = no cap)")
	fs.StringVar(&o.search, "search", "", "only games whose name contains this text")
	fs.StringVar(&o.sortBy, "sort", "completion", "completion | name | playtime | count")
	fs.IntVar(&o.top, "top", 0, "show only the first N results after sorting")
	fs.StringVar(&o.format, "format", "pretty", "pretty | plain | json")
	fs.BoolVar(&o.demo, "demo", false, "run with fictional data (no API key needed)")
	fs.BoolVar(&o.noCache, "no-cache", false, "ignore the local achievement cache")
	fs.BoolVar(&o.showAll, "all", false, "include games without any achievements as 0%")
	fs.BoolVar(&o.noIcons, "no-icons", false, "do not draw game icons")
	fs.Usage = func() { fmt.Fprint(os.Stderr, listUsageText) }
	_ = fs.Parse(args)

	switch o.sortBy {
	case "completion", "name", "playtime", "count":
	default:
		ui.Errorf("unknown sort mode %q", o.sortBy)
		ui.Fix("Use one of: completion, name, playtime, count")
		os.Exit(2)
	}
	switch o.format {
	case "pretty", "plain", "json":
	default:
		ui.Errorf("unknown format %q", o.format)
		ui.Fix("Use one of: pretty, plain, json")
		os.Exit(2)
	}

	lib := loadLibrary(o)
	filtered := applyFilters(lib.games, o)

	switch o.format {
	case "json":
		printJSON(filtered)
	case "plain":
		printPlain(filtered)
	default:
		var iconMap map[int]string
		if !o.noIcons && !o.demo && ui.IsTTY() && icons.Supported() {
			var reqs []icons.Request
			for _, g := range filtered {
				if g.Icon != "" {
					reqs = append(reqs, icons.Request{AppID: g.AppID, Hash: g.Icon})
				}
			}
			if len(reqs) > 0 {
				sp := ui.StartSpinner("Loading icons")
				iconMap = icons.Render(reqs, 18)
				sp.Stop()
			}
		}
		if ui.IsTTY() {
			fmt.Print(ui.Banner())
		}
		fmt.Print(ui.RenderTable(filtered, ui.TableOptions{
			Icons:      iconMap,
			Min:        o.min,
			Max:        o.max,
			TotalGames: countTracked(lib.games, o),
			Library:    lib.games,
			Owner:      lib.owner,
			SortBy:     o.sortBy,
			Demo:       o.demo,
		}))
	}
}

const listUsageText = `usage: steam list [flags]

flags:
  --min N        minimum completion percent (default 50; 'steam perfect' = --min 100)
  --max N        maximum completion percent
  --search TEXT  filter by game name (case-insensitive)
  --sort MODE    completion | name | playtime | count   (default: completion)
  --top N        keep only the first N results after sorting
  --format FMT   pretty | plain | json
  --all          also show games that have no achievement system (as 0%)
  --no-cache     re-fetch achievement counts even if cached
  --demo         fictional data, no API key needed
  --no-icons     do not draw game icons (Sixel terminals only)
  --theme NAME   hacker | gamer | steam | mono  (works on any command)
`

// --- loading the library ----------------------------------------------------------

type library struct {
	games  []model.Game
	owner  string
	client *api.Client
	cfg    config.Config
}

// loadLibrary returns the library with achievement counts attached, or
// prints an actionable error and exits.
func loadLibrary(o *listOptions) library {
	if o.demo {
		return library{games: demo.Games()}
	}
	client, cfg, err := resolveClient()
	if err != nil {
		fail(err, cfg)
	}

	sp := ui.StartSpinner("Reading your Steam library...")
	games, err := client.OwnedGames()
	sp.Stop()
	if err != nil {
		fail(err, cfg)
	}

	index := make(map[int]int, len(games))
	for i, g := range games {
		index[g.AppID] = i
	}

	entries := map[int]cache.Entry{}
	if !o.noCache {
		entries = cache.Load()
	}
	// A game that was never launched cannot have unlocked achievements, so
	// when only partially completed games are wanted it is not worth a
	// request. Skipped only if Steam reports playtime at all (it can be hidden).
	skipUnplayed := o.min > 0 && !o.showAll && anyPlaytime(games)
	// GetOwnedGames flags games that have achievements at all. Use the flag
	// only if Steam sent it for at least one game.
	useStatsFlag := anyStatsFlag(games)

	var pending []model.Game
	for i := range games {
		g := &games[i]
		if e, ok := entries[g.AppID]; ok && cache.Fresh(e, g.LastPlayed) {
			g.Achieved, g.Total, g.HasStats = e.Achieved, e.Total, e.Total > 0
		} else if o.gamesOnly {
			continue
		} else if useStatsFlag && !g.MayHaveAch {
			continue // no achievement system: nothing to fetch
		} else if skipUnplayed && g.Playtime == 0 && g.LastPlayed == 0 {
			continue
		} else {
			pending = append(pending, *g)
		}
	}

	if len(pending) > 0 {
		// Save progress as results arrive, every 25 games and on Ctrl+C, so an
		// interrupted run is not wasted.
		var mu sync.Mutex
		fetchedAt := time.Now().Unix()
		unsaved := 0
		save := func() { mu.Lock(); cache.Save(entries); unsaved = 0; mu.Unlock() }

		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		stop := make(chan struct{})
		go func() {
			select {
			case <-interrupt:
				save()
				fmt.Fprintln(os.Stderr, "\r\033[Kinterrupted: progress saved, run the command again to continue.")
				os.Exit(130)
			case <-stop:
			}
		}()

		sp = ui.StartSpinner("Reading achievements")
		sp.Progress(0, len(pending))
		res := client.FetchProgress(pending, 16, func(done, total int, g model.Game, definitive bool) {
			sp.Progress(done, total)
			if !definitive {
				return
			}
			mu.Lock()
			entries[g.AppID] = cache.Entry{Achieved: g.Achieved, Total: g.Total, Fetched: fetchedAt}
			unsaved++
			flush := unsaved >= 25
			mu.Unlock()
			if flush {
				save()
			}
		})
		sp.Stop()
		close(stop)
		signal.Stop(interrupt)
		save()

		if res.Forbidden > 0 && res.Forbidden == len(pending) {
			fail(client.Diagnose(&api.Error{Kind: api.KindForbidden, Status: 403}), cfg)
		}

		for _, p := range pending {
			if i, ok := index[p.AppID]; ok {
				games[i].Achieved, games[i].Total, games[i].HasStats = p.Achieved, p.Total, p.HasStats
			}
		}

		if res.NotPublic > 0 {
			nNotPublic := res.NotPublic
			later(func() {
				ui.Warnf("%s skipped: Steam still reports 'Profile is not public' for them.", plural(nNotPublic, "game", "games"))
				ui.Fix("If you just made your profile public, Steam can take a while to apply it everywhere. Run again later.")
			})
		}
		if n := len(res.Skipped); n > 0 {
			// Remember them for a day so one broken game does not slow down
			// and clutter every run.
			for _, sk := range res.Skipped {
				entries[sk.AppID] = cache.Entry{Fetched: fetchedAt, Unavailable: true}
			}
			cache.Save(entries)
			skipped := res.Skipped
			later(func() {
				ui.Warnf("Steam did not return achievements for %s. Retrying tomorrow.", plural(n, "game", "games"))
				for i, sk := range skipped {
					if i == 3 {
						ui.Fix("... and %d more", n-3)
						break
					}
					ui.Fix("%s: %s", sk.Name, sk.Reason)
				}
			})
		}
	}

	owner := ""
	if o.format == "pretty" {
		if s, err := client.PlayerSummary(); err == nil {
			owner = s.Persona
		}
	}
	return library{games: games, owner: owner, client: client, cfg: cfg}
}

// Warnings raised while loading are printed after the main output, so they
// are not lost above the banner.
var deferred []func()

func later(f func()) { deferred = append(deferred, f) }

func flushWarnings() {
	if len(deferred) > 0 {
		fmt.Fprintln(os.Stderr)
	}
	for _, f := range deferred {
		f()
	}
	deferred = nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func anyStatsFlag(games []model.Game) bool {
	for _, g := range games {
		if g.MayHaveAch {
			return true
		}
	}
	return false
}

func anyPlaytime(games []model.Game) bool {
	for _, g := range games {
		if g.Playtime > 0 {
			return true
		}
	}
	return false
}

// resolveClient builds an API client from config/env, resolving vanity names.
func resolveClient() (*api.Client, config.Config, error) {
	cfg, err := config.Resolve()
	if err != nil {
		return nil, cfg, err
	}
	switch {
	case cfg.APIKey == "" && cfg.SteamID == "":
		return nil, cfg, errNoCredentials{"API key and SteamID"}
	case cfg.APIKey == "":
		return nil, cfg, errNoCredentials{"API key"}
	case cfg.SteamID == "":
		return nil, cfg, errNoCredentials{"SteamID"}
	}
	client := api.New(cfg.APIKey, "")
	id := cfg.SteamID
	if !config.IsSteamID64(id) {
		sp := ui.StartSpinner(fmt.Sprintf("Resolving custom URL %q...", id))
		real, rerr := client.ResolveVanity(id)
		sp.Stop()
		if rerr != nil {
			return nil, cfg, rerr
		}
		id = real
	}
	client.SteamID = id
	return client, cfg, nil
}

// --- filtering / sorting ----------------------------------------------------------------

func applyFilters(games []model.Game, o *listOptions) []model.Game {
	out := []model.Game{}
	q := strings.ToLower(o.search)
	for _, g := range games {
		if g.HasStats && g.Total > 0 {
			g.Percent = float64(g.Achieved) / float64(g.Total) * 100
		} else {
			if !o.showAll {
				continue
			}
			g.Percent = 0
		}
		if g.Percent < o.min {
			continue
		}
		if o.max > 0 && g.Percent > o.max {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(g.Name), q) {
			continue
		}
		out = append(out, g)
	}
	switch o.sortBy {
	case "name":
		sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	case "playtime":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Playtime > out[j].Playtime })
	case "count":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	default: // completion
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Percent == out[j].Percent {
				return out[i].Playtime > out[j].Playtime
			}
			return out[i].Percent > out[j].Percent
		})
	}
	if o.top > 0 && len(out) > o.top {
		out = out[:o.top]
	}
	return out
}

func countTracked(games []model.Game, o *listOptions) int {
	n := 0
	for _, g := range games {
		if (g.HasStats && g.Total > 0) || o.showAll {
			n++
		}
	}
	return n
}

// --- output formats ------------------------------------------------------------------------

func printJSON(games []model.Game) {
	data, _ := json.MarshalIndent(games, "", "  ")
	fmt.Println(string(data))
}

func printPlain(games []model.Game) {
	fmt.Println("percent\tachieved\ttotal\tappid\tname")
	for _, g := range games {
		fmt.Printf("%.1f\t%d\t%d\t%d\t%s\n", g.Percent, g.Achieved, g.Total, g.AppID, g.Name)
	}
}

// --- steam game -------------------------------------------------------------------------------

func runGame(args []string) {
	defer flushWarnings()
	o := &listOptions{format: "plain", gamesOnly: true}
	fs := flag.NewFlagSet("game", flag.ExitOnError)
	fs.BoolVar(&o.demo, "demo", false, "fictional data")
	fs.BoolVar(&o.noCache, "no-cache", false, "re-fetch achievement counts")
	fs.BoolVar(&o.showAll, "all", false, "list every achievement, locked included")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: steam game <appid|name> [--all] [--demo] [--no-cache]") }
	pos, flagArgs := splitPositional(args)
	_ = fs.Parse(flagArgs)
	if len(pos) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	query := strings.Join(pos, " ")

	lib := loadLibrary(o)
	game, ok := pickGame(lib.games, query)
	if !ok {
		ui.Errorf("no game matching %q in your library", query)
		ui.Fix("Search by name with: steam list --all --search %q", query)
		os.Exit(1)
	}
	var ach []model.Achievement
	if o.demo {
		ach = demo.Achievements(game)
	} else {
		sp := ui.StartSpinner(fmt.Sprintf("Loading achievements for %s...", game.Name))
		var err error
		ach, err = lib.client.PlayerAchievements(game.AppID)
		if err == nil {
			var defs map[string]model.Achievement
			if _, defs, err = lib.client.GameDetail(game.AppID); err == nil {
				ach = lib.client.MergeDetail(ach, defs)
			}
		}
		sp.Stop()
		if err != nil {
			if api.KindOf(err) == api.KindNoStats {
				ui.Errorf("%s has no achievements.", game.Name)
				os.Exit(1)
			}
			fail(err, lib.cfg)
		}
		game.Achieved, game.Total = 0, len(ach)
		for _, a := range ach {
			if a.Achieved {
				game.Achieved++
			}
		}
	}
	if game.Total > 0 {
		game.Percent = float64(game.Achieved) / float64(game.Total) * 100
	}
	fmt.Print(ui.RenderDetail(game, ach, o.showAll))
}

// splitPositional separates non-flag arguments from flag arguments so that
// both `steam game neon --demo` and `steam game --demo neon` work.
func splitPositional(args []string) (pos []string, flags []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			pos = append(pos, a)
		}
	}
	return pos, flags
}

func pickGame(games []model.Game, query string) (model.Game, bool) {
	if id, err := strconv.Atoi(query); err == nil {
		for _, g := range games {
			if g.AppID == id {
				return g, true
			}
		}
		return model.Game{}, false
	}
	for _, g := range games {
		if strings.EqualFold(g.Name, query) {
			return g, true
		}
	}
	q := strings.ToLower(strings.ReplaceAll(query, "-", " "))
	best, found := model.Game{}, false
	for _, g := range games {
		if strings.Contains(strings.ToLower(g.Name), q) {
			if !found || g.Playtime > best.Playtime {
				best, found = g, true
			}
		}
	}
	return best, found
}

// --- steam stats ----------------------------------------------------------------------------------

func runStats(args []string) {
	defer flushWarnings()
	o := &listOptions{format: "pretty"}
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	fs.BoolVar(&o.demo, "demo", false, "fictional data")
	fs.BoolVar(&o.noCache, "no-cache", false, "re-fetch achievement counts")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: steam stats [--demo] [--no-cache]") }
	_ = fs.Parse(args)

	lib := loadLibrary(o)

	if o.demo {
		ui.Hint("Demo mode: fictional data, not your real library.")
	}
	if ui.IsTTY() {
		fmt.Print(ui.Banner())
	}
	fmt.Print(ui.RenderStats(lib.games, lib.owner))
}

// --- steam doctor ----------------------------------------------------------------------------------

// runDoctor checks, in order: credential format, the key, the profile and
// the game library, and stops at the first hard failure with a fix.
func runDoctor() {
	fmt.Println()
	fmt.Println("  " + ui.Title("steam doctor"))
	fmt.Println()

	cfg, err := config.Resolve()
	path := mustFilePath()
	if err != nil {
		ui.Check("config file", "fail", err.Error())
		os.Exit(1)
	}
	ui.Check("config file", "ok", path)

	// 1. Format checks.
	switch {
	case cfg.APIKey == "":
		ui.Check("api key", "fail", "not set")
	case !config.ValidKey(cfg.APIKey):
		ui.Check("api key", "fail", fmt.Sprintf("%s from %s is not 32 hex characters", config.MaskKey(cfg.APIKey), cfg.KeySource))
	default:
		ui.Check("api key", "ok", fmt.Sprintf("%s  (%s)", config.MaskKey(cfg.APIKey), cfg.KeySource))
	}
	if fileCfg, _ := config.Load(); strings.HasPrefix(cfg.KeySource, "env ") && fileCfg.APIKey != "" && fileCfg.APIKey != cfg.APIKey {
		ui.Check("", "warn", fmt.Sprintf("%s overrides a different key in config.json", config.EnvAPIKey))
	}
	switch {
	case cfg.SteamID == "":
		ui.Check("steam id", "fail", "not set")
	case config.IsSteamID64(cfg.SteamID):
		ui.Check("steam id", "ok", fmt.Sprintf("%s  (%s)", cfg.SteamID, cfg.IDSource))
	default:
		ui.Check("steam id", "warn", fmt.Sprintf("%q is a custom URL name, resolving it", cfg.SteamID))
	}
	if cfg.APIKey == "" || cfg.SteamID == "" {
		fmt.Println()
		missing := "API key and SteamID"
		if cfg.APIKey != "" {
			missing = "SteamID"
		} else if cfg.SteamID != "" {
			missing = "API key"
		}
		fail(errNoCredentials{missing}, cfg)
	}
	if !config.ValidKey(cfg.APIKey) {
		fmt.Println()
		ui.Errorf("the API key is malformed.")
		ui.Fix("Copy it again from %s and run: steam config set key YOUR_API_KEY", urlAPIKey)
		os.Exit(1)
	}

	client := api.New(cfg.APIKey, cfg.SteamID)
	if !config.IsSteamID64(cfg.SteamID) {
		id, err := client.ResolveVanity(cfg.SteamID)
		if err != nil {
			ui.Check("custom url", "fail", err.Error())
			doctorFail(err, cfg)
		}
		client.SteamID = id
		ui.Check("custom url", "ok", "resolves to SteamID64 "+id+"  (save it: steam config set id "+id+")")
	}

	// 2. The key, against an endpoint that works on any profile.
	summary, err := client.PlayerSummary()
	if err != nil {
		if api.KindOf(err) == api.KindInvalidKey {
			ui.Check("key accepted", "fail", "GetPlayerSummaries answered 403")
		} else {
			ui.Check("key accepted", "fail", err.Error())
		}
		ui.Check("profile", "skip", "")
		ui.Check("library", "skip", "")
		doctorFail(err, cfg)
	}
	ui.Check("key accepted", "ok", "GetPlayerSummaries answered 200")

	// 3. Profile visibility.
	if !summary.Public() {
		ui.Check("profile", "fail", fmt.Sprintf("%s is private or friends-only", summary.Persona))
		ui.Check("library", "skip", "")
		doctorFail(&api.Error{Kind: api.KindPrivateProfile}, cfg)
	}
	ui.Check("profile", "ok", summary.Persona+" is public")

	// 4. Library and achievements.
	games, err := client.OwnedGames()
	if err != nil {
		ui.Check("library", "fail", err.Error())
		doctorFail(err, cfg)
	}
	ui.Check("library", "ok", fmt.Sprintf("%d games", len(games)))

	// Sample the most played games: one game can refuse on its own (delisted,
	// hidden stats), so only a refusal on every sample means a privacy problem.
	sample := mostPlayed(games, 5)
	var readable, noStats, forbidden int
	var lastForbidden error
	for _, g := range sample {
		_, _, err := client.AchievementProgress(g.AppID)
		switch {
		case err == nil:
			readable++
		case api.KindOf(err) == api.KindNoStats:
			noStats++
		case api.KindOf(err) == api.KindForbidden:
			forbidden++
			lastForbidden = err
		}
	}
	switch {
	case len(sample) == 0:
	case readable > 0:
		ui.Check("achievements", "ok", fmt.Sprintf("readable (%d of %d most played games)", readable, len(sample)))
	case forbidden == len(sample):
		ui.Check("achievements", "fail", lastForbidden.Error())
		doctorFail(&api.Error{Kind: api.KindPrivateGames}, cfg)
	case forbidden > 0:
		ui.Check("achievements", "warn", fmt.Sprintf("%d of %d sampled games refused: %v", forbidden, len(sample), lastForbidden))
		ui.Check("", "warn", "Recent privacy change? Steam can take a while to apply it. Run doctor again later.")
	default:
		ui.Check("achievements", "warn", fmt.Sprintf("none of the %d most played games has achievements", len(sample)))
	}

	fmt.Println()
	ui.Ok("everything looks good. Run: steam list")
}

func doctorFail(err error, cfg config.Config) {
	fmt.Println()
	fail(err, cfg)
}

func mostPlayed(games []model.Game, n int) []model.Game {
	var sorted []model.Game
	for _, g := range games {
		if g.MayHaveAch || !anyStatsFlag(games) {
			sorted = append(sorted, g)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Playtime > sorted[j].Playtime })
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// --- steam config ----------------------------------------------------------------------------------

func runConfig(args []string) {
	cfg, err := config.Load()
	if err != nil {
		ui.Errorf("%v", err)
		ui.Fix("Fix the file by hand or reset it with: steam config clear")
		os.Exit(1)
	}

	if len(args) == 0 || args[0] == "show" {
		r, _ := config.Resolve()
		theme := r.Theme
		if theme == "" {
			theme = "hacker (default)"
		}
		id := r.SteamID
		if id == "" {
			id = "(not set)"
		}
		fmt.Printf("  %-10s %s  %s\n", "api_key", config.MaskKey(r.APIKey), ui.Dim("("+r.KeySource+")"))
		fmt.Printf("  %-10s %s  %s\n", "steam_id", id, ui.Dim("("+r.IDSource+")"))
		fmt.Printf("  %-10s %s\n", "theme", theme)
		fmt.Printf("  %-10s %s\n", "file", mustFilePath())
		return
	}

	switch args[0] {
	case "set":
		if len(args) != 3 {
			ui.Errorf("usage: steam config set <key|id|theme> <value>")
			os.Exit(2)
		}
		field, value := args[1], config.Normalize(args[2])
		switch field {
		case "key":
			if !config.ValidKey(value) {
				ui.Errorf("that does not look like a Steam Web API key (expected 32 hex characters, got %d)", len(value))
				ui.Fix("Copy the key from %s", urlAPIKey)
				os.Exit(2)
			}
			cfg.APIKey = value
		case "id":
			cfg.SteamID = value
			if !config.IsSteamID64(value) {
				r, _ := config.Resolve()
				key := r.APIKey
				if key == "" {
					ui.Warnf("no API key yet, saving %q as a custom URL name. It is resolved on first use.", value)
				} else if id, err := api.New(key, "").ResolveVanity(value); err == nil {
					ui.Hint("Custom URL %q resolves to SteamID64 %s.", value, id)
					cfg.SteamID = id
				} else {
					ui.Warnf("could not resolve %q right now (%v). Saved as-is.", value, err)
				}
			}
		case "theme":
			if err := ui.SetTheme(value); err != nil {
				ui.Errorf("%v", err)
				os.Exit(2)
			}
			cfg.Theme = value
		default:
			ui.Errorf("unknown field %q (use key, id or theme)", field)
			os.Exit(2)
		}
		if err := config.Save(cfg); err != nil {
			ui.Errorf("saving config: %v", err)
			os.Exit(1)
		}
		ui.Ok("saved to " + mustFilePath())
		if os.Getenv(config.EnvAPIKey) != "" && field == "key" {
			ui.Warnf("%s is set and overrides this value. Remove it to use the saved key.", config.EnvAPIKey)
		}
		if os.Getenv(config.EnvSteamID) != "" && field == "id" {
			ui.Warnf("%s is set and overrides this value. Remove it to use the saved ID.", config.EnvSteamID)
		}
	case "clear":
		path, err := config.FilePath()
		if err == nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				ui.Errorf("could not remove %s: %v", path, err)
				os.Exit(1)
			}
		}
		ui.Ok("config cleared")
	case "path":
		fmt.Println(mustFilePath())
	default:
		ui.Errorf("usage: steam config [show | set <key|id|theme> <value> | path | clear]")
		os.Exit(2)
	}
}

func mustFilePath() string {
	p, err := config.FilePath()
	if err != nil {
		return "(unavailable)"
	}
	return p
}

// --- help -------------------------------------------------------------------------------------------

func usage(w io.Writer, code int) {
	section := func(title string, rows [][2]string, colW int) {
		fmt.Fprintln(w, "  "+ui.Title(title))
		for _, r := range rows {
			fmt.Fprintf(w, "    %-*s  %s\n", colW, r[0], ui.Dim(r[1]))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprint(w, ui.Banner())
	fmt.Fprintf(w, "  %s %s\n\n", ui.Dim("version"), version)
	section("COMMANDS", [][2]string{
		{"steam", "every game with 50% or more achievements"},
		{"steam list [flags]", "filter and sort your library"},
		{"steam perfect", "only 100% complete games"},
		{"steam game <appid|name>", "achievement checklist for one game"},
		{"steam stats", "library dashboard"},
		{"steam doctor", "diagnose API key, SteamID and privacy settings"},
		{"steam config", "show, set or clear credentials"},
		{"steam version", "print the version"},
	}, 24)
	section("LIST FLAGS", [][2]string{
		{"--min N", "minimum completion percent (default 50)"},
		{"--max N", "maximum completion percent"},
		{"--search TEXT", "filter by name"},
		{"--sort MODE", "completion | name | playtime | count"},
		{"--top N", "keep only the first N results"},
		{"--format FMT", "pretty | plain | json"},
		{"--all", "include games without achievements"},
		{"--no-cache", "ignore the local cache"},
		{"--no-icons", "do not draw game icons"},
		{"--demo", "fictional data, no API key needed"},
		{"--theme NAME", "hacker | gamer | steam | mono (default: hacker)"},
	}, 24)
	section("EXAMPLES", [][2]string{
		{"steam list --min 80 --sort playtime", ""},
		{"steam list --search knight --top 5", ""},
		{"steam game 367520 --all", ""},
		{"steam --theme gamer stats", ""},
	}, 24)
	fmt.Fprintf(w, "  %s %s\n", ui.Dim("First run? Get a free API key at"), urlAPIKey)
	fmt.Fprintf(w, "  %s %s\n\n", ui.Dim("Something wrong? Run"), "steam doctor")
	os.Exit(code)
}

// Command steam shows how close you are to 100%ing your Steam library,
// straight from the terminal.
//
//	steam            → every game with ≥ 50% achievements completed
//	steam list --min 80 --sort playtime
//	steam game hollow-knight
//	steam stats
//	steam config set key XXXXXXX
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/api"
	"github.com/tomikav/steam-achievements-cli-/internal/cache"
	"github.com/tomikav/steam-achievements-cli-/internal/config"
	"github.com/tomikav/steam-achievements-cli-/internal/demo"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
	"github.com/tomikav/steam-achievements-cli-/internal/ui"
)

const version = "1.0.0"
const defaultThreshold = 50.0

var steamID64Re = regexp.MustCompile(`^\d{17}$`)

// listOptions collects every filter shared by list-like commands.
type listOptions struct {
	min      float64
	max      float64
	search   string
	sortBy   string
	top      int
	format   string
	demo     bool
	noCache  bool
	showAll  bool
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
	case "config":
		runConfig(args)
	case "version", "--version", "-v":
		fmt.Printf("steam %s\n", version)
	case "help", "--help", "-h":
		usage(0)
	default:
		ui.Errorf("unknown command %q", cmd)
		usage(1)
	}
}

// --- steam list -----------------------------------------------------------------

func runList(args []string, threshold float64) {
	o := &listOptions{min: threshold, sortBy: "completion", top: 0, format: "pretty"}
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.Float64Var(&o.min, "min", threshold, "minimum completion %% to include")
	fs.Float64Var(&o.max, "max", 0, "maximum completion %% to include (0 = no cap)")
	fs.StringVar(&o.search, "search", "", "only games whose name contains this text")
	fs.StringVar(&o.sortBy, "sort", "completion", "completion | name | playtime | count")
	fs.IntVar(&o.top, "top", 0, "show only the first N results after sorting")
	fs.StringVar(&o.format, "format", "pretty", "pretty | plain | json")
	fs.BoolVar(&o.demo, "demo", false, "run with fictional data (no API key needed)")
	fs.BoolVar(&o.noCache, "no-cache", false, "ignore the local achievement cache")
	fs.BoolVar(&o.showAll, "all", false, "include games without any achievements as 0%")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, listUsageText)
	}
	_ = fs.Parse(args)

	games, err := loadLibrary(o)
	if err != nil {
		handleLoadError(err)
	}
	filtered := applyFilters(games, o)

	switch o.format {
	case "json":
		printJSON(filtered)
	case "plain":
		printPlain(filtered)
	default:
		if o.demo {
			ui.Hint("(demo mode — fictional data, not your real library)")
		}
		fmt.Print(ui.Banner())
		fmt.Print(ui.RenderTable(filtered, ui.TableOptions{
			Min:        o.min,
			TotalGames: countTracked(games, o),
			FetchedFrom: "api.steampowered.com",
		}))
	}
}

const listUsageText = `usage: steam list [flags]

flags:
  --min N        minimum completion %% (default 50; alias: steam perfect = --min 100)
  --max N        maximum completion %%
  --search TEXT  filter by game name (case-insensitive)
  --sort MODE    completion | name | playtime | count   (default: completion)
  --top N        keep only the first N results after sorting
  --format FMT   pretty | plain | json
  --all          also show games that have no achievement system (as 0%%)
  --no-cache     re-fetch achievement counts even if cached
  --demo         fictional data, no API key needed
  --theme NAME   hacker | gamer | steam | mono

  (the theme flag works on any command: steam --theme mono list)
`

// --- loading the library -------------------------------------------------------------

// loadLibrary returns the library with achievement counts attached.
func loadLibrary(o *listOptions) ([]model.Game, error) {
	if o.demo {
		return demo.Games(), nil
	}
	client, err := resolveClient()
	if err != nil {
		return nil, err
	}

	sp := ui.StartSpinner("Reading your Steam library…")
	games, err := client.OwnedGames()
	sp.Stop()
	if err != nil {
		return nil, err
	}

	index := make(map[int]int, len(games)) // appid -> slice position
	for i, g := range games {
		index[g.AppID] = i
	}

	entries := map[int]cache.Entry{}
	if !o.noCache {
		entries = cache.Load()
	}
	var pending []model.Game
	for i := range games {
		g := &games[i]
		if e, ok := entries[g.AppID]; ok && cache.Valid(e) {
			g.Achieved = e.Achieved
			g.Total = e.Total
			g.HasStats = e.Total > 0
		} else {
			pending = append(pending, *g)
		}
	}

	if len(pending) > 0 {
		sp = ui.StartSpinner(fmt.Sprintf("Downloading achievements… 0/%d", len(pending)))
		client.FetchProgress(pending, 8, func(done, total int, g model.Game) {
			sp.Update(fmt.Sprintf("Downloading achievements… %d/%d", done, total))
		})
		sp.Stop()

		fetchedAt := time.Now().Unix()
		cacheMap := entries
		if o.noCache {
			cacheMap = map[int]cache.Entry{}
		}
		for _, p := range pending {
			if i, ok := index[p.AppID]; ok {
				games[i].Achieved = p.Achieved
				games[i].Total = p.Total
				games[i].HasStats = p.HasStats
			}
			cacheMap[p.AppID] = cache.Entry{Achieved: p.Achieved, Total: p.Total, Fetched: fetchedAt}
		}
		cache.Save(cacheMap)
	}
	return games, nil
}

// handleLoadError prints the right guidance and exits.
func handleLoadError(err error) {
	if isNoCreds(err) {
		dieNoCredentials()
	} else {
		ui.Errorf("%v", err)
	}
	os.Exit(1)
}

// resolveClient builds an API client from config/env, resolving vanity names.
func resolveClient() (*api.Client, error) {
	cfg, err := config.Resolve()
	if err != nil {
		return nil, err
	}
	if cfg.APIKey == "" || cfg.SteamID == "" {
		return nil, errNoCredentials{}
	}
	client := api.New(cfg.APIKey, "")
	id := cfg.SteamID
	if !steamID64Re.MatchString(id) {
		sp := ui.StartSpinner(fmt.Sprintf("Resolving vanity URL %q…", id))
		real, rerr := client.ResolveVanity(id)
		sp.Stop()
		if rerr != nil {
			return nil, fmt.Errorf("resolving vanity URL: %w", rerr)
		}
		id = real
	}
	client.SteamID = id
	return client, nil
}

type errNoCredentials struct{}

func (errNoCredentials) Error() string { return "missing Steam API key or SteamID" }

func dieNoCredentials() {
	ui.Errorf("Steam API key or SteamID not configured.")
	fmt.Fprintln(os.Stderr, `
  1. Grab a free Web API key → https://steamcommunity.com/dev/apikey
  2. Find your SteamID64     → https://steamid.io (or use your custom URL name)
  3. Save them once:

       steam config set key YOUR_API_KEY
       steam config set id  76561198XXXXXXXXX

  You can also use env vars: STEAM_API_KEY and STEAM_ID.
  Or just play around first:  steam list --demo`)
}

// --- filtering / sorting ---------------------------------------------------------------

func applyFilters(games []model.Game, o *listOptions) []model.Game {
	var out []model.Game
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
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	case "playtime":
		sort.Slice(out, func(i, j int) bool { return out[i].Playtime > out[j].Playtime })
	case "count":
		sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	default: // completion
		sort.Slice(out, func(i, j int) bool {
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
		if g.HasStats && g.Total > 0 {
			n++
		} else if o.showAll {
			n++
		}
	}
	return n
}

// --- output formats ----------------------------------------------------------------------

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

// --- steam game -----------------------------------------------------------------------------

func runGame(args []string) {
	o := &listOptions{demo: false, noCache: false}
	fs := flag.NewFlagSet("game", flag.ExitOnError)
	fs.BoolVar(&o.demo, "demo", false, "fictional data")
	fs.BoolVar(&o.noCache, "no-cache", false, "re-fetch achievement counts")
	fs.BoolVar(&o.showAll, "all", false, "list every achievement, locked included")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: steam game <appid|name> [--all]") }
	pos, flagArgs := splitPositional(args)
	_ = fs.Parse(flagArgs)
	rest := pos
	if len(rest) != 1 {
		fs.Usage()
		os.Exit(2)
	}
	query := rest[0]

	games, err := loadLibrary(o)
	if err != nil {
		if isNoCreds(err) {
			dieNoCredentials()
		} else {
			ui.Errorf("%v", err)
		}
		os.Exit(1)
	}
	game, ok := pickGame(games, query)
	if !ok {
		ui.Errorf("no game matching %q in your library", query)
		os.Exit(1)
	}

	var ach []model.Achievement
	if o.demo {
		ach = demo.Achievements(game)
	} else {
		client, err := resolveClient()
		if err != nil {
			ui.Errorf("%v", err)
			os.Exit(1)
		}
		sp := ui.StartSpinner(fmt.Sprintf("Loading achievements for %s…", game.Name))
		ach, err = client.PlayerAchievements(game.AppID)
		var defs map[string]model.Achievement
		if err == nil {
			_, defs, err = client.GameDetail(game.AppID)
			ach = client.MergeDetail(game.AppID, ach, defs)
		}
		sp.Stop()
		if err != nil {
			ui.Errorf("could not load achievements for %s: %v", game.Name, err)
			os.Exit(1)
		}
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
	q := strings.ToLower(query)
	for _, g := range games {
		if strings.EqualFold(g.Name, query) {
			return g, true
		}
	}
	best, bestSeen := model.Game{}, false
	for _, g := range games {
		if strings.Contains(strings.ToLower(g.Name), q) {
			if !bestSeen || g.Playtime > best.Playtime {
				best, bestSeen = g, true
			}
		}
	}
	return best, bestSeen
}

// --- steam stats ------------------------------------------------------------------------------

func runStats(args []string) {
	o := &listOptions{demo: false, noCache: false}
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	fs.BoolVar(&o.demo, "demo", false, "fictional data")
	fs.BoolVar(&o.noCache, "no-cache", false, "re-fetch achievement counts")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: steam stats [--demo] [--no-cache]") }
	_ = fs.Parse(args)

	games, err := loadLibrary(o)
	if err != nil {
		if isNoCreds(err) {
			dieNoCredentials()
		} else {
			ui.Errorf("%v", err)
		}
		os.Exit(1)
	}

	var s ui.Stats
	sum := 0.0
	tracked := 0
	for _, g := range games {
		s.TotalGames++
		s.TotalPlaytime += g.Playtime
		if !g.HasStats || g.Total == 0 {
			continue
		}
		tracked++
		pct := float64(g.Achieved) / float64(g.Total) * 100
		sum += pct
		if pct >= 50 {
			s.OverHalf++
		}
		if pct >= 99.5 {
			s.Perfect++
		}
	}
	s.WithAchievements = tracked
	if tracked > 0 {
		s.Average = sum / float64(tracked)
	}
	if o.demo {
		ui.Hint("(demo mode — fictional data, not your real library)")
	}
	fmt.Print(ui.Banner())
	fmt.Print(ui.RenderStats(s))
}

// --- steam config ---------------------------------------------------------------------------------

func runConfig(args []string) {
	cfg, _ := config.Load()

	if len(args) == 0 {
		cfgResolved, _ := config.Resolve()
		fmt.Print(ui.Banner())
		fmt.Println("  " + strings.ReplaceAll(cfgResolved.String(), "\n", "\n  "))
		fmt.Println()
		ui.Hint("config file: %s", mustFilePath())
		return
	}

	switch args[0] {
	case "set":
		if len(args) != 3 {
			ui.Errorf("usage: steam config set <key|id> <value>")
			os.Exit(2)
		}
		field, value := args[1], args[2]
		switch field {
		case "key":
			cfg.APIKey = value
		case "id":
			cfg.SteamID = value
			if !steamID64Re.MatchString(value) {
				client, err := resolveClientRaw(cfg.APIKey, value)
				if err == nil {
					ui.Hint("resolved %q → SteamID64 %s", value, client.SteamID)
					cfg.SteamID = client.SteamID
				} else {
					ui.Hint("could not resolve vanity URL now (%v); saving it as-is.", err)
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
		ui.Ok("saved. Try it: steam list")
	case "clear":
		path, err := config.FilePath()
		if err == nil {
			_ = os.Remove(path)
		}
		ui.Ok("config cleared.")
	default:
		ui.Errorf("usage: steam config [set <key|id> <value> | clear]")
		os.Exit(2)
	}
}

// resolveClientRaw builds a client from explicit credentials.
func resolveClientRaw(key, id string) (*api.Client, error) {
	if key == "" || id == "" {
		return nil, fmt.Errorf("need both key and id")
	}
	client := api.New(key, "")
	if !steamID64Re.MatchString(id) {
		real, err := client.ResolveVanity(id)
		if err != nil {
			return nil, err
		}
		client.SteamID = real
	} else {
		client.SteamID = id
	}
	return client, nil
}

func mustFilePath() string {
	p, err := config.FilePath()
	if err != nil {
		return "(unavailable)"
	}
	return p
}

// --- misc -----------------------------------------------------------------------------------------

func isNoCreds(err error) bool {
	_, ok := err.(errNoCredentials)
	return ok
}

// usage prints the master help screen.
func usage(code int) {
	fmt.Print(ui.Banner())
	fmt.Print(`  COMMANDS
    steam                        every game with ≥ 50% achievements
    steam list [flags]           filter & sort your library
    steam perfect                only 100% complete games
    steam game <appid|name>      detailed achievement checklist
    steam stats                  library-wide statistics
    steam config                 set / show / clear your credentials
    steam version                print version

  LIST FLAGS
    --min N        minimum completion % (default 50)
    --max N        maximum completion %
    --search TEXT  filter by name
    --sort MODE    completion | name | playtime | count
    --top N        keep only the first N results
    --format FMT   pretty | plain | json
    --all          include games without achievements
    --no-cache     bypass the 24h local cache
    --demo         fictional data, no API key needed
    --theme NAME   hacker | gamer | steam | mono (default: hacker)

  EXAMPLES
    steam list --min 80 --sort playtime
    steam list --search knight --top 5
    steam game 367520 --all
    steam stats --demo

  First run? Grab a free API key → https://steamcommunity.com/dev/apikey
  Full docs in the README. ⭐ If this tool sparks joy, star the repo!
`)
	os.Exit(code)
}

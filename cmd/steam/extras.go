package main

// Wiring for the views that go beyond achievements: hunt, deals, news,
// storage, the Steam memory meter, and `steam play`.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/api"
	"github.com/tomikav/steam-achievements-cli-/internal/cache"
	"github.com/tomikav/steam-achievements-cli-/internal/demo"
	"github.com/tomikav/steam-achievements-cli-/internal/launcher"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
	"github.com/tomikav/steam-achievements-cli-/internal/ui"
)

// blobCache is a small concurrent cache saved to one file.
type blobCache struct {
	mu   sync.Mutex
	name string
	ttl  time.Duration
	m    map[string]cache.Blob
}

func openBlobs(name string, ttl time.Duration) *blobCache {
	return &blobCache{name: name, ttl: ttl, m: cache.LoadBlobs(name)}
}

func (c *blobCache) get(key string, v any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[key]
	if !ok || time.Since(time.Unix(b.At, 0)) > c.ttl {
		return false
	}
	return json.Unmarshal(b.Data, v) == nil
}

func (c *blobCache) put(key string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cache.Blob{At: time.Now().Unix(), Data: data}
	for k, b := range c.m { // drop expired entries so the file stays small
		if time.Since(time.Unix(b.At, 0)) > c.ttl {
			delete(c.m, k)
		}
	}
	cache.SaveBlobs(c.name, c.m)
}

// parallel runs fn over ids with n workers.
func parallel(ids []int, n int, fn func(id int)) {
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				fn(id)
			}
		}()
	}
	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
}

func wireExtras(opt *ui.BrowseOptions, lib library, isDemo bool) {
	if isDemo {
		opt.LoadDeals = func() ([]model.Deal, error) { return demo.Deals(), nil }
		opt.LoadNews = func([]int) ([]model.News, error) { return demo.News(), nil }
		opt.LoadInstalled = func() ([]ui.Installed, error) {
			var out []ui.Installed
			for _, in := range demo.Installed() {
				out = append(out, ui.Installed{AppID: in.AppID, Name: in.Name, Size: in.Size})
			}
			return out, nil
		}
		opt.SteamMemory = func() (int64, bool) { return 742 << 20, true }
		return
	}
	c := lib.client
	names := map[int]string{}
	for _, g := range lib.games {
		names[g.AppID] = g.Name
	}

	globals := openBlobs("globals", 7*24*time.Hour)
	opt.LoadGlobal = func(appID int) (map[string]float64, error) {
		var m map[string]float64
		key := strconv.Itoa(appID)
		if globals.get(key, &m) {
			return m, nil
		}
		m, err := c.GlobalPercentages(appID)
		if err != nil {
			return nil, err
		}
		globals.put(key, m)
		return m, nil
	}

	storeNames := openBlobs("names", 90*24*time.Hour)
	opt.LoadDeals = func() ([]model.Deal, error) {
		var out []model.Deal
		ids, wishErr := c.Wishlist()
		if len(ids) > 0 {
			prices, err := api.Prices(ids)
			if err != nil && len(prices) == 0 {
				wishErr = err
			}
			var mu sync.Mutex
			parallel(ids, 8, func(id int) {
				d, ok := prices[id]
				if !ok {
					d = model.Deal{AppID: id}
				}
				d.Wishlist = true
				key := strconv.Itoa(id)
				if !storeNames.get(key, &d.Name) {
					if n, err := api.AppName(id); err == nil {
						d.Name = n
						storeNames.put(key, n)
					}
				}
				if d.Name == "" {
					d.Name = fmt.Sprintf("App %d", id)
				}
				mu.Lock()
				out = append(out, d)
				mu.Unlock()
			})
		}
		specials, specErr := api.Specials()
		owned := map[int]bool{}
		for _, g := range lib.games {
			owned[g.AppID] = true
		}
		for _, d := range specials {
			if !owned[d.AppID] {
				out = append(out, d)
			}
		}
		if len(out) == 0 {
			if wishErr != nil {
				return nil, wishErr
			}
			return nil, specErr
		}
		return out, nil
	}

	opt.LoadNews = func(appIDs []int) ([]model.News, error) {
		var mu sync.Mutex
		var out []model.News
		var firstErr error
		parallel(appIDs, 6, func(id int) {
			items, err := c.News(id, 3)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			cutoff := time.Now().AddDate(0, -6, 0).Unix()
			for _, n := range items {
				if n.Date >= cutoff {
					n.Game = names[id]
					out = append(out, n)
				}
			}
		})
		sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
		if len(out) == 0 && firstErr != nil {
			return nil, firstErr
		}
		return out, nil
	}

	opt.LoadInstalled = func() ([]ui.Installed, error) {
		var out []ui.Installed
		for _, in := range launcher.Installed() {
			name := in.Name
			if n, ok := names[in.AppID]; ok {
				name = n
			}
			out = append(out, ui.Installed{AppID: in.AppID, Name: name, Size: in.SizeBytes, Dir: in.Dir})
		}
		return out, nil
	}
	opt.SteamMemory = launcher.SteamMemory
	opt.CloseSteam = launcher.CloseSteam
	opt.Uninstall = launcher.Uninstall
}

// --- steam play ----------------------------------------------------------------

// runPlay launches a game by name without opening the Steam window.
func runPlay(args []string) {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	isDemo := fs.Bool("demo", false, "fictional data")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: steam play <appid|name>") }
	pos, flagArgs := splitPositional(args)
	_ = fs.Parse(flagArgs)
	if len(pos) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	query := strings.Join(pos, " ")

	var games []model.Game
	if *isDemo {
		games = demo.Games()
	} else {
		client, cfg, err := resolveClient()
		if err != nil {
			fail(err, cfg)
		}
		sp := ui.StartSpinner("Looking up " + query + "...")
		games, err = client.OwnedGames()
		sp.Stop()
		if err != nil {
			fail(err, cfg)
		}
	}
	game, ok := pickGame(games, query)
	if !ok {
		ui.Errorf("no game matching %q in your library", query)
		ui.Fix("Search by name with: steam list --all --search %q", query)
		os.Exit(1)
	}
	if *isDemo {
		ui.Ok(fmt.Sprintf("Demo mode: would launch %s (%d)", game.Name, game.AppID))
		return
	}
	if err := launcher.Launch(game.AppID); err != nil {
		ui.Errorf("could not launch %s: %v", game.Name, err)
		os.Exit(1)
	}
	ui.Ok("Launching " + game.Name)
	ui.Hint("Steam starts hidden in the tray if it was closed.")
}

// --- store, profile, wrapped ------------------------------------------------

func wireStore(opt *ui.BrowseOptions, lib library, isDemo bool) {
	cart := openBlobs("cart", 100*365*24*time.Hour)
	if isDemo {
		opt.LoadStoreFront = func() (map[string][]model.StoreItem, error) { return demo.StoreFront(), nil }
		opt.SearchStore = func(t string) ([]model.StoreItem, error) { return demo.StoreSearch(t), nil }
		opt.LoadStore = func(id int) (model.StoreInfo, error) { return demo.StoreDetail(id), nil }
		opt.LoadProfile = func() (model.Profile, error) { return demo.Profile(), nil }
		var mem []model.CartItem
		opt.LoadCart = func() []model.CartItem { return mem }
		opt.SaveCart = func(c []model.CartItem) { mem = c }
	} else {
		// Store lists, store pages and the profile are cached on disk so
		// the views open instantly; they refresh after a short while.
		front := openBlobs("storefront", 30*time.Minute)
		pages := openBlobs("storepages", 72*time.Hour)
		prof := openBlobs("profile", 10*time.Minute)
		opt.LoadStoreFront = func() (map[string][]model.StoreItem, error) {
			var m map[string][]model.StoreItem
			if front.get("v1", &m) && len(m) > 0 {
				return m, nil
			}
			m, err := api.StoreFront()
			if err == nil {
				front.put("v1", m)
			}
			return m, err
		}
		opt.LoadStore = func(id int) (model.StoreInfo, error) {
			var info model.StoreInfo
			key := strconv.Itoa(id)
			if pages.get(key, &info) && info.Name != "" {
				return info, nil
			}
			info, err := api.StoreDetails(id)
			if err == nil {
				pages.put(key, info)
			}
			return info, err
		}
		opt.SearchStore = api.StoreSearch
		opt.LoadProfile = func() (model.Profile, error) {
			var p model.Profile
			if prof.get(lib.client.SteamID, &p) && p.Persona != "" {
				return p, nil
			}
			p, err := lib.client.Profile()
			if err == nil {
				prof.put(lib.client.SteamID, p)
			}
			return p, err
		}
		opt.BuyInSteam = launcher.StorePage
		opt.InstallApp = launcher.InstallApp
		opt.LoadCart = func() []model.CartItem {
			var c []model.CartItem
			cart.get("items", &c)
			return c
		}
		opt.SaveCart = func(c []model.CartItem) { cart.put("items", c) }
	}
	if isDemo {
		opt.LoadVersus = func(f model.Friend) (ui.Versus, error) { return demoVersus(lib.games, f), nil }
	} else {
		opt.LoadVersus = func(f model.Friend) (ui.Versus, error) { return compare(lib, f) }
	}
	opt.ExportWrapped = func(svg string) (string, error) {
		dir, err := os.UserHomeDir()
		if err != nil {
			dir = "."
		}
		if d := filepath.Join(dir, "Desktop"); dirExists(d) {
			dir = d
		}
		path := filepath.Join(dir, fmt.Sprintf("steam-wrapped-%d.svg", time.Now().Year()))
		return path, os.WriteFile(path, []byte(svg), 0o644)
	}
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// runStore opens the interactive store, searching right away when a name
// is given.
func runStore(args []string) {
	pos, flagArgs := splitPositional(args)
	runBrowseAt(flagArgs, "store", strings.Join(pos, " "))
}

// runWrapped shows the year: interactive slides in a terminal, a printed
// summary otherwise, and an SVG share card with --svg.
func runWrapped(args []string) {
	fs := flag.NewFlagSet("wrapped", flag.ExitOnError)
	isDemo := fs.Bool("demo", false, "fictional data")
	svg := fs.String("svg", "", "save a 1200x630 share card to this file")
	year := fs.Int("year", time.Now().Year(), "year to summarize")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: steam wrapped [--svg FILE] [--year N] [--demo]") }
	_ = fs.Parse(args)
	if *svg == "" && *year == time.Now().Year() && ui.IsTTY() {
		rest := []string{}
		if *isDemo {
			rest = append(rest, "--demo")
		}
		runBrowseAt(rest, "wrapped", "")
		return
	}
	lib := loadLibrary(&listOptions{format: "plain", demo: *isDemo})
	w := ui.ComputeWrapped(lib.games, *year, nil)
	fmt.Print(ui.RenderWrapped(w, lib.owner, ui.TermWidth()))
	if *svg != "" {
		if err := os.WriteFile(*svg, []byte(ui.WrappedSVG(w, lib.owner)), 0o644); err != nil {
			ui.Errorf("could not save %s: %v", *svg, err)
			os.Exit(1)
		}
		ui.Ok("Share card saved to " + *svg)
	}
}

// compare builds the versus view: games both own, hours, and achievement
// progress for the most played shared games.
func compare(lib library, f model.Friend) (ui.Versus, error) {
	fc := api.New(lib.client.Key, f.SteamID)
	theirs, err := fc.OwnedGames()
	if err != nil {
		return ui.Versus{}, err
	}
	mine := map[int]model.Game{}
	for _, g := range lib.games {
		mine[g.AppID] = g
	}
	v := ui.Versus{TheirCount: len(theirs)}
	for _, t := range theirs {
		m, ok := mine[t.AppID]
		if !ok {
			continue
		}
		vg := ui.VersusGame{AppID: t.AppID, Name: m.Name, Mine: m.Playtime, Theirs: t.Playtime, MyPct: -1, TheirPct: -1}
		if m.HasStats && m.Total > 0 {
			vg.MyPct = float64(m.Achieved) / float64(m.Total) * 100
		}
		v.Games = append(v.Games, vg)
	}
	sort.SliceStable(v.Games, func(i, j int) bool {
		return v.Games[i].Mine+v.Games[i].Theirs > v.Games[j].Mine+v.Games[j].Theirs
	})
	// their progress for the top shared games with achievements
	var ids []int
	idx := map[int]int{}
	for i, g := range v.Games {
		if g.MyPct >= 0 && len(ids) < 12 {
			ids = append(ids, g.AppID)
			idx[g.AppID] = i
		}
	}
	var mu sync.Mutex
	parallel(ids, 4, func(id int) {
		a, t, err := fc.AchievementProgress(id)
		if err != nil || t == 0 {
			return
		}
		mu.Lock()
		v.Games[idx[id]].TheirPct = float64(a) / float64(t) * 100
		mu.Unlock()
	})
	return v, nil
}

func demoVersus(games []model.Game, f model.Friend) ui.Versus {
	v := ui.Versus{TheirCount: 140}
	for i, g := range games {
		if i%3 == 2 {
			continue
		}
		vg := ui.VersusGame{AppID: g.AppID, Name: g.Name, Mine: g.Playtime, Theirs: (g.Playtime*(7+i*5))%40000 + 600, MyPct: -1, TheirPct: -1}
		if g.HasStats && g.Total > 0 {
			vg.MyPct = float64(g.Achieved) / float64(g.Total) * 100
			vg.TheirPct = float64((g.AppID*37 + len(f.Name)*11) % 100)
		}
		v.Games = append(v.Games, vg)
	}
	sort.SliceStable(v.Games, func(i, j int) bool {
		return v.Games[i].Mine+v.Games[i].Theirs > v.Games[j].Mine+v.Games[j].Theirs
	})
	return v
}

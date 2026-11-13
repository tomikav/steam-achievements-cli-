package main

// Wiring for the views that go beyond achievements: hunt, deals, news,
// storage, the Steam memory meter, and `steam play`.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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

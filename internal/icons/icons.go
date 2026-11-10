// Package icons downloads Steam game icons and renders them inline with the
// Sixel graphics protocol (Windows Terminal 1.22+, WezTerm, foot, xterm -ti
// vt340, mlterm, iTerm2 3.5+). Icons are cached on disk forever: a game's
// icon hash changes when the icon does.
package icons

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // Steam serves icons as JPEG
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Supported reports whether the terminal is expected to render Sixel images.
// STEAM_ICONS=1 or STEAM_ICONS=0 overrides the detection.
func Supported() bool {
	switch os.Getenv("STEAM_ICONS") {
	case "1", "true", "on":
		return true
	case "0", "false", "off":
		return false
	}
	if os.Getenv("WT_SESSION") != "" { // Windows Terminal
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "WezTerm", "iTerm.app", "mintty":
		return true
	}
	t := os.Getenv("TERM")
	return strings.HasPrefix(t, "foot") || strings.Contains(t, "mlterm")
}

// URL is where Steam serves a game's 32x32 library icon.
func URL(appID int, hash string) string {
	return fmt.Sprintf("https://media.steampowered.com/steamcommunity/public/images/apps/%d/%s.jpg", appID, hash)
}

func cachePath(appID int, hash string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "steam-achievements-cli", "icons", fmt.Sprintf("%d_%s.jpg", appID, hash)), nil
}

var client = &http.Client{Timeout: 8 * time.Second}

// Load returns the decoded icon, from disk cache or downloaded.
func Load(appID int, hash string) (image.Image, error) {
	if hash == "" {
		return nil, fmt.Errorf("no icon")
	}
	path, err := cachePath(appID, hash)
	if err != nil {
		return nil, err
	}
	if f, err := os.Open(path); err == nil {
		img, _, derr := image.Decode(f)
		f.Close()
		if derr == nil {
			return img, nil
		}
	}
	resp, err := client.Get(URL(appID, hash))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("icon: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
	img, _, err := image.Decode(strings.NewReader(string(data)))
	return img, err
}

// Request identifies one icon to fetch.
type Request struct {
	AppID int
	Hash  string
}

// Render loads every requested icon in parallel and returns Sixel strings
// keyed by AppID, each size x size pixels. Failures are skipped silently:
// a missing icon just leaves its slot blank.
func Render(reqs []Request, size int) map[int]string {
	out := make(map[int]string, len(reqs))
	var mu sync.Mutex
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, r := range reqs {
		wg.Add(1)
		go func(r Request) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			img, err := Load(r.AppID, r.Hash)
			if err != nil {
				return
			}
			s := Sixel(img, size, size)
			mu.Lock()
			out[r.AppID] = s
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	return out
}

// --- sixel encoding --------------------------------------------------------------

// resize scales img to w x h with a box filter (averages source pixels).
func resize(img image.Image, w, h int) [][]color.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := make([][]color.RGBA, h)
	for y := 0; y < h; y++ {
		out[y] = make([]color.RGBA, w)
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var r, g, bl, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r, g, bl, n = r+cr>>8, g+cg>>8, bl+cb>>8, n+1
				}
			}
			out[y][x] = color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255}
		}
	}
	return out
}

// quantize maps pixels to a palette of at most 256 colours by dropping low
// bits until the image fits.
func quantize(px [][]color.RGBA) (pal []color.RGBA, idx [][]int) {
	for bits := 5; bits >= 2; bits-- {
		shift := 8 - bits
		key := func(c color.RGBA) uint32 {
			return uint32(c.R>>shift)<<16 | uint32(c.G>>shift)<<8 | uint32(c.B>>shift)
		}
		seen := map[uint32]int{}
		for _, row := range px {
			for _, c := range row {
				if _, ok := seen[key(c)]; !ok {
					seen[key(c)] = len(seen)
				}
			}
		}
		if len(seen) > 256 && bits > 2 {
			continue
		}
		// Palette entry = average of the pixels in each bucket.
		sums := make([][4]uint32, len(seen))
		idx = make([][]int, len(px))
		for y, row := range px {
			idx[y] = make([]int, len(row))
			for x, c := range row {
				i := seen[key(c)]
				idx[y][x] = i
				sums[i][0] += uint32(c.R)
				sums[i][1] += uint32(c.G)
				sums[i][2] += uint32(c.B)
				sums[i][3]++
			}
		}
		pal = make([]color.RGBA, len(sums))
		for i, s := range sums {
			pal[i] = color.RGBA{uint8(s[0] / s[3]), uint8(s[1] / s[3]), uint8(s[2] / s[3]), 255}
		}
		return pal, idx
	}
	return nil, nil
}

// Sixel encodes img scaled to w x h as a Sixel escape sequence.
func Sixel(img image.Image, w, h int) string {
	px := resize(img, w, h)
	pal, idx := quantize(px)
	var b strings.Builder
	// P2=1: pixels not drawn keep the terminal background.
	b.WriteString("\x1bP0;1;0q")
	fmt.Fprintf(&b, "\"1;1;%d;%d", w, h)
	pct := func(v uint8) int { return (int(v)*100 + 127) / 255 }
	for i, c := range pal {
		fmt.Fprintf(&b, "#%d;2;%d;%d;%d", i, pct(c.R), pct(c.G), pct(c.B))
	}
	for band := 0; band < h; band += 6 {
		// colours present in this band, in stable order
		used := map[int]bool{}
		for y := band; y < min(band+6, h); y++ {
			for x := 0; x < w; x++ {
				used[idx[y][x]] = true
			}
		}
		colors := make([]int, 0, len(used))
		for c := range used {
			colors = append(colors, c)
		}
		sort.Ints(colors)
		for ci, c := range colors {
			if ci > 0 {
				b.WriteByte('$') // back to the start of the band
			}
			fmt.Fprintf(&b, "#%d", c)
			run, last := 0, byte(0)
			flush := func() {
				switch {
				case run == 0:
				case run > 3:
					fmt.Fprintf(&b, "!%d%c", run, last)
				default:
					b.WriteString(strings.Repeat(string(last), run))
				}
			}
			for x := 0; x < w; x++ {
				var bits byte
				for k := 0; k < 6 && band+k < h; k++ {
					if idx[band+k][x] == c {
						bits |= 1 << k
					}
				}
				ch := 63 + bits
				if ch == last && run > 0 {
					run++
					continue
				}
				flush()
				last, run = ch, 1
			}
			flush()
		}
		b.WriteByte('-') // next band
	}
	b.WriteString("\x1b\\")
	return b.String()
}

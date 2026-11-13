// Package icons downloads Steam game icons and renders them inline with the
// Sixel graphics protocol (Windows Terminal 1.22+, WezTerm, foot, xterm -ti
// vt340, mlterm, iTerm2 3.5+). Icons are cached on disk forever: a game's
// icon hash changes when the icon does.
package icons

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // Steam serves icons as JPEG
	_ "image/png"
	"io"
	"math"
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

// Load returns the decoded game icon, from disk cache or downloaded.
func Load(appID int, hash string) (image.Image, error) {
	if hash == "" {
		return nil, fmt.Errorf("no icon")
	}
	path, err := cachePath(appID, hash)
	if err != nil {
		return nil, err
	}
	return loadCached(URL(appID, hash), path)
}

// LoadURL returns any image from Steam's CDN (achievement icons, for
// instance), cached on disk under a hash of its URL.
func LoadURL(u string) (image.Image, error) {
	if u == "" {
		return nil, fmt.Errorf("no icon")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(u))
	path := filepath.Join(base, "steam-achievements-cli", "icons", "u_"+hex.EncodeToString(sum[:10])+".img")
	return loadCached(u, path)
}

func loadCached(u, path string) (image.Image, error) {
	if f, err := os.Open(path); err == nil {
		img, _, derr := image.Decode(f)
		f.Close()
		if derr == nil {
			return img, nil
		}
	}
	resp, err := client.Get(u)
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

// Request identifies one icon to fetch: a game icon (AppID + Hash) or any
// image URL. Key is how the result is indexed.
type Request struct {
	Key   string
	AppID int
	Hash  string
	URL   string
	Size  int    // pixels; 0 means the size passed to RenderKeyed
	W, H  int    // explicit pixel size for non-square images (wins over Size)
	Frame string // optional hex colour of a 1-2 px border ("#ffc83d")
	Dim   bool   // darken, for locked achievements
}

// RenderKeyed loads requests in parallel and returns Sixel strings by Key.
func RenderKeyed(reqs []Request, size int) map[string]string {
	out := make(map[string]string, len(reqs))
	var mu sync.Mutex
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, r := range reqs {
		wg.Add(1)
		go func(r Request) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var img image.Image
			var err error
			if r.URL != "" {
				img, err = LoadURL(r.URL)
			} else {
				img, err = Load(r.AppID, r.Hash)
			}
			if err != nil {
				return
			}
			sz := size
			if r.Size > 0 {
				sz = r.Size
			}
			w, h := sz, sz
			if r.W > 0 && r.H > 0 {
				w, h = r.W, r.H
			}
			s := SixelStyled(img, w, h, r.Frame, r.Dim)
			mu.Lock()
			out[r.Key] = s
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	return out
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

// quantize reduces the image to at most 256 colours with a weighted median
// cut over 5-bit-per-channel buckets, then maps each pixel to the nearest
// palette entry.
func quantize(px [][]color.RGBA) (pal []color.RGBA, idx [][]int) {
	type entry struct {
		c       [3]int // bucket colour, 0..31 per channel
		r, g, b int
		n       int
	}
	key := func(c color.RGBA) int { return int(c.R>>3)<<10 | int(c.G>>3)<<5 | int(c.B>>3) }
	byKey := map[int]*entry{}
	for _, row := range px {
		for _, c := range row {
			k := key(c)
			e := byKey[k]
			if e == nil {
				e = &entry{c: [3]int{int(c.R >> 3), int(c.G >> 3), int(c.B >> 3)}}
				byKey[k] = e
			}
			e.r += int(c.R)
			e.g += int(c.G)
			e.b += int(c.B)
			e.n++
		}
	}
	all := make([]*entry, 0, len(byKey))
	for _, e := range byKey {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool { // deterministic output
		return all[i].c[0]<<10|all[i].c[1]<<5|all[i].c[2] < all[j].c[0]<<10|all[j].c[1]<<5|all[j].c[2]
	})

	boxes := [][]*entry{all}
	spread := func(b []*entry) (int, int) { // widest channel and its range, weighted by pixel count
		best, bestR := 0, -1
		for ch := 0; ch < 3; ch++ {
			lo, hi := 99, -1
			for _, e := range b {
				lo, hi = min(lo, e.c[ch]), max(hi, e.c[ch])
			}
			total := 0
			for _, e := range b {
				total += e.n
			}
			if r := (hi - lo) * int(math.Sqrt(float64(total))); r > bestR {
				best, bestR = ch, r
			}
		}
		return best, bestR
	}
	for len(boxes) < 256 {
		bi, bestR, bestCh := -1, 0, 0
		for i, b := range boxes {
			if len(b) < 2 {
				continue
			}
			if ch, r := spread(b); r > bestR {
				bi, bestR, bestCh = i, r, ch
			}
		}
		if bi < 0 {
			break
		}
		b := boxes[bi]
		sort.SliceStable(b, func(i, j int) bool { return b[i].c[bestCh] < b[j].c[bestCh] })
		total := 0
		for _, e := range b {
			total += e.n
		}
		half, acc, cut := total/2, 0, 1
		for i, e := range b {
			acc += e.n
			if acc >= half {
				cut = max(1, min(i+1, len(b)-1))
				break
			}
		}
		boxes[bi] = b[:cut]
		boxes = append(boxes, b[cut:])
	}

	slot := map[int]int{}
	for i, b := range boxes {
		var r, g, bl, n int
		for _, e := range b {
			r, g, bl, n = r+e.r, g+e.g, bl+e.b, n+e.n
			slot[e.c[0]<<10|e.c[1]<<5|e.c[2]] = i
		}
		pal = append(pal, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
	}
	idx = make([][]int, len(px))
	for y, row := range px {
		idx[y] = make([]int, len(row))
		for x, c := range row {
			idx[y][x] = slot[key(c)]
		}
	}
	return pal, idx
}

// Sixel encodes img scaled to w x h as a Sixel escape sequence.
func Sixel(img image.Image, w, h int) string { return SixelStyled(img, w, h, "", false) }

// SixelStyled is Sixel with an optional coloured frame and darkening.
func SixelStyled(img image.Image, w, h int, frame string, dim bool) string {
	px := resize(img, w, h)
	if dim {
		for y := range px {
			for x := range px[y] {
				c := px[y][x]
				px[y][x] = color.RGBA{uint8(float64(c.R) * 0.45), uint8(float64(c.G) * 0.45), uint8(float64(c.B) * 0.45), 255}
			}
		}
	}
	if frame != "" {
		var r, g, b uint8
		fmt.Sscanf(strings.TrimPrefix(frame, "#"), "%02x%02x%02x", &r, &g, &b)
		fc := color.RGBA{r, g, b, 255}
		t := 1
		if w >= 28 {
			t = 2
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if x < t || y < t || x >= w-t || y >= h-t {
					px[y][x] = fc
				}
			}
		}
	}
	return encode(px, w, h)
}

func encode(px [][]color.RGBA, w, h int) string {
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

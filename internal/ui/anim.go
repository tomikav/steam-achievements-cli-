package ui

// Motion: every view eases in when it opens (bars grow, numbers count up,
// charts fill), and the home screen keeps a slow shimmer on the banner.
// Everything here is cosmetic: with motion off, or in tests, the frame is
// simply the final one.

import (
	"math"
	"strings"
	"time"
)

const (
	animDur   = 650 * time.Millisecond
	frameTick = 66 * time.Millisecond // 15 frames per second
)

// animT is the eased progress (0..1) of the current view's entry animation.
// It is 1 outside of an animated frame, so plain CLI output is never affected.
var animT = 1.0

// animClock is the time in seconds since the browser started, for looping
// effects. Zero means "no motion".
var animClock = 0.0

func easeOut(t float64) float64 {
	t = math.Min(math.Max(t, 0), 1)
	return 1 - math.Pow(1-t, 3)
}

// animKey names the current view; when it changes the entry animation restarts.
func (b *browser) animKey() string {
	k := []int{int(b.mode)}
	switch b.mode {
	case modePanel:
		k = append(k, int(b.extra.kind), b.extra.panels[b.extra.kind].tab)
	case modePlay:
		k = append(k, b.play.appID)
	case modeList:
		k = append(k, b.tab)
	case modeWrapped:
		k = append(k, b.wrapped.slide)
	case modeStore:
		k = append(k, b.store.tab)
	case modeHome:
		if g, ok := b.homeFocus(); ok {
			k = append(k, g.AppID)
		}
	}
	var s strings.Builder
	for _, n := range k {
		s.WriteString(itoaFast(n))
		s.WriteByte('.')
	}
	return s.String()
}

func itoaFast(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

// animating reports whether the entry animation is still running.
func (b *browser) animating() bool {
	return b.motion && time.Since(b.animStart) < animDur+2*frameTick
}

// beginFrame sets the animation clocks for one build; endFrame resets them.
func (b *browser) beginFrame() {
	if !b.motion {
		animT, animClock = 1, 0
		return
	}
	animT = easeOut(float64(time.Since(b.animStart)) / float64(animDur))
	animClock = time.Since(b.started).Seconds()
}

func endFrame() { animT, animClock = 1, 0 }

// spin is the current spinner glyph.
func (b *browser) spin() string { return spinnerFrames[(b.frameN/2)%len(spinnerFrames)] }

// countUp animates a number towards its value.
func countUp(n int) int { return int(math.Round(float64(n) * animT)) }

// pulse blends two colours on a slow sine; static when motion is off.
func pulse(a, c string) string {
	if animClock == 0 {
		return a
	}
	// a few discrete steps: most frames repeat the previous colour, so the
	// row is not sent again
	return lerp(a, c, math.Round((0.5+0.5*math.Sin(animClock*4))*6)/6)
}

// animatedBanner draws the title art: it wipes in from the left when the
// home screen opens and a highlight sweeps across it every few seconds.
func animatedBanner() []string {
	lines := strings.Split(bannerArt, "\n")
	out := make([]string, 0, len(lines)+1)
	cols := 0
	for _, l := range lines {
		cols = max(cols, len([]rune(l)))
	}
	reveal := int(math.Ceil(animT * float64(cols+4)))
	// the highlight crosses the banner in 1.2 s, every 5 s
	sweep := -100.0
	if animClock > 0 {
		if ph := math.Mod(animClock, 5); ph < 1.2 {
			sweep = ph/1.2*float64(cols+16) - 8
		}
	}
	for row, line := range lines {
		runes := []rune(line)
		var sb strings.Builder
		sb.WriteString("  ")
		for col, r := range runes {
			if col > reveal || r == ' ' {
				sb.WriteByte(' ')
				continue
			}
			t := (float64(col)/float64(len(runes)) + float64(row)/float64(len(lines))*0.35) / 1.35
			c := lerp(active.From, active.To, t)
			if d := math.Abs(float64(col) + float64(row)*1.5 - sweep); d < 5 {
				c = lerp(c, "#ffffff", (1-d/5)*0.75)
			}
			if col > reveal-3 && animT < 1 {
				c = lerp(c, "#ffffff", 0.6) // bright leading edge of the wipe
			}
			if r == '█' {
				sb.WriteString(paint(c, string(r)))
			} else {
				sb.WriteString(paint(lerp(c, active.Faint, 0.55), string(r)))
			}
		}
		sb.WriteString(strings.Repeat(" ", max(cols-len(runes), 0)))
		out = append(out, sb.String())
	}
	rule := int(math.Round(18 * animT))
	out = append(out, "  "+paint(active.Dim, "A C H I E V E M E N T S")+"   "+paint(active.Faint, strings.Repeat("─", rule))+strings.Repeat(" ", 18-rule))
	return out
}

// shimmer colours s along the theme gradient with a moving highlight; used
// for the achievement-unlocked toast.
func shimmer(s string) string {
	if animClock == 0 {
		return gradient(s)
	}
	runes := []rune(s)
	pos := math.Mod(animClock*18, float64(len(runes)+12)) - 6
	var sb strings.Builder
	for i, r := range runes {
		c := lerp(active.From, active.To, float64(i)/float64(max(len(runes)-1, 1)))
		if d := math.Abs(float64(i) - pos); d < 4 {
			c = lerp(c, "#ffffff", (1-d/4)*0.8)
		}
		sb.WriteString(bold(c, string(r)))
	}
	return sb.String()
}

// liveFrame reports whether the current screen changes on its own and must
// be redrawn every tick (spinners, the roulette, the palette caret).
func (b *browser) liveFrame() bool {
	switch {
	case b.palette.open && b.motion:
		return true
	case b.rouletteSpinning():
		return true
	case b.mode == modeStore && (b.store.loading || b.store.searching || b.store.detailBusy != 0 || b.store.typing):
		return true
	case b.mode == modeVersus && b.versus.loading:
		return true
	case (b.mode == modeProfile || b.mode == modeWrapped) && (b.profile.loading || b.profile.showcaseBusy):
		return true
	}
	return false
}

package ui

// A small pixel font drawn with half blocks, so every pixel is square:
// a glyph 5 pixels tall takes 3 terminal rows. Used for the profile name,
// the Wrapped slides and big numbers.

import (
	"strings"
	"unicode"
)

var glyphs = map[rune][]string{
	'A':  {".##.", "#..#", "####", "#..#", "#..#"},
	'B':  {"###.", "#..#", "###.", "#..#", "###."},
	'C':  {".###", "#...", "#...", "#...", ".###"},
	'D':  {"###.", "#..#", "#..#", "#..#", "###."},
	'E':  {"####", "#...", "###.", "#...", "####"},
	'F':  {"####", "#...", "###.", "#...", "#..."},
	'G':  {".###", "#...", "#.##", "#..#", ".###"},
	'H':  {"#..#", "#..#", "####", "#..#", "#..#"},
	'I':  {"###", ".#.", ".#.", ".#.", "###"},
	'J':  {"..##", "...#", "...#", "#..#", ".##."},
	'K':  {"#..#", "#.#.", "##..", "#.#.", "#..#"},
	'L':  {"#...", "#...", "#...", "#...", "####"},
	'M':  {"#...#", "##.##", "#.#.#", "#...#", "#...#"},
	'N':  {"#...#", "##..#", "#.#.#", "#..##", "#...#"},
	'O':  {".##.", "#..#", "#..#", "#..#", ".##."},
	'P':  {"###.", "#..#", "###.", "#...", "#..."},
	'Q':  {".##.", "#..#", "#..#", "#.#.", ".#.#"},
	'R':  {"###.", "#..#", "###.", "#.#.", "#..#"},
	'S':  {".###", "#...", ".##.", "...#", "###."},
	'T':  {"#####", "..#..", "..#..", "..#..", "..#.."},
	'U':  {"#..#", "#..#", "#..#", "#..#", ".##."},
	'V':  {"#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W':  {"#...#", "#...#", "#.#.#", "##.##", "#...#"},
	'X':  {"#..#", "#..#", ".##.", "#..#", "#..#"},
	'Y':  {"#...#", ".#.#.", "..#..", "..#..", "..#.."},
	'Z':  {"####", "...#", ".##.", "#...", "####"},
	'0':  {".##.", "#..#", "#.##", "##.#", ".##."},
	'1':  {".#.", "##.", ".#.", ".#.", "###"},
	'2':  {"###.", "...#", ".##.", "#...", "####"},
	'3':  {"###.", "...#", ".##.", "...#", "###."},
	'4':  {"#..#", "#..#", "####", "...#", "...#"},
	'5':  {"####", "#...", "###.", "...#", "###."},
	'6':  {".##.", "#...", "###.", "#..#", ".##."},
	'7':  {"####", "...#", "..#.", ".#..", ".#.."},
	'8':  {".##.", "#..#", ".##.", "#..#", ".##."},
	'9':  {".##.", "#..#", ".###", "...#", ".##."},
	'.':  {".", ".", ".", ".", "#"},
	',':  {"..", "..", "..", ".#", "#."},
	'%':  {"#..#", "...#", "..#.", ".#..", "#..#"},
	'+':  {"...", ".#.", "###", ".#.", "..."},
	'-':  {"...", "...", "###", "...", "..."},
	'!':  {"#", "#", "#", ".", "#"},
	'?':  {"###.", "...#", ".##.", "....", ".#.."},
	':':  {".", "#", ".", "#", "."},
	'/':  {"...#", "..#.", ".#..", "#...", "...."},
	'_':  {"....", "....", "....", "....", "####"},
	'\'': {"#", "#", ".", ".", "."},
	'#':  {".#.#.", "#####", ".#.#.", "#####", ".#.#."},
	'&':  {".#..", "#.#.", ".#.#", "#.#.", ".#.#"},
	' ':  {"..", "..", "..", "..", ".."},
}

// bigTextWidth is the width in columns of s drawn with bigText at scale 1.
func bigTextWidth(s string) int {
	w := 0
	for i, r := range strings.ToUpper(s) {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs['?']
		}
		if i > 0 {
			w++
		}
		w += len(g[0])
	}
	return w
}

// bigText draws s in the pixel font, three rows tall, coloured along the
// theme gradient from left to right. Letters it does not know become '?'.
// reveal (0..1) shows only the left part, for wipe-in animations.
func bigText(s string, from, to string, reveal float64) []string {
	s = strings.ToUpper(s)
	// pixel grid
	var grid [5][]bool
	first := true
	for _, r := range s {
		if !unicode.IsPrint(r) {
			continue
		}
		g, ok := glyphs[r]
		if !ok {
			g = glyphs['?']
		}
		for y := 0; y < 5; y++ {
			if !first {
				grid[y] = append(grid[y], false)
			}
			for _, c := range g[y] {
				grid[y] = append(grid[y], c == '#')
			}
		}
		first = false
	}
	w := len(grid[0])
	shown := int(float64(w)*reveal + 0.999)
	rows := make([]string, 3)
	for ry := 0; ry < 3; ry++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			top := grid[ry*2][x]
			bot := ry*2+1 < 5 && grid[ry*2+1][x]
			if x >= shown {
				top, bot = false, false
			}
			col := lerp(from, to, float64(x)/float64(max(w-1, 1)))
			switch {
			case top && bot:
				sb.WriteString(paint(col, "█"))
			case top:
				sb.WriteString(paint(col, "▀"))
			case bot:
				sb.WriteString(paint(col, "▄"))
			default:
				sb.WriteByte(' ')
			}
		}
		rows[ry] = sb.String()
	}
	return rows
}

// bigLines draws s in the pixel font, wrapping words over up to maxLines
// lines of w columns. Words that do not fit even alone are shortened.
func bigLines(s string, w, maxLines int, from, to string, reveal float64) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case bigTextWidth(cur+" "+word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	var out []string
	for i, l := range lines {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, bigText(fitBigText(l, w), from, to, reveal)...)
	}
	return out
}

// fitBigText shortens s until it fits w columns.
func fitBigText(s string, w int) string {
	r := []rune(s)
	for len(r) > 1 && bigTextWidth(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}

// glyphGrid is s in the pixel font as a 5-row bitmap.
func glyphGrid(s string) [5][]bool {
	var grid [5][]bool
	for i, r := range strings.ToUpper(s) {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs['?']
		}
		for y := 0; y < 5; y++ {
			if i > 0 {
				grid[y] = append(grid[y], false)
			}
			for _, c := range g[y] {
				grid[y] = append(grid[y], c == '#')
			}
		}
	}
	return grid
}

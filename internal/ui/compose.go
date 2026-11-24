package ui

// Frames are built by putting text at (row, col). Views may paint over
// earlier text (a cleared row, then content; a dimmed screen, then the
// palette). compose flattens such rows into one segment so the terminal
// receives each cell once and widths stay exact.

import (
	"sort"
	"strings"

	"github.com/mattn/go-runewidth"
)

type cell struct {
	sgr  string // escape sequences in effect, "" for default
	text string // "" for the right half of a wide rune
}

// rasterize writes the visible cells of s into line starting at col.
func rasterize(line []cell, col int, s string) []cell {
	sgr := ""
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			seq := s[i:min(j+1, len(s))]
			if seq == "\x1b[0m" || seq == "\x1b[m" {
				sgr = ""
			} else {
				sgr += seq
			}
			i = j + 1
			continue
		}
		r, size := rune(s[i]), 1
		if s[i] >= 0x80 {
			for _, rr := range s[i:] {
				r = rr
				break
			}
			size = len(string(r))
		}
		w := runewidth.RuneWidth(r)
		i += size
		if w == 0 {
			// combining mark: attach to the previous cell
			if col > 0 && col-1 < len(line) {
				line[col-1].text += string(r)
			}
			continue
		}
		for len(line) < col+w {
			line = append(line, cell{text: " "})
		}
		line[col] = cell{sgr: sgr, text: string(r)}
		if w == 2 {
			line[col+1] = cell{sgr: sgr, text: ""}
		}
		col += w
	}
	return line
}

func encodeCells(line []cell) string {
	var b strings.Builder
	cur := ""
	for _, c := range line {
		if c.text == "" {
			continue
		}
		if c.sgr != cur {
			if cur != "" {
				b.WriteString("\x1b[0m")
			}
			b.WriteString(c.sgr)
			cur = c.sgr
		}
		b.WriteString(c.text)
	}
	if cur != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// compose merges overlapping segments row by row.
func (f *frame) compose() {
	for r, segs := range f.rows {
		if len(segs) < 2 {
			continue
		}
		overlap := false
		sorted := make([]seg, len(segs))
		copy(sorted, segs)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].col < sorted[j].col })
		end := -1
		for _, s := range sorted {
			if s.col < end {
				overlap = true
				break
			}
			end = s.col + width(s.text)
		}
		if !overlap {
			continue
		}
		var line []cell
		for _, s := range segs { // insertion order: later wins
			line = rasterize(line, s.col, s.text)
		}
		// keep leading blanks as columns, not as a segment
		f.rows[r] = []seg{{col: 0, text: encodeCells(line)}}
	}
}

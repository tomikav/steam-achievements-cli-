// Package launcher starts, detects and closes Steam games on this machine.
// Launching goes through the steam:// protocol; detection and closing read
// Steam's own files (libraryfolders.vdf, appmanifest_*.acf) and, on Windows,
// the registry value Steam keeps for the running game.
package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Install describes a game installed through Steam.
type Install struct {
	Installed bool
	Dir       string // full path of the game folder
	SizeBytes int64
}

// Launch starts a game (or opens Steam's install dialog if it is not
// installed) through the steam:// protocol handler.
func Launch(appID int) error {
	return Open(fmt.Sprintf("steam://rungameid/%d", appID))
}

// Open hands a URL (https:// or steam://) to the system handler.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// Lookup finds where a game is installed. It never fails hard: an unknown
// Steam location just reports the game as not installed.
func Lookup(appID int) Install {
	root := steamRoot()
	if root == "" {
		return Install{}
	}
	for _, lib := range libraries(root) {
		data, err := os.ReadFile(filepath.Join(lib, "steamapps", fmt.Sprintf("appmanifest_%d.acf", appID)))
		if err != nil {
			continue
		}
		m := parseKV(string(data))
		dir := m["installdir"]
		if dir == "" {
			continue
		}
		size, _ := strconv.ParseInt(m["sizeondisk"], 10, 64)
		return Install{Installed: true, Dir: filepath.Join(lib, "steamapps", "common", dir), SizeBytes: size}
	}
	return Install{}
}

// libraries lists Steam library folders, the main one first.
func libraries(root string) []string {
	libs := []string{root}
	data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		return libs
	}
	for _, p := range parseLibraryPaths(string(data)) {
		if !strings.EqualFold(filepath.Clean(p), filepath.Clean(root)) {
			libs = append(libs, p)
		}
	}
	return libs
}

var (
	kvRe   = regexp.MustCompile(`"([^"]+)"\s+"((?:[^"\\]|\\.)*)"`)
	pathRe = regexp.MustCompile(`"path"\s+"((?:[^"\\]|\\.)*)"`)
)

// parseKV reads the flat "key" "value" pairs of a VDF/ACF file (keys
// lower-cased; the first occurrence wins).
func parseKV(s string) map[string]string {
	out := map[string]string{}
	for _, m := range kvRe.FindAllStringSubmatch(s, -1) {
		k := strings.ToLower(m[1])
		if _, ok := out[k]; !ok {
			out[k] = unescape(m[2])
		}
	}
	return out
}

func parseLibraryPaths(s string) []string {
	var out []string
	for _, m := range pathRe.FindAllStringSubmatch(s, -1) {
		out = append(out, unescape(m[1]))
	}
	return out
}

func unescape(s string) string { return strings.ReplaceAll(s, `\\`, `\`) }

// FormatSize renders a byte count as "12.3 GB".
func FormatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	case b > 0:
		return fmt.Sprintf("%d KB", b>>10)
	}
	return "-"
}

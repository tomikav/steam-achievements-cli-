//go:build !windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func steamRoot() string {
	home, _ := os.UserHomeDir()
	var candidates []string
	if runtime.GOOS == "darwin" {
		candidates = []string{filepath.Join(home, "Library", "Application Support", "Steam")}
	} else {
		candidates = []string{filepath.Join(home, ".steam", "steam"), filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam")}
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "steamapps")); err == nil {
			return c
		}
	}
	return ""
}

// RunningAppID is not available outside Windows; callers fall back to
// tracking the game they launched.
func RunningAppID() int { return 0 }

// Close stops the processes started from the game's folder.
func Close(appID int) error {
	in := Lookup(appID)
	if !in.Installed {
		return fmt.Errorf("could not find where the game is installed")
	}
	if err := exec.Command("pkill", "-f", in.Dir).Run(); err != nil {
		return fmt.Errorf("the game is not running")
	}
	return nil
}

func steamExe() string {
	if runtime.GOOS == "darwin" {
		return "" // macOS: the steam:// handler is the reliable route
	}
	p, err := exec.LookPath("steam")
	if err != nil {
		return ""
	}
	return p
}

// SteamRunning reports whether a Steam client process exists.
func SteamRunning() bool {
	return exec.Command("pgrep", "-x", "steam").Run() == nil
}

// SteamMemory returns the resident memory of the Steam processes, in bytes.
func SteamMemory() (int64, bool) {
	out, err := exec.Command("ps", "-C", "steam,steamwebhelper", "-o", "rss=").Output()
	if err != nil {
		return 0, false
	}
	var kb int64
	for _, f := range strings.Fields(string(out)) {
		n, _ := strconv.ParseInt(f, 10, 64)
		kb += n
	}
	return kb * 1024, kb > 0
}

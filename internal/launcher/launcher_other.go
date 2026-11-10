//go:build !windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

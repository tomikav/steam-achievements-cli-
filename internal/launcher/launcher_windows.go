//go:build windows

package launcher

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var regValueRe = regexp.MustCompile(`(?m)^\s*(\S+)\s+REG_\w+\s+(.*?)\s*$`)

// regQuery returns a value from the registry using reg.exe (no cgo, no
// extra dependencies).
func regQuery(key, name string) string {
	cmd := exec.Command("reg", "query", key, "/v", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, m := range regValueRe.FindAllStringSubmatch(string(out), -1) {
		if strings.EqualFold(m[1], name) {
			return m[2]
		}
	}
	return ""
}

func steamRoot() string {
	p := regQuery(`HKCU\Software\Valve\Steam`, "SteamPath")
	return strings.ReplaceAll(p, "/", `\`)
}

// RunningAppID reports which game Steam says is running (0 for none).
func RunningAppID() int {
	v := regQuery(`HKCU\Software\Valve\Steam`, "RunningAppID")
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(v), "0x"), 16, 64)
	if err != nil {
		return 0
	}
	return int(n)
}

// Close asks every process running from the game's folder to close its
// window, then force-stops whatever is still running after a few seconds.
func Close(appID int) error {
	in := Lookup(appID)
	if !in.Installed {
		return fmt.Errorf("could not find where the game is installed")
	}
	dir := strings.ReplaceAll(in.Dir, "'", "''")
	script := fmt.Sprintf(`$d='%s';`+
		`$p=Get-Process|Where-Object{$_.Path -and $_.Path.StartsWith($d,[StringComparison]::OrdinalIgnoreCase)};`+
		`if(-not $p){exit 3};`+
		`$p|ForEach-Object{[void]$_.CloseMainWindow()};Start-Sleep -Seconds 4;`+
		`$p|Where-Object{-not $_.HasExited}|Stop-Process -Force`, dir)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 3 {
		return fmt.Errorf("the game is not running")
	}
	return err
}

func steamExe() string {
	return strings.ReplaceAll(regQuery(`HKCU\Software\Valve\Steam`, "SteamExe"), "/", `\`)
}

// SteamRunning reports whether the Steam client is running (visible or in
// the tray).
func SteamRunning() bool {
	v := regQuery(`HKCU\Software\Valve\Steam\ActiveProcess`, "pid")
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(v), "0x"), 16, 64)
	return err == nil && n != 0
}

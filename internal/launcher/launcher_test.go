package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsers(t *testing.T) {
	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"apps" { "620" "123" }
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
	}
}`
	p := parseLibraryPaths(vdf)
	if len(p) != 2 || p[1] != `D:\SteamLibrary` {
		t.Fatalf("paths: %q", p)
	}
	acf := `"AppState"
{
	"appid"		"620"
	"name"		"Portal 2"
	"installdir"		"Portal 2"
	"SizeOnDisk"		"13054912"
}`
	m := parseKV(acf)
	if m["installdir"] != "Portal 2" || m["sizeondisk"] != "13054912" {
		t.Fatalf("kv: %v", m)
	}
}

func TestLookupFromFolder(t *testing.T) {
	root := t.TempDir()
	lib2 := t.TempDir()
	must := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(root, "steamapps", "libraryfolders.vdf"), `"path" "`+root+`" "path" "`+lib2+`"`)
	must(filepath.Join(lib2, "steamapps", "appmanifest_620.acf"), `"installdir" "Portal 2" "SizeOnDisk" "2147483648"`)
	var found Install
	for _, lib := range libraries(root) {
		if _, err := os.Stat(filepath.Join(lib, "steamapps", "appmanifest_620.acf")); err == nil {
			found = Install{Installed: true, Dir: lib}
		}
	}
	if !found.Installed || found.Dir != lib2 {
		t.Fatalf("library scan failed: %+v", found)
	}
	if FormatSize(2147483648) != "2.0 GB" {
		t.Fatal(FormatSize(2147483648))
	}
}

func TestParseAppManifestFields(t *testing.T) {
	m := parseKV(`"AppState" { "appid" "1422450" "name" "Deadlock" "installdir" "Deadlock" "SizeOnDisk" "21474836480" "LastUpdated" "1731000000" }`)
	if m["appid"] != "1422450" || m["name"] != "Deadlock" || m["lastupdated"] != "1731000000" {
		t.Fatalf("%v", m)
	}
}

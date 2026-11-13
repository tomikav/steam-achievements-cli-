package config

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"  ABCDEF  \r\n":      "ABCDEF",
		`"76561198000000000"`: "76561198000000000",
		"https://steamcommunity.com/profiles/76561198000000000/": "76561198000000000",
		"https://steamcommunity.com/id/tomika/":                  "tomika",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidators(t *testing.T) {
	if !ValidKey("0123456789abcdef0123456789ABCDEF") || ValidKey("short") || ValidKey("0123456789abcdef0123456789ABCDEG") {
		t.Error("ValidKey misclassifies")
	}
	if !IsSteamID64("76561198000000000") || IsSteamID64("tomika") || IsSteamID64("7656119800000000") {
		t.Error("IsSteamID64 misclassifies")
	}
}

func TestResolveEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	if err := Save(Config{APIKey: "FILEKEY", SteamID: "76561198000000000"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAPIKey, " ENVKEY \n")
	c, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "ENVKEY" || c.KeySource != "env "+EnvAPIKey {
		t.Errorf("key = %q from %q", c.APIKey, c.KeySource)
	}
	if c.IDSource != SourceFile {
		t.Errorf("id source = %q", c.IDSource)
	}
}

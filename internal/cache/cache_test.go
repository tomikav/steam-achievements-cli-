package cache

import (
	"testing"
	"time"
)

func TestFresh(t *testing.T) {
	now := time.Now().Unix()
	day := int64(24 * 60 * 60)
	cases := []struct {
		name       string
		fetched    int64
		lastPlayed int64
		want       bool
	}{
		{"recent fetch", now - 60, now, true},
		{"old fetch, not played since", now - 10*day, now - 20*day, true},
		{"old fetch, played since", now - 10*day, now - day, false},
		{"old fetch, playtime unknown", now - 10*day, 0, false},
		{"older than max age", now - 40*day, now - 50*day, false},
	}
	for _, c := range cases {
		if got := Fresh(Entry{Fetched: c.fetched}, c.lastPlayed); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if Fresh(Entry{Fetched: now - 2*day, Unavailable: true}, now-30*day) {
		t.Error("unavailable entries must expire after TTL")
	}
	if !Fresh(Entry{Fetched: now - 60, Unavailable: true}, now-30*day) {
		t.Error("fresh unavailable entry should be reused")
	}
}

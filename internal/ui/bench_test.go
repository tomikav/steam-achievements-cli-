package ui

import (
	"testing"
	"time"
)

func benchView(b *testing.B, keys ...string) {
	br := featureBrowser(160, 48)
	lists, _ := br.opt.LoadStoreFront()
	for _, k := range keys {
		br.handleKey(k)
	}
	br.store.lists, br.store.loaded, br.store.loading, br.store.err = lists, true, false, nil
	br.motion, br.started, br.animStart = true, time.Now(), time.Now().Add(-time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = br.build()
	}
}

func BenchmarkFrameHome(b *testing.B)      { benchView(b) }
func BenchmarkFrameLibrary(b *testing.B)   { benchView(b, "l") }
func BenchmarkFrameDashboard(b *testing.B) { benchView(b, "d") }
func BenchmarkFrameStore(b *testing.B)     { benchView(b, "s") }
func BenchmarkFramePalette(b *testing.B)   { benchView(b, "ctrl+k", "e") }

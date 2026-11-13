package icons

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestSixelShape(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 8), 128, 255})
		}
	}
	s := Sixel(img, 18, 18)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;18;18") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("bad framing: %q", s[:20])
	}
	if strings.Count(s, "-") != 3 { // 18 rows = 3 bands of 6
		t.Errorf("bands = %d, want 3", strings.Count(s, "-"))
	}
}

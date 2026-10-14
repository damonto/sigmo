package communication

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestThumbnail(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		wantError     bool
	}{
		{"square", 32, 32, false}, {"landscape", 800, 400, false}, {"oversized", 4097, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := image.NewNRGBA(image.Rect(0, 0, tc.width, tc.height))
			for y := range tc.height {
				for x := range tc.width {
					source.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
				}
			}
			var input bytes.Buffer
			if err := png.Encode(&input, source); err != nil {
				t.Fatal(err)
			}
			data, tint, err := thumbnail(input.Bytes())
			if (err != nil) != tc.wantError {
				t.Fatalf("thumbnail error = %v", err)
			}
			if tc.wantError {
				return
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || format != "png" || config.Width != config.Height || config.Width > 512 || tint != "#ff0000" {
				t.Fatalf("thumbnail = %+v, %s, %q, %v", config, format, tint, err)
			}
		})
	}
	if _, _, err := thumbnail([]byte("not an image")); err == nil {
		t.Fatal("accepted invalid image")
	}
}

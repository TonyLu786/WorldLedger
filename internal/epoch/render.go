package epoch

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"sort"
)

// Palette assigns a colour to each selection status.
//
// The point of the image is the thing a text summary cannot convey: where the
// coverage is. Unknown is drawn as the page background rather than as a colour,
// so gaps read as absence instead of as another category of data. That is the
// same distinction the archive makes internally, carried through to what a
// person looks at.
var Palette = map[Status]color.RGBA{
	StatusCorroborated: {R: 0x2E, G: 0x7D, B: 0x32, A: 0xFF},
	StatusSingleSource: {R: 0x64, G: 0xB5, B: 0xF6, A: 0xFF},
	StatusSuperseded:   {R: 0xFB, G: 0xC0, B: 0x2D, A: 0xFF},
	StatusConflict:     {R: 0xC6, G: 0x28, B: 0x28, A: 0xFF},
}

var unobserved = color.RGBA{R: 0x1B, G: 0x1B, B: 0x1F, A: 0xFF}

// RenderPNG draws one pixel per chunk, scaled by the given factor, and writes a
// PNG.
//
// A chunk never observed is left as background. A chunk observed but with
// nothing at or before the epoch is also background: the archive knows of it,
// but has nothing to say about that moment, and colouring it would assert
// otherwise.
func (s Snapshot) RenderPNG(path string, scale int) error {
	if scale < 1 {
		scale = 1
	}
	drawn := make(map[[2]int32]Status, len(s.Selections))
	for _, selection := range s.Selections {
		if selection.Status == StatusUnknown {
			continue
		}
		drawn[[2]int32{selection.Chunk.X, selection.Chunk.Z}] = selection.Status
	}
	if len(drawn) == 0 {
		return fmt.Errorf("nothing to draw: no chunk has an observation at %s", s.At.Format("2006-01-02T15:04:05Z"))
	}

	minX, minZ, maxX, maxZ := bounds(drawn)
	width := int(maxX-minX+1) * scale
	height := int(maxZ-minZ+1) * scale
	const maxDimension = 16384
	if width > maxDimension || height > maxDimension {
		return fmt.Errorf(
			"the coverage spans %dx%d chunks, which is %dx%d pixels at scale %d; lower the scale",
			maxX-minX+1, maxZ-minZ+1, width, height, scale)
	}

	// The image is a map, so +X is right and +Z is down, matching how the game
	// presents coordinates.
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, unobserved)
		}
	}
	for chunk, status := range drawn {
		shade, known := Palette[status]
		if !known {
			continue
		}
		originX := int(chunk[0]-minX) * scale
		originZ := int(chunk[1]-minZ) * scale
		for dy := 0; dy < scale; dy++ {
			for dx := 0; dx < scale; dx++ {
				canvas.Set(originX+dx, originZ+dy, shade)
			}
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, canvas); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Bounds reports the chunk rectangle the snapshot covers, for callers that want
// to describe the image they just wrote.
func (s Snapshot) Bounds() (minX, minZ, maxX, maxZ int32, ok bool) {
	drawn := make(map[[2]int32]Status, len(s.Selections))
	for _, selection := range s.Selections {
		if selection.Status == StatusUnknown {
			continue
		}
		drawn[[2]int32{selection.Chunk.X, selection.Chunk.Z}] = selection.Status
	}
	if len(drawn) == 0 {
		return 0, 0, 0, 0, false
	}
	minX, minZ, maxX, maxZ = bounds(drawn)
	return minX, minZ, maxX, maxZ, true
}

func bounds(drawn map[[2]int32]Status) (minX, minZ, maxX, maxZ int32) {
	keys := make([][2]int32, 0, len(drawn))
	for key := range drawn {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] == keys[j][0] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})

	minX, maxX = keys[0][0], keys[0][0]
	minZ, maxZ = keys[0][1], keys[0][1]
	for _, key := range keys {
		if key[0] < minX {
			minX = key[0]
		}
		if key[0] > maxX {
			maxX = key[0]
		}
		if key[1] < minZ {
			minZ = key[1]
		}
		if key[1] > maxZ {
			maxZ = key[1]
		}
	}
	return minX, minZ, maxX, maxZ
}

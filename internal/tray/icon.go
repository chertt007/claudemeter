package tray

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
)

const maxGauges = 4

var (
	trackColor  = rgb(0x4B5563)
	levelColors = map[Level]color.NRGBA{
		OK:       rgb(0x4ADE80),
		Warn:     rgb(0xFACC15),
		Critical: rgb(0xEF4444),
	}
	iconSizes = []int{16, 32, 48}
)

func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{uint8(hex >> 16), uint8(hex >> 8), uint8(hex), 0xFF}
}

// Icon renders one vertical bar per gauge, filled from the bottom, as an
// .ico file with 16, 32 and 48 px images.
func Icon(gauges []Gauge) []byte {
	var imgs [][]byte
	for _, s := range iconSizes {
		var b bytes.Buffer
		_ = png.Encode(&b, render(gauges, s))
		imgs = append(imgs, b.Bytes())
	}

	var out bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&out, le, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for i, data := range imgs {
		_ = binary.Write(&out, le, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{uint8(iconSizes[i]), uint8(iconSizes[i]), 0, 0, 1, 32, uint32(len(data)), uint32(offset)})
		offset += len(data)
	}
	for _, data := range imgs {
		out.Write(data)
	}
	return out.Bytes()
}

// iconKey changes only when the rendered icon would.
func iconKey(gauges []Gauge) string {
	var b strings.Builder
	for _, g := range gauges {
		fmt.Fprintf(&b, "%d:%d,", g.Level, filled(g, 48))
	}
	return b.String()
}

// filled is the number of lit pixel rows out of h.
func filled(g Gauge, h int) int {
	if g.Level == Unknown {
		return 0
	}
	p := math.Max(0, math.Min(100, g.Pct))
	n := int(math.Round(p / 100 * float64(h)))
	if p > 0 && n == 0 {
		n = 1 // any usage at all stays visible
	}
	return n
}

// columns lays out n bars on a 16-unit grid: x offset and width of each.
func columns(n int) [][2]int {
	gap := 2
	if n > 2 {
		gap = 1
	}
	w := (16 - 2 - gap*(n-1)) / n
	x := (16 - (w*n + gap*(n-1))) / 2
	cols := make([][2]int, n)
	for i := range cols {
		cols[i] = [2]int{x, w}
		x += w + gap
	}
	return cols
}

func render(gauges []Gauge, size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	if len(gauges) > maxGauges {
		gauges = gauges[:maxGauges]
	}
	if len(gauges) == 0 {
		gauges = []Gauge{{Level: Unknown}}
	}
	k := size / 16
	for i, c := range columns(len(gauges)) {
		g := gauges[i]
		lit := size - filled(g, size)
		for y := 0; y < size; y++ {
			col := trackColor
			if y >= lit {
				col = levelColors[g.Level]
			}
			for x := c[0] * k; x < (c[0]+c[1])*k; x++ {
				img.SetNRGBA(x, y, col)
			}
		}
	}
	return img
}

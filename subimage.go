package ctxmenu

import (
	"image"
	"image/color"
	"image/draw"
)

// SubImage is a wrapper to offset an draw.Image to specific Boundaries
type SubImage struct {
	Src  draw.Image
	Rect image.Rectangle
}

// At returns the color of the pixel at (x, y).
// At(Bounds().Min.X, Bounds().Min.Y) returns the upper-left pixel of the grid.
// At(Bounds().Max.X-1, Bounds().Max.Y-1) returns the lower-right one.
func (si *SubImage) At(x int, y int) color.Color {
	if x < 0 || x >= si.Rect.Dx() || y < 0 || y >= si.Rect.Dy() {
		return nil
	}
	return si.Src.At(si.Rect.Min.X+x, si.Rect.Min.Y+y)
}

// Set modifies the color of the pixel at (x, y).
func (si *SubImage) Set(x, y int, c color.Color) {
	if x < 0 || x >= si.Rect.Dx() || y < 0 || y >= si.Rect.Dy() {
		return
	}
	si.Src.Set(si.Rect.Min.X+x, si.Rect.Min.Y+y, c)
}

// Bounds returns the domain for which At can return non-zero color.
// The bounds do not necessarily contain the point (0, 0).
func (si *SubImage) Bounds() image.Rectangle {
	return image.Rect(0, 0, si.Rect.Dx(), si.Rect.Dy())
}

// ColorModel returns the Image's color model.
func (si *SubImage) ColorModel() color.Model {
	return si.Src.ColorModel()
}

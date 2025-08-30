package ctxmenu

import (
	"image"
	"image/draw"
)

type Dir int

const (
	DirRight Dir = iota
	DirLeft
	DirUp
	DirDown
)

// centralize arrow, this means calculate its size (w, h) by applying the correct ratio and set an offset if out of center
func centralize(bounds image.Rectangle, dir Dir) (x, y, w, h int) {
	w, h = bounds.Dx(), bounds.Dy()

	switch dir {
	case DirLeft, DirRight:
		nh := w*2 + 1
		if nh <= h {
			y = (h - nh) / 2
			h = nh
		} else {
			nw := (h - 1) / 2
			x = (w - nw) / 2
			w = nw
		}
	case DirUp, DirDown:
		nw := h*2 + 1
		if nw <= w {
			x = (w - nw) / 2
			w = nw
		} else {
			nh := (w - 1) / 2
			y = (h - nh) / 2
			h = nh
		}
	}
	return
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func DrawArrow(dst draw.Image, bounds image.Rectangle, src image.Image, srcp image.Point, dir Dir) {
	if bounds.Empty() {
		return
	}
	offX, offY, w, h := centralize(bounds, dir)
	bm := bounds.Min

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ok := false
			switch dir {
			case DirRight:
				ok = abs(y-w) <= x // vertical hypotenuse on left
			case DirLeft:
				ok = abs(y-w) <= w-x // vertical hypotenuse on right
			case DirUp:
				ok = abs(x-h) <= y // horizontal hypotenuse on bottom
			case DirDown:
				ok = abs(x-h) <= h-y // horizontal hypotenuse on top
			}
			if ok {
				dx, dy := bm.X+offX+x, bm.Y+offY+y
				sx, sy := srcp.X+offX+x, srcp.Y+offY+y
				dst.Set(dx, dy, src.At(sx, sy))
			}
		}
	}
}

package ctxmenu

import (
	"image"
	"image/draw"
	"os"

	xdraw "golang.org/x/image/draw"
)

/* itemState is an element inside a Menu */
type itemState[T comparable] interface {
	Id() T
	Parent() *menuState[T]
	GetSubMenu() *menuState[T]
	Geometry() (int, int)
	Draw(draw.Image, image.Rectangle, ColorPair)
	Selectable() bool
}

type labelItem[T comparable] struct {
	Item[T]
	ctxmenu  *ContextMenu
	menu     *menuState[T] /* parent */
	labeltex draw.Image
	submenu  *menuState[T] /* submenu spawned by clicking on item */
	icon     draw.Image

	w, h int /* item geometry */
}

func newLabelItem[T comparable](menu *menuState[T], orig Item[T]) (itemState[T], error) {
	item := &labelItem[T]{
		Item:    orig,
		ctxmenu: menu.ctxmenu,
		menu:    menu,
	}

	item.w = menu.ctxmenu.PaddingX * 2

	item.w += menu.ctxmenu.measureText(item.Label)
	item.h = menu.ctxmenu.font.Metrics().Height.Ceil() + menu.ctxmenu.PaddingY*2

	/* try to load icon */
	if item.Imagefile != "" && !menu.ctxmenu.DisableIcons {
		dec, err := getDecoder(item.Imagefile)
		if err != nil {
			return nil, err
		}

		r, err := os.Open(item.Imagefile)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		img, err := dec(r)
		if err != nil {
			return nil, err
		}

		dst := image.NewRGBA(image.Rect(0, 0, menu.ctxmenu.IconSize, menu.ctxmenu.IconSize))
		item.icon = dst

		// Ugh, NearestNeighbor is ugly... but really fast and suits the case as I want to create small 30x30 (or so) icons
		xdraw.NearestNeighbor.Scale(dst, dst.Rect, img, img.Bounds(), draw.Src, nil)
		item.w += menu.ctxmenu.IconSize + menu.ctxmenu.PaddingX
		item.h = max(item.h, menu.ctxmenu.IconSize+menu.ctxmenu.PaddingY*2)
	}
	if len(item.SubMenu) > 0 {
		item.w += menu.ctxmenu.SubmenuArrowWidth
		var err error
		item.submenu, err = item.SubMenu.makeMenu(menu.ctxmenu, item)
		if err != nil {
			return nil, err
		}
	}

	return item, nil
}

func (item *labelItem[T]) Selectable() bool {
	return true
}
func (item *labelItem[T]) Parent() *menuState[T] {
	return item.menu
}
func (item *labelItem[T]) Id() T {
	return item.Output
}
func (item *labelItem[T]) GetSubMenu() *menuState[T] {
	return item.submenu
}
func (item *labelItem[T]) Geometry() (int, int) {
	return item.w, item.h
}
func (item *labelItem[T]) Draw(dst draw.Image, bounds image.Rectangle, color ColorPair) {
	img := &SubImage{dst, bounds}

	draw.Draw(img, img.Bounds(), color.Background, image.Point{}, draw.Src)

	x := item.ctxmenu.PaddingX + item.ctxmenu.BorderSize
	if item.icon != nil {
		x += item.ctxmenu.IconSize + item.ctxmenu.PaddingX
	}

	textH := item.ctxmenu.font.Metrics().Height.Ceil()
	textW := item.ctxmenu.measureText(item.Label)
	if item.labeltex == nil {
		item.labeltex = image.NewAlpha(image.Rect(0, 0, textW, textH))
		item.ctxmenu.drawText(item.labeltex, item.Label)
	}
	textY := item.h/2 - textH/2

	draw.DrawMask(img, item.labeltex.Bounds().Add(image.Point{x, textY}), color.Foreground, image.Point{}, item.labeltex, image.Point{}, draw.Over)

	if item.submenu != nil {
		x := bounds.Dx() - item.ctxmenu.SubmenuArrowWidth - item.ctxmenu.SubmenuArrowMargin - item.ctxmenu.BorderSize - item.ctxmenu.PaddingX

		DrawArrow(img, image.Rect(x, 0, x+item.ctxmenu.SubmenuArrowWidth, item.h-item.ctxmenu.SubmenuArrowMargin*2), color.Foreground, image.Point{}, DirRight)
	}

	if item.icon != nil {
		x := item.ctxmenu.BorderSize + item.ctxmenu.PaddingX
		y := item.h/2 - item.ctxmenu.IconSize/2
		draw.Draw(img, image.Rect(x, y, x+item.ctxmenu.IconSize, y+item.ctxmenu.IconSize), item.icon, image.Point{}, draw.Over)
	}

}

type overflowItem[T comparable] struct {
	ctxmenu *ContextMenu
	menu    *menuState[T] /* parent */
	dir     Dir
}

func newOverflowItem[T comparable](menu *menuState[T], dir Dir) itemState[T] {
	item := &overflowItem[T]{
		ctxmenu: menu.ctxmenu,
		menu:    menu,
		dir:     dir,
	}

	return item
}

func (item *overflowItem[T]) Selectable() bool {
	return false
}
func (item *overflowItem[T]) Parent() *menuState[T] {
	return item.menu
}
func (item *overflowItem[T]) Id() (def T) {
	return def
}
func (item *overflowItem[T]) GetSubMenu() *menuState[T] {
	return nil
}
func (item *overflowItem[T]) Geometry() (int, int) {
	return 0, item.ctxmenu.OverflowArrowHeight + item.ctxmenu.PaddingY*2
}
func (item *overflowItem[T]) Draw(dst draw.Image, bounds image.Rectangle, color ColorPair) {
	m := item.ctxmenu.OverflowArrowMargin
	r := bounds
	r.Min = r.Min.Add(image.Point{m, m})
	r.Max = r.Max.Sub(image.Point{m, m})

	draw.Draw(dst, bounds, color.Background, image.Point{}, draw.Src)
	DrawArrow(dst, r, color.Foreground, image.Point{}, item.dir)
}

type separatorItem[T comparable] struct {
	ctxmenu *ContextMenu
	menu    *menuState[T] /* parent */
}

func newseparatorItem[T comparable](menu *menuState[T], dir Dir) itemState[T] {
	item := &separatorItem[T]{
		ctxmenu: menu.ctxmenu,
		menu:    menu,
	}

	return item
}

func (item *separatorItem[T]) Selectable() bool {
	return false
}
func (item *separatorItem[T]) Parent() *menuState[T] {
	return item.menu
}
func (item *separatorItem[T]) Id() (def T) {
	return def
}
func (item *separatorItem[T]) GetSubMenu() *menuState[T] {
	return nil
}
func (item *separatorItem[T]) Geometry() (int, int) {
	w := item.ctxmenu.PaddingX * 2
	h := 1 + item.ctxmenu.PaddingY*2
	return w, h
}
func (item *separatorItem[T]) Draw(dst draw.Image, bounds image.Rectangle, color ColorPair) {
	x := bounds.Min.X + item.ctxmenu.BorderSize + item.ctxmenu.PaddingX + item.ctxmenu.SeperatorLength
	y := bounds.Min.Y + item.ctxmenu.PaddingY
	draw.Draw(dst, image.Rect(x, y, x+bounds.Dx()-x*2, y+1), item.ctxmenu.separator, image.Point{}, draw.Src)
}

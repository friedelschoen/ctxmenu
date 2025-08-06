package ctxmenu

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"iter"
	"os"
	"strconv"
	"time"
	"unicode"

	"github.com/veandco/go-sdl2/img"
	"github.com/veandco/go-sdl2/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type Action int

const (
	ActionClear Action = 1 << iota /* clear text */
	ActionMap                      /* remap menu windows */
	ActionDraw                     /* redraw menu windows */
	ActionWarp                     /* warp the pointer */
)

/* enum for keyboard menu navigation */
const (
	ItemPrev = iota
	ItemNext
	ItemFirst
	ItemLast
)

type Alignment int

/* enum for text alignment */
const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
)

/* ColorPair holds text-color information */
type ColorPair struct {
	Foreground, Background *color.NRGBA
}

/* Config holds configurations for ctxmenu */
type Config struct {
	/* the values below are set by menu.ctxmenu.h */
	FontName           string
	BackgroundColor    string
	ForegroundColor    string
	SelbackgroundColor string
	SelforegroundColor string
	SeparatorColor     string
	BorderColor        string

	MinItemWidth       int
	BorderSize         int
	SeperatorLength    int
	IconSize           int
	PaddingX, PaddingY int
	Alignment          Alignment
}

var ErrExited = errors.New("window was closed")

type OverflowItem int

const (
	OverflowTop OverflowItem = iota - 1
	OverflowNone
	OverflowBottom
)

/* Item is an element inside a Menu */
type Item[T comparable] struct {
	parent     *Menu[T] /* parent */
	output     T        /* string to be outputed when item is clicked */
	label      string   /* string to be drawed on menu */
	labeltex   *sdl.Texture
	submenu    *Menu[T] /* submenu spawned by clicking on item */
	icon       *sdl.Surface
	icontex    *sdl.Texture
	overflower OverflowItem

	w, h int /* item geometry */
}

/* Menu is a menu- or submenu-window */
type Menu[T comparable] struct {
	ctxmenu      *ContextMenu  /* context */
	items        []*Item[T]    /* list of items contained by the menu */
	first        int           /* index of first element, if scrolled */
	selected     int           /* index of item currently selected in the menu */
	overflow     int           /* index of first item out of sight, -1 if not overflowing */
	x, y         int           /* menu position */
	w, h         int           /* geometry */
	win          *sdl.Window   /* menu window to map on the screen */
	render       *sdl.Renderer /* hardware-accelerated renderer */
	caller       *Menu[T]      /* current parent of this window, nil if root-window */
	itemsChanged bool          /*  */

	overflowItemTop    *Item[T]
	overflowItemBottom *Item[T]
}

type ContextMenu struct {
	Config

	normal    ColorPair
	selected  ColorPair
	border    *color.NRGBA
	separator *color.NRGBA

	font font.Face

	/* flags */
	disableIcons bool /* whether to disable icons */

	seen bool /* if the cursor is seen above menu */
}

func parseFontString(s string) (font.Face, error) {
	path, opts, err := FontMatch(s)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fnt, err := opentype.Parse(content)
	if err != nil {
		return nil, err
	}

	return opentype.NewFace(fnt, opts)
}

func parseColor(s string) (*color.NRGBA, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("empty color")
	}
	if s[0] == '#' {
		s = s[1:]
	}
	switch len(s) {
	case 3:
		s = string([]byte{
			s[0], s[0],
			s[1], s[1],
			s[2], s[2],
			'f', 'f',
		})
	case 4:
		s = string([]byte{
			s[0], s[0],
			s[1], s[1],
			s[2], s[2],
			s[3], s[3],
		})
	case 6:
		s += "ff"
	case 8:
		/* do nothing */
	default:
		return nil, fmt.Errorf("invalid color: %s", s)
	}
	r, err := strconv.ParseUint(s[0:2], 16, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid color: %s", s)
	}
	g, err := strconv.ParseUint(s[2:4], 16, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid color: %s", s)
	}
	b, err := strconv.ParseUint(s[4:6], 16, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid color: %s", s)
	}
	a, err := strconv.ParseUint(s[6:8], 16, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid color: %s", s)
	}
	return &color.NRGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(a),
	}, nil
}

/* MakeMenu allocates a menu and create its window */
func MakeMenu[T comparable](ctxmenu *ContextMenu) *Menu[T] {
	// XSetWindowAttributes swa;
	menu := Menu[T]{
		ctxmenu: ctxmenu,
	}
	menu.x = -1
	menu.y = -1

	/* ignoring error as an error only happens with icons */
	menu.overflowItemTop = menu.makeOverflow(true)
	menu.overflowItemBottom = menu.makeOverflow(false)

	return &menu
}

func (menu *Menu[T]) Append(label string, output T, imagefile string, depth int) error {
	for range depth {
		if len(menu.items) == 0 {
			return fmt.Errorf("too much depth")
		}
		tail := menu.items[len(menu.items)-1]
		if tail.submenu == nil {
			sub := MakeMenu[T](menu.ctxmenu)
			tail.setSubmenu(sub)
		}
		menu = tail.submenu
	}

	err := menu.AppendItem(label, output, imagefile)
	if err != nil {
		return err
	}

	return nil
}

func (menu *Menu[T]) makeItem(label string, output T, imagefile string) (*Item[T], error) {
	item := Item[T]{
		parent: menu,
		label:  label,
		output: output,
	}

	item.w = menu.ctxmenu.PaddingX * 2

	if label == "" {
		item.h = 1 + menu.ctxmenu.PaddingY*2
		return &item, nil
	}

	item.w += menu.ctxmenu.messureText(label)
	item.h = menu.ctxmenu.font.Metrics().Height.Ceil() + menu.ctxmenu.PaddingY*2

	/* try to load icon */
	if imagefile != "" && !menu.ctxmenu.disableIcons {
		var err error
		item.icon, err = img.Load(imagefile)
		if err != nil {
			return nil, err
		}
		item.w += menu.ctxmenu.IconSize + menu.ctxmenu.PaddingX
		item.h = max(item.h, menu.ctxmenu.IconSize+menu.ctxmenu.PaddingY*2)
	}
	return &item, nil
}

func (menu *Menu[T]) makeOverflow(top bool) *Item[T] {
	item := Item[T]{
		parent: menu,
	}

	item.overflower = OverflowBottom
	if top {
		item.overflower = OverflowTop
	}
	item.w = topBottomSize.X + menu.ctxmenu.PaddingX*2
	item.h = topBottomSize.Y + menu.ctxmenu.PaddingY*2
	return &item
}

func (menu *Menu[T]) AppendItem(label string, output T, imagefile string) error {
	item, err := menu.makeItem(label, output, imagefile)
	if err != nil {
		return err
	}
	menu.items = append(menu.items, item)
	menu.itemsChanged = true
	return nil
}

func (item *Item[T]) setSubmenu(sub *Menu[T]) {
	item.w += leftRightSize.X
	item.parent.w = max(item.parent.w, item.w)
	item.submenu = sub
}

func (ctxmenu *ContextMenu) drawText(dest draw.Image, text string) int {
	var dot fixed.Point26_6
	dot.X = 0
	dot.Y = ctxmenu.font.Metrics().Ascent

	prev := rune(-1)
	for _, chr := range text {
		if prev != -1 {
			dot.X += ctxmenu.font.Kern(prev, chr)
		}
		prev = chr
		dr, mask, maskp, advance, _ := ctxmenu.font.Glyph(dot, chr)
		draw.DrawMask(dest, dr, image.Opaque, image.Point{}, mask, maskp, draw.Over)
		dot.X += advance
	}
	return dot.X.Ceil()
}

func (ctxmenu *ContextMenu) messureText(text string) int {
	prev := rune(-1)
	width := fixed.Int26_6(0)
	for _, chr := range text {
		if prev != -1 {
			width += ctxmenu.font.Kern(prev, chr)
		}
		prev = chr
		advance, _ := ctxmenu.font.GlyphAdvance(chr)
		width += advance
	}
	return width.Ceil()
}

func (menu *Menu[T]) updateWindow() error {
	var err error
	if menu.win == nil {
		menu.win, err = sdl.CreateWindow("menu", int32(menu.x), int32(menu.y), int32(menu.w), int32(menu.h), sdl.WINDOW_SHOWN|sdl.WINDOW_POPUP_MENU)
		if err != nil {
			return err
		}
		menu.render, err = sdl.CreateRenderer(menu.win, -1, sdl.RENDERER_ACCELERATED)
		if err != nil {
			return err
		}
	} else {
		menu.win.SetSize(int32(menu.w), int32(menu.h))
		menu.win.SetPosition(int32(menu.x), int32(menu.y))
		menu.win.Show()
	}

	return nil
}

/* setup the position of a menu */
func (menu *Menu[T]) show(caller *Menu[T]) error {
	if caller == menu {
		caller = nil
	}
	menu.hideChildren(nil)
	if caller != nil {
		caller.hideChildren(menu)
	}

	display, err := menu.win.GetDisplayIndex()
	if err != nil {
		sdl.PumpEvents()
		x, y, _ := sdl.GetGlobalMouseState()
		nmon, err := sdl.GetNumVideoDisplays()
		if err != nil || nmon == -1 {
			display = 0
		} else {
			for i := range nmon {
				mr, err := sdl.GetDisplayBounds(i)
				if err != nil {
					continue
				}
				if x >= mr.X && x < mr.X+mr.W &&
					y >= mr.Y && y < mr.Y+mr.H {
					display = i
					break
				}
			}
		}
	}

	mr, err := sdl.GetDisplayBounds(display)
	if err != nil {
		return err
	}

	if menu.itemsChanged {
		menu.itemsChanged = false
		menu.w = menu.ctxmenu.BorderSize*2 + menu.ctxmenu.MinItemWidth
		menu.h = menu.ctxmenu.BorderSize * 2
		menu.first = 0
		menu.overflow = -1

		for _, item := range menu.items {
			menu.w = max(menu.w, item.w)
			menu.h += item.h
		}

		if menu.h > int(mr.Y+mr.H) {
			/* both arrow items */
			menu.h = (topBottomSize.Y + menu.ctxmenu.PaddingY*2 + menu.ctxmenu.BorderSize) * 2
			for i, item := range menu.items {
				if item.h+menu.h > int(mr.Y+mr.H) {
					menu.overflow = i
					break
				}
				menu.w = max(menu.w, item.w)
				menu.h += item.h
			}
		}
	}

	if caller != nil && menu.caller != caller {
		menu.caller = caller
		menu.x = caller.x + caller.w

		if menu.x < int(mr.X) {
			menu.x = int(mr.X)
		} else if menu.x+menu.w > int(mr.X+mr.W) {
			menu.x = caller.x - menu.w
		}
		if menu.overflow == -1 {
			menu.y = caller.y
			start := 0
			if caller.overflow != -1 {
				start = caller.first
			}
			for i := start; i < caller.selected; i++ {
				menu.y += caller.items[i].h
			}
		}
	} else if menu.x == -1 || menu.y == -1 {
		curX, curY, _ := sdl.GetGlobalMouseState()
		menu.x = int(curX)
		menu.y = 0
		if menu.overflow == -1 {
			menu.y = int(curY)
		}
	}

	if menu.x < int(mr.X) {
		menu.x = int(mr.X)
	} else if menu.x+menu.w > int(mr.X+mr.W) {
		menu.x = int(mr.X+mr.W) - menu.w
	}
	if menu.y < int(mr.Y) {
		menu.y = int(mr.Y)
	} else if menu.y+menu.h > int(mr.Y+mr.H) {
		menu.y = int(mr.Y+mr.H) - menu.h
	}

	menu.updateWindow()
	return nil
}

func (menu *Menu[T]) hideChildren(except *Menu[T]) {
	for _, item := range menu.items {
		if item.submenu != nil && item.submenu != except {
			item.submenu.hide()
		}
	}
}

func (menu *Menu[T]) hide() {
	menu.hideChildren(nil)
	menu.win.Hide()
}

/* draw overflow button */
func (menu *Menu[T]) drawItem(y int, index int, item *Item[T]) error {
	// x := menu.ctxmenu.vertpadding
	// y += menu.ctxmenu.horzpadding

	color := menu.ctxmenu.normal
	if index != -1 && index == menu.selected {
		color = menu.ctxmenu.selected
	}

	menu.render.SetDrawColor(color.Background.R, color.Background.G, color.Background.B, color.Background.A)
	menu.render.FillRect(&sdl.Rect{X: 0, Y: int32(y), W: int32(menu.w), H: int32(item.h)})

	menu.render.SetDrawColor(color.Foreground.R, color.Foreground.G, color.Foreground.B, color.Foreground.A)

	if item.overflower != OverflowNone {
		pixels := topArrow
		if item.overflower == OverflowBottom {
			pixels = bottomArrow
		}

		x := menu.w/2 - topBottomSize.X/2
		y := y + item.h/2 - topBottomSize.Y/2
		for i, pix := range pixels {
			offx, offy := i%topBottomSize.X, i/topBottomSize.X
			if pix > 0 {
				menu.render.DrawPoint(int32(x+offx), int32(y+offy))
			}
		}
	} else if item.label != "" {
		x := menu.ctxmenu.PaddingX + menu.ctxmenu.BorderSize
		if item.icon != nil {
			x += menu.ctxmenu.IconSize + menu.ctxmenu.PaddingX
		}

		textH := menu.ctxmenu.font.Metrics().Height.Ceil()
		textW := menu.ctxmenu.messureText(item.label)
		if item.labeltex == nil {
			surf, err := sdl.CreateRGBSurface(0, int32(textW), int32(textH), 32, 0xff000000, 0x00ff0000, 0x0000ff00, 0x000000ff)
			if err != nil {
				return err
			}

			surf.FillRect(&sdl.Rect{W: int32(textW), H: int32(textH)}, 0x00)
			menu.ctxmenu.drawText(surf, item.label)

			item.labeltex, err = menu.render.CreateTextureFromSurface(surf)
			if err != nil {
				return err
			}
		}
		textY := item.h/2 - textH/2
		item.labeltex.SetColorMod(color.Foreground.R, color.Foreground.G, color.Foreground.B)
		menu.render.Copy(item.labeltex, nil, &sdl.Rect{X: int32(x), Y: int32(y + textY), W: int32(textW), H: int32(textH)})

		if item.submenu != nil {
			x := menu.w - leftRightSize.X - menu.ctxmenu.BorderSize - menu.ctxmenu.PaddingX
			y := y + item.h/2 - leftRightSize.Y/2
			for i, pix := range rightArrow {
				offx, offy := i%leftRightSize.X, i/leftRightSize.X
				if pix > 0 {
					menu.render.DrawPoint(int32(x+offx), int32(y+offy))
				}
			}
		}

		if item.icon != nil {
			if item.icontex == nil {
				var err error
				item.icontex, err = menu.render.CreateTextureFromSurface(item.icon)
				if err != nil {
					return err
				}
			}

			x := menu.ctxmenu.BorderSize + menu.ctxmenu.PaddingX
			y := y + item.h/2 - menu.ctxmenu.IconSize/2
			menu.render.Copy(item.icontex, nil, &sdl.Rect{X: int32(x), Y: int32(y), W: int32(menu.ctxmenu.IconSize), H: int32(menu.ctxmenu.IconSize)})
		}
	} else {
		x := menu.ctxmenu.BorderSize + menu.ctxmenu.PaddingX + menu.ctxmenu.SeperatorLength
		y := y + menu.ctxmenu.PaddingY
		menu.render.SetDrawColor(menu.ctxmenu.separator.R, menu.ctxmenu.separator.G, menu.ctxmenu.separator.B, menu.ctxmenu.separator.A)
		menu.render.FillRect(&sdl.Rect{X: int32(x), Y: int32(y), W: int32(menu.w - x*2), H: int32(1)})
	}
	return nil
}

func (menu *Menu[T]) visibleItems(withOverflow bool) iter.Seq2[int, *Item[T]] {
	return func(yield func(int, *Item[T]) bool) {
		if withOverflow && menu.overflow != -1 {
			if !yield(-1, menu.overflowItemTop) {
				return
			}
		}
		start := 0
		end := len(menu.items)
		if menu.overflow != -1 {
			start = menu.first
			end = menu.first + menu.overflow
		}
		for i := start; i < end; i++ {
			if !yield(i, menu.items[i]) {
				return
			}
		}
		if withOverflow && menu.overflow != -1 {
			if !yield(-1, menu.overflowItemBottom) {
				return
			}
		}
	}
}

/* draw pixmap for the selected and unselected version of each item on menu */
func (menu *Menu[T]) draw() error {
	y := menu.ctxmenu.BorderSize

	for i, item := range menu.visibleItems(true) {
		menu.drawItem(y, i, item)
		y += item.h
	}

	menu.render.SetDrawColor(menu.ctxmenu.border.R, menu.ctxmenu.border.G, menu.ctxmenu.border.B, menu.ctxmenu.border.A)
	/* draw border */
	for s := range menu.ctxmenu.BorderSize {
		menu.render.DrawRect(&sdl.Rect{
			X: int32(s),
			Y: int32(s),
			W: int32(menu.w - s*2),
			H: int32(menu.h - s*2),
		})
	}
	menu.render.Present()
	return nil
}

/* get menu of given window */
func (menu *Menu[T]) getmenu(win uint32) *Menu[T] {
	if menu == nil {
		return nil
	}
	if menu.win != nil {
		id, err := menu.win.GetID()
		if err == nil && id == win {
			return menu
		}
	}
	for _, item := range menu.items {
		w := item.submenu.getmenu(win)
		if w != nil {
			return w
		}
	}
	return nil
}

/* get in *ret the item in given menu and position; return 1 if position is on a scroll triangle */
func (menu *Menu[T]) getitem(target int) int {
	y := menu.ctxmenu.BorderSize

	for i, item := range menu.visibleItems(true) {
		if i != -1 && y <= target && target < y+item.h {
			return i
		}
		y += item.h
	}

	return -1
}

func (menu *Menu[T]) isoverflowitem(target int) OverflowItem {
	if menu == nil || menu.overflow == -1 {
		return OverflowNone
	}
	y := menu.ctxmenu.BorderSize

	item := menu.overflowItemTop
	if y <= target && target < y+item.h {
		return OverflowTop
	}
	y += item.h

	for _, item := range menu.visibleItems(false) {
		y += item.h
	}

	item = menu.overflowItemBottom
	if y <= target && target < y+item.h {
		return OverflowBottom
	}

	return OverflowNone
}

/* cycle through the items; non-zero direction is next, zero is prev */
func (menu *Menu[T]) itemcycle(direction int) int {
	/* menu.selected item (either separator or labeled item) in given direction */
	item := -1
	switch direction {
	case ItemNext:
		if menu.selected == -1 {
			item = 0
		} else if menu.selected < len(menu.items)-1 {
			item = menu.selected + 1
		}
	case ItemPrev:
		if menu.selected == -1 {
			item = len(menu.items) - 1
		} else if menu.selected >= 0 {
			item = menu.selected - 1
		}
	case ItemFirst:
		item = 0
	case ItemLast:
		item = len(menu.items) - 1
	}

	/*
	 * the selected item can be a separator
	 * let's menu.selected the closest labeled item (ie., one that isn't a separator)
	 */
	switch direction {
	case ItemNext:
	case ItemFirst:
		for ; item < len(menu.items) && menu.items[item].label == ""; item++ {
		}
		if menu.items[item].label == "" {
			item = 0
		}
	case ItemPrev:
	case ItemLast:
		for ; item >= 0 && menu.items[item].label == ""; item-- {
		}
		if menu.items[item].label == "" {
			item = len(menu.items) - 1
		}
	}
	return item
}

/* get item in menu matching text from given direction (or from beginning, if dir = 0) */
func (menu *Menu[T]) matchitem(text string, dir int) int {
	// struct Item *item, *lastitem;
	dirinc := 0
	switch {
	case dir < 0:
		dirinc = -1
	case dir >= 0:
		dirinc = 1
	}

	item := -1
	if dir < 0 {
		if menu.selected != -1 && menu.selected > 0 {
			item = menu.selected - 1
		} else {
			item = len(menu.items) - 1
		}
	} else if dir > 0 {
		if menu.selected != -1 && menu.selected < len(menu.items)-1 {
			item = menu.selected + 1
		} else {
			item = 0
		}
	} else {
		item = 0
	}
	/* find next item from selected item */

	for ; item >= 0 && item < len(menu.items); item += dirinc {
		for s := menu.items[item].label; len(s) > 0; s = s[1:] {
			if s == text {
				return item
			}
		}
	}
	/* if not found, try to find from the beginning/end of list */
	if dir > 0 {
		item = 0
	} else {
		item = len(menu.items) - 1
	}
	for ; item >= 0 && item < len(menu.items); item += dirinc {
		for s := menu.items[item].label; len(s) > 0; s = s[1:] {
			if s == text {
				return item
			}
		}
	}
	return -1
}

func (menu *Menu[T]) warp() bool {
	y := menu.ctxmenu.BorderSize
	for i, item := range menu.visibleItems(true) {
		if i != -1 && i == menu.selected {
			y += menu.y + item.h/2
			x := menu.x + menu.w/2
			sdl.WarpMouseGlobal(int32(x), int32(y))
			return true
		}
		y += item.h
	}
	return false
}

/* run event loop */
func (rootmenu *Menu[T]) Run(hover func(T)) (def T, err error) {
	if err := rootmenu.show(nil); err != nil {
		return def, err
	}

	curmenu := rootmenu
	var buf []byte
	var previtem *Item[T]
	// curmenu.selected := -1
	var hasleft *time.Timer
	warped := false
	action := Action(0)
	quit := make(chan struct{})
	for {
		select {
		case <-quit:
			return def, ErrExited
		default:
		}
		event := sdl.WaitEventTimeout(100)
		if event == nil {
			continue
		}
		action = 0
		switch ev := event.(type) {
		case *sdl.QuitEvent:
			return def, ErrExited
		case *sdl.WindowEvent:
			if ev.Event == sdl.WINDOWEVENT_LEAVE && rootmenu.ctxmenu.seen {
				hasleft = time.AfterFunc(100*time.Millisecond, func() {
					quit <- struct{}{}
				})
			}
			if ev.Event == sdl.WINDOWEVENT_ENTER {
				if hasleft != nil {
					hasleft.Stop()
					hasleft = nil
				}
			}
			action = ActionDraw
		case *sdl.MouseMotionEvent:
			if warped {
				warped = false
				break
			}
			menu := rootmenu.getmenu(ev.WindowID)
			if rootmenu.ctxmenu.seen && menu == nil {
				return def, ErrExited
			}
			if menu == nil {
				continue
			}
			itemidx := menu.getitem(int(ev.Y))
			if itemidx == -1 {
				continue
			}
			item := menu.items[itemidx]
			if previtem == item {
				continue
			}
			rootmenu.ctxmenu.seen = true
			previtem = item
			if item.label == "" {
				menu.selected = -1
			} else {
				menu.selected = itemidx
			}
			menu.draw()
			if item.submenu != nil {
				curmenu = item.submenu
				curmenu.selected = -1
			} else {
				curmenu = menu
			}
			curmenu.show(menu)
			if item.label != "" && hover != nil {
				hover(item.output)
			}
			action = ActionClear | ActionMap | ActionDraw
		case *sdl.MouseWheelEvent:
			if curmenu.overflow == -1 {
				break
			}
			if ev.Y < 0 {
				curmenu.first = max(curmenu.first-1, 0)
				action = ActionClear | ActionMap | ActionDraw
				break
			} else if ev.Y > 0 {
				curmenu.first = min(curmenu.first+1, len(curmenu.items)-curmenu.overflow)
				action = ActionClear | ActionMap | ActionDraw
				break
			}
		case *sdl.MouseButtonEvent:
			if ev.State != sdl.PRESSED {
				break
			}
			menu := curmenu.getmenu(ev.WindowID)
			if menu == nil {
				return def, ErrExited
			}
			item := menu.getitem(int(ev.Y))
			ovitem := menu.isoverflowitem(int(ev.Y))
			if item == -1 && ovitem == OverflowNone {
				curmenu.selected = -1
				menu.first = 0
				action = ActionClear | ActionMap | ActionDraw
				break
			}
			if ovitem == OverflowTop {
				curmenu.first = max(curmenu.first-1, 0)
				action = ActionClear | ActionMap | ActionDraw
				break
			} else if ovitem == OverflowBottom {
				curmenu.first = min(curmenu.first+1, len(curmenu.items)-curmenu.overflow)
				action = ActionClear | ActionMap | ActionDraw
				break
			}
			if menu.items[item].label == "" {
				return /* ignore separators */
			}
			if menu.items[item].submenu != nil {
				curmenu = menu.items[item].submenu
				curmenu.show(menu)
			} else {
				return menu.items[item].output, nil
			}
			curmenu.selected = 0
			action = ActionClear | ActionMap | ActionDraw
			if ev.Button == sdl.BUTTON_MIDDLE {
				action |= ActionWarp
			}
		case *sdl.KeyboardEvent:
			if ev.State != sdl.PRESSED {
				break
			}

			/* esc closes ctxmenu when current menu is the root menu */
			if ev.Keysym.Sym == sdl.K_ESCAPE && curmenu.caller == nil {
				return def, ErrExited
			}

			/* cycle through menu */
			curmenu.selected = -1
			switch ev.Keysym.Sym {
			case sdl.K_HOME:
				curmenu.selected = curmenu.itemcycle(ItemFirst)
				action = ActionClear | ActionDraw
			case sdl.K_END:
				curmenu.selected = curmenu.itemcycle(ItemLast)
				action = ActionClear | ActionDraw
			case sdl.K_TAB:
				if ev.Keysym.Mod&sdl.KMOD_SHIFT > 0 {
					if len(buf) > 0 {
						curmenu.selected = curmenu.matchitem(string(buf), -1)
						action = ActionDraw
					} else {
						curmenu.selected = curmenu.itemcycle(ItemPrev)
						action = ActionClear | ActionDraw
					}
				} else {
					if len(buf) > 0 {
						curmenu.selected = curmenu.matchitem(string(buf), 1)
						action = ActionDraw
					} else {
						curmenu.selected = curmenu.itemcycle(ItemNext)
						action = ActionClear | ActionDraw
					}
				}
			case sdl.K_UP:
				curmenu.selected = curmenu.itemcycle(ItemPrev)
				action = ActionClear | ActionDraw
			case sdl.K_DOWN:
				curmenu.selected = curmenu.itemcycle(ItemNext)
				action = ActionClear | ActionDraw
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				item := curmenu.itemcycle(ItemFirst)
				for range ev.Keysym.Sym - '0' {
					curmenu.selected = item
					item = curmenu.itemcycle(ItemNext)
				}
				curmenu.selected = item
				action = ActionClear | ActionDraw
			case sdl.K_RETURN, sdl.K_RIGHT:
				if curmenu.selected != -1 {
					if curmenu.items[curmenu.selected].label == "" {
						return /* ignore separators */
					}
					if curmenu.items[curmenu.selected].submenu != nil {
						curmenu = curmenu.items[curmenu.selected].submenu
						curmenu.show(curmenu)
					} else {
						return curmenu.items[curmenu.selected].output, nil
					}
					curmenu.selected = 0
					action = ActionClear | ActionMap | ActionDraw
				}
			case sdl.K_ESCAPE, sdl.K_LEFT:
				if curmenu.caller != nil {
					curmenu.selected = curmenu.caller.selected
					curmenu = curmenu.caller
					action = ActionClear | ActionMap | ActionDraw
				}
			case sdl.K_BACKSPACE, sdl.K_CLEAR, sdl.K_DELETE:
				action = ActionClear | ActionDraw
			default:
				if !unicode.IsPrint(rune(ev.Keysym.Sym)) {
					break
				}
				for range 2 {
					buf = append(buf, byte(ev.Keysym.Sym))
					if curmenu.selected = curmenu.matchitem(string(buf), 0); curmenu.selected != -1 {
						break
					}
					buf = buf[:0]
				}
				action = ActionDraw
			}
		}
		if action&ActionClear != 0 {
			buf = buf[:0]
		}
		if action&ActionDraw != 0 {
			err := curmenu.draw()
			if err != nil {
				panic(err)
			}
		}
		if action&ActionWarp != 0 {
			curmenu.warp()
			warped = true
		}
	}
}

func XmenuInit(conf Config) (*ContextMenu, error) {
	var ctxmenu ContextMenu
	/* initializers */
	var err error
	ctxmenu.Config = conf
	ctxmenu.normal.Background, err = parseColor(ctxmenu.BackgroundColor)
	if err != nil {
		return nil, err
	}
	ctxmenu.normal.Foreground, err = parseColor(ctxmenu.ForegroundColor)
	if err != nil {
		return nil, err
	}
	ctxmenu.selected.Background, err = parseColor(ctxmenu.SelbackgroundColor)
	if err != nil {
		return nil, err
	}
	ctxmenu.selected.Foreground, err = parseColor(ctxmenu.SelforegroundColor)
	if err != nil {
		return nil, err
	}
	ctxmenu.separator, err = parseColor(ctxmenu.SeparatorColor)
	if err != nil {
		return nil, err
	}
	ctxmenu.border, err = parseColor(ctxmenu.BorderColor)
	if err != nil {
		return nil, err
	}
	ctxmenu.font, err = parseFontString(ctxmenu.Config.FontName)
	if err != nil {
		return nil, err
	}
	return &ctxmenu, err
}

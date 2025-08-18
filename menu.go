package ctxmenu

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"iter"
	"log"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/KononK/resize"
	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
	"github.com/veandco/go-sdl2/sdl"
)

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
	labeltex   draw.Image
	submenu    *Menu[T] /* submenu spawned by clicking on item */
	icon       image.Image
	overflower OverflowItem

	w, h int /* item geometry */
}

/* Menu is a menu- or submenu-window */
type Menu[T comparable] struct {
	ctxmenu      *ContextMenu /* context */
	items        []*Item[T]   /* list of items contained by the menu */
	first        int          /* index of first element, if scrolled */
	selected     int          /* index of item currently selected in the menu */
	overflow     int          /* index of first item out of sight, -1 if not overflowing */
	x, y         int          /* menu position */
	w, h         int          /* geometry */
	surf         *image.RGBA  /* rendering surface */
	caller       *Menu[T]     /* current parent of this window, nil if root-window */
	itemsChanged bool         /* if the boundaries require updating */

	overflowItemTop    *Item[T]
	overflowItemBottom *Item[T]

	exit         bool
	surface      *proto.WlSurface
	layersurface *proto.LayerSurface

	file *os.File
	pool *proto.ShmPool
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

func getDecoder(imagepath string) (func(io.Reader) (image.Image, error), error) {
	ext := strings.ToLower(path.Ext(imagepath))
	switch ext {
	case ".png":
		return png.Decode, nil
	case ".jpg", ".jpeg":
		return jpeg.Decode, nil
	case ".gif":
		return gif.Decode, nil
	default:
		return nil, fmt.Errorf("unknown image format: %s", ext)
	}
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
		dec, err := getDecoder(imagefile)
		if err != nil {
			return nil, err
		}

		r, err := os.Open(imagefile)
		if err != nil {
			return nil, err
		}
		img, err := dec(r)
		if err != nil {
			return nil, err
		}

		item.icon = resize.Resize(uint(menu.ctxmenu.IconSize), uint(menu.ctxmenu.IconSize), img, resize.Bilinear)
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
	item.w = bottomArrow.Rect.Max.X + menu.ctxmenu.PaddingX*2
	item.h = bottomArrow.Rect.Max.Y + menu.ctxmenu.PaddingY*2
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
	item.w += rightArrow.Rect.Max.X
	item.parent.w = max(item.parent.w, item.w)
	item.submenu = sub
}

func (menu *Menu[T]) updateWindow() error {
	if menu.surface == nil {
		menu.surf = image.NewRGBA(image.Rect(0, 0, menu.w, menu.h))
		draw.Draw(menu.surf, menu.surf.Rect, image.Black, image.Point{}, draw.Over)

		// Create a wl_surface for toplevel menudow
		menu.surface = menu.ctxmenu.compositor.CreateSurface(nil)

		// zwlr_layer_shell_v1.get_layer_surface(surface, output, layer, namespace)
		menu.layersurface = menu.ctxmenu.layerShell.GetLayerSurface(menu.surface, nil, proto.LayerShellLayerOverlay, "menu", &proto.LayerSurfaceHandlers{
			// Listen for configure/closed
			OnConfigure: func(ev wayland.Event) {
				e := ev.(*proto.LayerSurfaceConfigureEvent)
				// Ack first (required)
				menu.layersurface.AckConfigure(e.Serial)

				// If compositor provides width/height > 0, you can resize your buffer here.
				// For now we just attach whatever frame we have.
				menu.drawFrame()
				menu.surface.Commit()
			},
		})

		menu.layersurface.SetKeyboardInteractivity(proto.LayerSurfaceKeyboardInteractivityOnDemand)

		// Typical “popup” anchoring: top-left (change as you like)
		menu.layersurface.SetAnchor(proto.LayerSurfaceAnchorTop | proto.LayerSurfaceAnchorLeft)

		menu.layersurface.SetMargin(int32(menu.x), 0, 0, int32(menu.y))

		// Desired size — compositor may override via configure.
		// If you want the surface to size to your buffer, set 0,0 here; otherwise set a hint.
		menu.layersurface.SetSize(uint32(menu.surf.Rect.Dx()), uint32(menu.surf.Rect.Dy()))

		// Optional: Make it ignore struts (don’t reserve space like a panel)
		// -1 means “auto” exclusive zone; 0 means none. For a popup-like surface, 0 is typical.
		menu.layersurface.SetExclusiveZone(0)

		// Commit the state changes (title & appID) to the server
		menu.surface.Commit()

		menu.openFile()
	} else {
		menu.layersurface.SetMargin(int32(menu.x), 0, 0, int32(menu.y))

		menu.surface.Commit()
		// TODO:
		// menu.win.SetSize(int32(menu.w), int32(menu.h))
		// menu.win.SetPosition(int32(menu.x), int32(menu.y))
		// menu.win.Show()
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

	mr := menu.ctxmenu.Monitor()

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

		if menu.h > mr.Max.Y {
			/* both arrow items */
			menu.h = (bottomArrow.Rect.Max.Y + menu.ctxmenu.PaddingY*2 + menu.ctxmenu.BorderSize) * 2
			for i, item := range menu.items {
				if item.h+menu.h > mr.Max.Y {
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

		if menu.x < mr.Min.X {
			menu.x = mr.Min.X
		} else if menu.x+menu.w > mr.Max.X {
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
		menu.x = menu.ctxmenu.x
		menu.y = 0
		if menu.overflow == -1 {
			menu.y = menu.ctxmenu.y
		}
	}

	if menu.x < int(mr.Min.X) {
		menu.x = int(mr.Min.X)
	} else if menu.x+menu.w > int(mr.Max.X) {
		menu.x = int(mr.Max.X) - menu.w
	}
	if menu.y < int(mr.Min.Y) {
		menu.y = int(mr.Min.Y)
	} else if menu.y+menu.h > int(mr.Max.Y) {
		menu.y = int(mr.Max.Y) - menu.h
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
	if menu.layersurface != nil {
		menu.layersurface.Destroy()
		menu.layersurface = nil
	}
	if menu.surface != nil {
		menu.surface.Destroy()
		menu.surface = nil
	}
}

/* draw overflow button */
func (menu *Menu[T]) drawItem(y int, index int, item *Item[T]) error {
	// x := menu.ctxmenu.vertpadding
	// y += menu.ctxmenu.horzpadding

	color := menu.ctxmenu.normal
	if index != -1 && index == menu.selected {
		color = menu.ctxmenu.selected
	}

	img := &SubImage{menu.surf, image.Rect(0, y, menu.w, y+item.h)}

	draw.Draw(img, img.Bounds(), image.NewUniform(color.Background), image.Point{}, draw.Src)

	if item.overflower != OverflowNone {
		pixels := topArrow
		if item.overflower == OverflowBottom {
			pixels = bottomArrow
		}

		x := menu.w/2 - bottomArrow.Rect.Max.X/2
		y := item.h/2 - bottomArrow.Rect.Max.Y/2

		draw.DrawMask(img, pixels.Bounds().Add(image.Point{x, y}), image.NewUniform(color.Foreground), image.Point{}, pixels, image.Point{}, draw.Over)
	} else if item.label != "" {
		x := menu.ctxmenu.PaddingX + menu.ctxmenu.BorderSize
		if item.icon != nil {
			x += menu.ctxmenu.IconSize + menu.ctxmenu.PaddingX
		}

		textH := menu.ctxmenu.font.Metrics().Height.Ceil()
		textW := menu.ctxmenu.messureText(item.label)
		if item.labeltex == nil {
			item.labeltex = image.NewAlpha(image.Rect(0, 0, textW, textH))
			menu.ctxmenu.drawText(item.labeltex, item.label)
		}
		textY := item.h/2 - textH/2

		draw.DrawMask(img, item.labeltex.Bounds().Add(image.Point{x, textY}), image.NewUniform(color.Foreground), image.Point{}, item.labeltex, image.Point{}, draw.Over)

		if item.submenu != nil {
			x := menu.w - rightArrow.Rect.Max.X - menu.ctxmenu.BorderSize - menu.ctxmenu.PaddingX
			y := item.h/2 - rightArrow.Rect.Max.Y/2
			draw.DrawMask(img, rightArrow.Bounds().Add(image.Point{x, y}), image.NewUniform(color.Foreground), image.Point{}, rightArrow, image.Point{}, draw.Over)
		}

		if item.icon != nil {
			x := menu.ctxmenu.BorderSize + menu.ctxmenu.PaddingX
			y := item.h/2 - menu.ctxmenu.IconSize/2
			draw.Draw(img, image.Rect(x, y, x+menu.ctxmenu.IconSize, y+menu.ctxmenu.IconSize), item.icon, image.Point{}, draw.Over)
		}
	} else {
		x := menu.ctxmenu.BorderSize + menu.ctxmenu.PaddingX + menu.ctxmenu.SeperatorLength
		y := menu.ctxmenu.PaddingY
		draw.Draw(img, image.Rect(x, y, x+menu.w-x*2, y+1), image.NewUniform(menu.ctxmenu.separator), image.Point{}, draw.Src)
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
func (menu *Menu[T]) draw() {
	y := menu.ctxmenu.BorderSize

	for i, item := range menu.visibleItems(true) {
		menu.drawItem(y, i, item)
		y += item.h
	}

	bw := menu.ctxmenu.BorderSize
	/* top */
	draw.Draw(menu.surf, image.Rect(0, 0, menu.w, bw), image.NewUniform(menu.ctxmenu.border), image.Point{}, draw.Src)

	/* bottom */
	draw.Draw(menu.surf, image.Rect(0, menu.h-bw, menu.w, menu.h), image.NewUniform(menu.ctxmenu.border), image.Point{}, draw.Src)

	/* left */
	draw.Draw(menu.surf, image.Rect(0, 0, bw, menu.h), image.NewUniform(menu.ctxmenu.border), image.Point{}, draw.Src)

	/* right */
	draw.Draw(menu.surf, image.Rect(menu.w-bw, 0, menu.w, menu.h), image.NewUniform(menu.ctxmenu.border), image.Point{}, draw.Src)
	fmt.Printf("here %p\n", menu.surf)
	menu.drawFrame()
	menu.surface.Commit()
}

/* get menu of given window */
func (menu *Menu[T]) getmenu(win uint32) *Menu[T] {
	if menu == nil {
		return nil
	}
	if menu.surface != nil {
		id := menu.surface.ID()
		if id == win {
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
		for item < len(menu.items) && menu.items[item].label == "" {
			item++
		}
		if menu.items[item].label == "" {
			item = 0
		}
	case ItemPrev:
	case ItemLast:
		for item >= 0 && menu.items[item].label == "" {
			item--
		}
		if menu.items[item].label == "" {
			item = len(menu.items) - 1
		}
	}
	fmt.Printf("cycle %d -> %d\n", menu.selected, item)
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

func (menu *Menu[T]) openFile() {
	if menu.surf == nil {
		return
	}

	size := len(menu.surf.Pix)

	var err error
	menu.file, err = createTmpfile(int64(size))
	if err != nil {
		log.Fatalf("unable to create a temporary file: %v", err)
	}
	// defer file.Close()

	fmt.Printf("before: %p\n", menu.surf.Pix)
	menu.surf.Pix, err = syscall.Mmap(int(menu.file.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Fatalf("unable to create mapping: %v", err)
	}
	fmt.Printf("after: %p\n", menu.surf.Pix)

	menu.pool = menu.ctxmenu.shm.CreatePool(int(menu.file.Fd()), int32(size), nil)
}

func (menu *Menu[T]) drawFrame() {
	if menu.pool == nil {
		return
	}
	menu.surface.Damage(0, 0, int32(menu.w), int32(menu.h))
	buf := menu.pool.CreateBuffer(0, int32(menu.surf.Rect.Dx()), int32(menu.surf.Rect.Dy()), int32(menu.surf.Stride), proto.ShmFormatAbgr8888, &proto.BufferHandlers{
		OnRelease: func(e wayland.Event) {
			fmt.Println("released!")
			e.Proxy().(*proto.Buffer).Destroy()
		},
	})

	menu.surface.Attach(buf, 0, 0)
}

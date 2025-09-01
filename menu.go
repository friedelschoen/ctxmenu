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
	"path"
	"strings"

	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
)

var ErrExited = errors.New("quit-request received")

/* menuState is a menuState- or submenu-window */
type menuState[T comparable] struct {
	children     []Item[T]    /* list of items contained by the menu */
	ctxmenu      *ContextMenu /* context */
	first        int          /* index of first element, if scrolled */
	selected     int          /* index of item currently selected in the menu */
	overflow     int          /* index of first item out of sight, -1 if not overflowing */
	x, y         int          /* menu position */
	w, h         int          /* geometry */
	itemsChanged bool         /* if the boundaries require updating */

	exit     bool
	surface  *proto.WlSurface
	buffer   *proto.Buffer
	lsurface *proto.LayerSurface
	surf     *SurfaceImage
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

func makeMenu[T comparable](ctxmenu *ContextMenu, items []Item[T]) (*menuState[T], error) {
	menu := &menuState[T]{
		children: items,
		ctxmenu:  ctxmenu,
	}
	menu.x = -1
	menu.y = -1

	menu.itemsChanged = true
	return menu, nil
}

func (menu *menuState[T]) updateWindow() error {
	if menu.surface == nil {
		// Create a wl_surface for toplevel menudow
		menu.surface = menu.ctxmenu.compositor.CreateSurface(nil)

		// zwlr_layer_shell_v1.get_layer_surface(surface, output, layer, namespace)
		menu.lsurface = menu.ctxmenu.lshell.GetLayerSurface(menu.surface, menu.ctxmenu.output, proto.LayerShellLayerOverlay, "menu", &proto.LayerSurfaceHandlers{
			// Listen for configure/closed
			OnConfigure: func(ev wayland.Event) bool {
				e := ev.(*proto.LayerSurfaceConfigureEvent)
				// Ack first (required)
				e.Proxy().(*proto.LayerSurface).AckConfigure(e.Serial())

				// If compositor provides width/height > 0, you can resize your buffer here.
				menu.surface.Commit()
				return true
			},
		})

		menu.lsurface.SetKeyboardInteractivity(proto.LayerSurfaceKeyboardInteractivityOnDemand)

		// Optional: Make it ignore struts (don’t reserve space like a panel)
		// -1 means “auto” exclusive zone; 0 means none. For a popup-like surface, 0 is typical.
		menu.lsurface.SetExclusiveZone(0)

		// Typical “popup” anchoring: top-left (change as you like)
		menu.lsurface.SetAnchor(proto.LayerSurfaceAnchorTop | proto.LayerSurfaceAnchorLeft)

		menu.lsurface.SetMargin(int32(menu.y), 0, 0, int32(menu.x))

		// Desired size — compositor may override via configure.
		// If you want the surface to size to your buffer, set 0,0 here; otherwise set a hint.
		menu.lsurface.SetSize(uint32(menu.w), uint32(menu.h))

		// Commit the state changes (title & appID) to the server
		menu.surface.Commit()

		menu.ctxmenu.sync()

		var err error
		menu.surf, err = NewSurfaceImage(image.Rect(0, 0, menu.w, menu.h), menu.ctxmenu.shm)
		if err != nil {
			log.Fatalf("unable to create image: %v\n", err)
		}
		menu.buffer = menu.surf.Buffer()
		menu.surface.Attach(menu.buffer, 0, 0)
	} else {
		menu.lsurface.SetMargin(int32(menu.y), 0, 0, int32(menu.x))
		menu.surface.Commit()
	}

	return nil
}

/* setup the position of a menu */
func (menu *menuState[T]) show(parent *menuState[T], x, y int) error {
	mr := menu.ctxmenu.Monitor()

	if menu.itemsChanged {
		menu.w = menu.ctxmenu.BorderSize*2 + menu.ctxmenu.MinItemWidth
		menu.h = menu.ctxmenu.BorderSize * 2
		menu.first = 0
		menu.overflow = -1

		for _, item := range menu.children {
			w, h := item.Geometry(menu.ctxmenu)
			menu.w = max(menu.w, w)
			menu.h += h
		}

		if menu.h > mr.Max.Y {
			/* both arrow items */
			menu.h = (menu.ctxmenu.OverflowArrowHeight + menu.ctxmenu.PaddingY*2 + menu.ctxmenu.BorderSize) * 2
			for i, item := range menu.children {
				w, h := item.Geometry(menu.ctxmenu)
				if h+menu.h > mr.Max.Y {
					menu.overflow = i
					break
				}
				menu.w = max(menu.w, w)
				menu.h += h
			}
		}
	}

	if menu.x == -1 || menu.y == -1 || menu.itemsChanged {
		if parent != nil {
			menu.x = parent.x + parent.w

			if menu.x < mr.Min.X {
				menu.x = mr.Min.X
			} else if menu.x+menu.w > mr.Max.X {
				menu.x = parent.x - menu.w
			}
			if menu.overflow == -1 {
				menu.y = parent.y
				start := 0
				if parent.overflow != -1 {
					start = parent.first
				}
				for i := start; i < parent.selected; i++ {
					_, h := parent.children[i].Geometry(menu.ctxmenu)
					menu.y += h
				}
			}
		} else {
			menu.x = x
			menu.y = 0
			if menu.overflow == -1 {
				menu.y = y
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
	}

	menu.itemsChanged = false

	menu.updateWindow()
	return nil
}

/* draw overflow button */
func (menu *menuState[T]) drawItem(y int, index int, item Item[T]) error {
	color := menu.ctxmenu.normal
	if index != -1 && index == menu.selected {
		color = menu.ctxmenu.selected
	}

	_, h := item.Geometry(menu.ctxmenu)
	r := image.Rect(0, y, menu.w, y+h)

	item.Draw(menu.ctxmenu, menu.surf, r, color)
	return nil
}

func (menu *menuState[T]) visibleItems(withOverflow bool) iter.Seq2[int, Item[T]] {
	return func(yield func(int, Item[T]) bool) {
		if withOverflow && menu.overflow != -1 {
			if !yield(-1, overflowItem[T](DirUp)) {
				return
			}
		}
		start := 0
		end := len(menu.children)
		if menu.overflow != -1 {
			start = menu.first
			end = menu.first + menu.overflow
		}
		for i := start; i < end; i++ {
			if !yield(i, menu.children[i]) {
				return
			}
		}
		if withOverflow && menu.overflow != -1 {
			if !yield(-1, overflowItem[T](DirDown)) {
				return
			}
		}
	}
}

/* draw pixmap for the selected and unselected version of each item on menu */
func (menu *menuState[T]) draw() {
	y := menu.ctxmenu.BorderSize

	for i, item := range menu.visibleItems(true) {
		menu.drawItem(y, i, item)
		_, h := item.Geometry(menu.ctxmenu)
		y += h
	}

	bw := menu.ctxmenu.BorderSize
	/* top */
	draw.Draw(menu.surf, image.Rect(0, 0, menu.w, bw), menu.ctxmenu.border, image.Point{}, draw.Src)

	/* bottom */
	draw.Draw(menu.surf, image.Rect(0, menu.h-bw, menu.w, menu.h), menu.ctxmenu.border, image.Point{}, draw.Src)

	/* left */
	draw.Draw(menu.surf, image.Rect(0, 0, bw, menu.h), menu.ctxmenu.border, image.Point{}, draw.Src)

	/* right */
	draw.Draw(menu.surf, image.Rect(menu.w-bw, 0, menu.w, menu.h), menu.ctxmenu.border, image.Point{}, draw.Src)

	menu.surface.Damage(0, 0, int32(menu.w), int32(menu.h))
	menu.buffer = menu.surf.Buffer()
	menu.surface.Attach(menu.buffer, 0, 0)
	menu.surface.Commit()
}

func (menu *menuState[T]) close() {
	if menu.surf != nil {
		menu.surf.Close()
		menu.surf = nil
	}
	if menu.lsurface != nil {
		menu.lsurface.Destroy()
		menu.lsurface = nil
	}
	if menu.surface != nil {
		menu.surface.Destroy()
		menu.surface = nil
	}
}

/* get in *ret the item in given menu and position; return 1 if position is on a scroll triangle */
func (menu *menuState[T]) getitem(target int) int {
	y := menu.ctxmenu.BorderSize

	for i, item := range menu.visibleItems(true) {
		_, h := item.Geometry(menu.ctxmenu)
		if i != -1 && y <= target && target < y+h {
			return i
		}
		y += h
	}

	return -1
}

func (menu *menuState[T]) isoverflowitem(target int) int {
	if menu == nil || menu.overflow == -1 {
		return 0
	}
	y := menu.ctxmenu.BorderSize

	h := menu.ctxmenu.OverflowArrowHeight + menu.ctxmenu.PaddingY*2
	if y <= target && target < y+h {
		return 1
	}
	y += h

	for _, item := range menu.visibleItems(false) {
		_, h := item.Geometry(menu.ctxmenu)
		y += h
	}

	h = menu.ctxmenu.OverflowArrowHeight + menu.ctxmenu.PaddingY*2
	if y <= target && target < y+h {
		return -1
	}

	return 0
}

/* cycle through the items; non-zero direction is next, zero is prev */
func (menu *menuState[T]) itemcycle(direction int) int {
	/* menu.selected item (either separator or labeled item) in given direction */
	item := -1
	switch direction {
	case ItemNext:
		if menu.selected == -1 {
			item = 0
		} else if menu.selected < len(menu.children)-1 {
			item = menu.selected + 1
		}
	case ItemPrev:
		if menu.selected == -1 {
			item = len(menu.children) - 1
		} else if menu.selected >= 0 {
			item = menu.selected - 1
		}
	case ItemFirst:
		item = 0
	case ItemLast:
		item = len(menu.children) - 1
	}

	/*
	 * the selected item can be a separator
	 * let's menu.selected the closest labeled item (ie., one that isn't a separator)
	 */
	switch direction {
	case ItemNext:
	case ItemFirst:
		for item < len(menu.children) && !menu.children[item].Selectable() {
			item++
		}
		if !menu.children[item].Selectable() {
			item = 0
		}
	case ItemPrev:
	case ItemLast:
		for item >= 0 && !menu.children[item].Selectable() {
			item--
		}
		if !menu.children[item].Selectable() {
			item = len(menu.children) - 1
		}
	}
	return item
}

/* get item in menu matching text from given direction (or from beginning, if dir = 0) */
func (menu *menuState[T]) matchitem(text string, dir int) int {
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
			item = len(menu.children) - 1
		}
	} else if dir > 0 {
		if menu.selected != -1 && menu.selected < len(menu.children)-1 {
			item = menu.selected + 1
		} else {
			item = 0
		}
	} else {
		item = 0
	}
	/* find next item from selected item */

	for ; item >= 0 && item < len(menu.children); item += dirinc {
		if strings.HasPrefix(menu.children[item].Label(), text) {
			return item
		}
	}
	/* if not found, try to find from the beginning/end of list */
	if dir > 0 {
		item = 0
	} else {
		item = len(menu.children) - 1
	}
	for ; item >= 0 && item < len(menu.children); item += dirinc {
		if strings.HasPrefix(menu.children[item].Label(), text) {
			return item
		}
	}
	return -1
}

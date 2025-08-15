package ctxmenu

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strconv"
	"time"
	"unicode"

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

type ContextMenu struct {
	Config
	WaylandGlobals

	normal    ColorPair
	selected  ColorPair
	border    *color.NRGBA
	separator *color.NRGBA
	x, y      int /* initial position */

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
		draw.DrawMask(dest, dr, image.Opaque, image.Point{}, mask, maskp, draw.Src)
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

/* run event loop */
func Run[T comparable](rootmenu *Menu[T], hover func(T)) (def T, err error) {
	if err := rootmenu.show(nil); err != nil {
		return def, err
	}
	rootmenu.draw()

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
			curmenu.draw()
		}
		if action&ActionWarp != 0 {
			curmenu.warp()
			warped = true
		}
	}
}

func CtxMenuInit(conf Config, wlDisplay string) (*ContextMenu, error) {
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

	ctxmenu.InitWayland(wlDisplay)
	return &ctxmenu, err
}

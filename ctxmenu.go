package ctxmenu

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"strconv"
	"time"
	"unicode"

	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
	"github.com/friedelschoen/wayland/xkb"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	actionClear = 1 << iota /* clear text */
	actionDraw              /* redraw menu windows */
)

/* enum for keyboard menu navigation */
const (
	ItemPrev = iota
	ItemNext
	ItemFirst
	ItemLast
)

/* ColorPair holds text-color information */
type ColorPair struct {
	Foreground, Background image.Image
}

type ContextMenu struct {
	*Config
	*Display

	normal    ColorPair
	selected  ColorPair
	border    image.Image
	separator image.Image
	font      font.Face

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

func parseColor(s string) (image.Image, error) {
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
	return image.NewUniform(&color.NRGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(a),
	}), nil
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

func (ctxmenu *ContextMenu) measureText(text string) int {
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

type QuitEvent struct {
}

func (QuitEvent) Proxy() wayland.Proxy {
	return nil
}

/* run event loop */
func Run[T comparable](items []Item[T], conf *Config, wlDisplay string, hover func(T)) (ret T, werr error) {
	if conf == nil {
		conf = &DefaultConfig
	}

	/* event queue with a buffer of 128, we really don't want events to kill the event-thread */
	events := make(chan wayland.Event, 128)

	disp, err := NewDisplay(wlDisplay, events)
	if err != nil {
		return ret, err
	}
	ctxmenu, err := initContext(conf, disp)
	if err != nil {
		return ret, err
	}

	pointerpos, layerpos := ctxmenu.getPointerPosition()

	rootmenu, err := makeMenu(ctxmenu, items)
	if err != nil {
		return ret, err
	}

	var stack []*menuState[T]
	curmenu := -1
	var buf []byte
	var previtem Item[T]
	// stack[curmenu].selected := -1
	var hasleft *time.Timer
	var kb *xkb.Keyboard
	var curY int

	open := func(item Item[T]) bool {
		for i := curmenu + 1; i < len(stack); i++ {
			stack[i].close()
		}
		stack = stack[:curmenu+1]

		items := item.GetSubMenu()
		if len(items) == 0 {
			return false
		}
		m, err := makeMenu(ctxmenu, items)
		if err != nil {
			log.Printf("unable to make menu: %v\n", err)
		} else {
			m.show(stack[curmenu], 0, 0)
			m.draw()
			stack = append(stack, m)
		}
		return true
	}

eventLoop:
	for event := range events {
		if ev, ok := event.(*proto.PointerEnterEvent); ok && pointerpos != nil {
			if ev.Surface().ID() == pointerpos.ID() {
				layerpos.Destroy()
				layerpos = nil
				pointerpos.Destroy()
				pointerpos = nil
				curmenu = 0
				stack = append(stack, rootmenu)
				if err := rootmenu.show(nil, int(ev.SurfaceX()), int(ev.SurfaceY())); err != nil {
					break eventLoop
				}
				rootmenu.draw()
				ctxmenu.sync()
				continue
			}
		}

		if curmenu == -1 {
			continue
		}

		var action int
		switch ev := event.(type) {
		case QuitEvent:
			err = ErrExited
			break eventLoop
		case *proto.DisplayErrorEvent:
			err = fmt.Errorf("displayerror on %s: %s [%d]\n", ev.ObjectID().Name(), ev.Message(), ev.Code())
			break eventLoop
		case *proto.WlSurfaceEnterEvent:
			action = actionDraw
		case *proto.PointerEnterEvent:
			if hasleft != nil {
				hasleft.Stop()
				hasleft = nil
			}
			for i, menu := range stack {
				if menu.surface != nil && ev.Surface().ID() == menu.surface.ID() {
					curmenu = i
					break
				}
			}
			ctxmenu.pshapedev.SetShape(ev.Serial(), proto.CursorShapeDeviceShapePointer)
		case *proto.PointerLeaveEvent:
			if rootmenu.ctxmenu.seen {
				hasleft = time.AfterFunc(100*time.Millisecond, func() {
					events <- QuitEvent{}
				})
			}
		case *proto.PointerMotionEvent:
			curY = int(ev.SurfaceY())
			itemidx := stack[curmenu].getitem(curY)
			if itemidx == -1 {
				continue
			}
			if itemidx == stack[curmenu].selected {
				continue
			}
			item := stack[curmenu].children[itemidx]
			if previtem == item {
				continue
			}
			rootmenu.ctxmenu.seen = true
			previtem = item
			if !item.Selectable() {
				stack[curmenu].selected = -1
			} else {
				stack[curmenu].selected = itemidx
			}

			open(item)
			if hover != nil {
				hover(item.Id())
			}
			action = actionClear | actionDraw
		case *proto.PointerAxisEvent:
			if ev.Axis() != proto.PointerAxisHorizontalScroll {
				break
			}
			if stack[curmenu].overflow == -1 {
				break
			}
			if ev.Value() < 0 {
				stack[curmenu].first = max(stack[curmenu].first-1, 0)
				action = actionClear | actionDraw
				break
			} else if ev.Value() > 0 {
				stack[curmenu].first = min(stack[curmenu].first+1, len(stack[curmenu].children)-stack[curmenu].overflow)
				action = actionClear | actionDraw
				break
			}
		case *proto.PointerButtonEvent:
			if ev.State() != proto.PointerButtonStatePressed {
				break
			}
			menu := stack[curmenu]
			item := menu.getitem(curY)
			ovitem := menu.isoverflowitem(curY)
			if item == -1 && ovitem == 0 {
				stack[curmenu].selected = -1
				menu.first = 0
				action = actionClear | actionDraw
				break
			}
			if ovitem == 1 {
				stack[curmenu].first = max(stack[curmenu].first-1, 0)
				action = actionClear | actionDraw
				break
			} else if ovitem == -1 {
				stack[curmenu].first = min(stack[curmenu].first+1, len(stack[curmenu].children)-stack[curmenu].overflow)
				action = actionClear | actionDraw
				break
			}
			if !menu.children[item].Selectable() {
				break /* ignore separators */
			}
			if !open(menu.children[item]) {
				ret, werr = menu.children[item].Id(), nil
				break eventLoop
			}
			stack[curmenu].selected = 0
			action = actionClear | actionDraw
		case *proto.KeyboardKeymapEvent:
			if ev.Format() != proto.KeyboardKeymapFormatXkbV1 {
				log.Printf("unsupported keymap: %v\n", ev.Format())
				break
			}
			if kb != nil {
				kb.Close()
				kb = nil
			}
			format, err := wayland.MapMemory(ev, wayland.ProtRead, wayland.MapPrivate)
			if err != nil {
				log.Fatalf("unable to create mapping: %v", err)
				break
			}
			kb, err = xkb.NewFromKeymapText(format, "")
			if err != nil {
				log.Fatalf("unable to create mapping: %v", err)
				break
			}
		case *proto.KeyboardKeyEvent:
			// if ev.State != proto.KeyboardKeyStatePressed {
			// 	break
			// }
			/* keyboard mapping is not set yet */
			if kb == nil {
				break
			}
			key, err := kb.Translate(ev.Key()+8, ev.State() == proto.KeyboardKeyStatePressed)
			if err != nil {
				log.Printf("unable to translate key: %v\n", err)
				break
			}

			/* esc closes ctxmenu when current menu is the root menu */
			if key.Sym == xkb.K_Escape && len(stack) > 1 {
				werr = ErrExited
				break eventLoop
			}

			/* cycle through menu */
			switch key.Sym {
			case xkb.K_Home:
				stack[curmenu].selected = stack[curmenu].itemcycle(ItemFirst)
				action = actionClear | actionDraw
			case xkb.K_End:
				stack[curmenu].selected = stack[curmenu].itemcycle(ItemLast)
				action = actionClear | actionDraw
			case xkb.K_Tab:
				if key.Mod&xkb.ModShift > 0 {
					if len(buf) > 0 {
						stack[curmenu].selected = stack[curmenu].matchitem(string(buf), -1)
						action = actionDraw
					} else {
						stack[curmenu].selected = stack[curmenu].itemcycle(ItemPrev)
						action = actionClear | actionDraw
					}
				} else {
					if len(buf) > 0 {
						stack[curmenu].selected = stack[curmenu].matchitem(string(buf), 1)
						action = actionDraw
					} else {
						stack[curmenu].selected = stack[curmenu].itemcycle(ItemNext)
						action = actionClear | actionDraw
					}
				}
			case xkb.K_Up:
				stack[curmenu].selected = stack[curmenu].itemcycle(ItemPrev)
				action = actionClear | actionDraw
			case xkb.K_Down:
				stack[curmenu].selected = stack[curmenu].itemcycle(ItemNext)
				action = actionClear | actionDraw
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				item := stack[curmenu].itemcycle(ItemFirst)
				for range key.Char - '0' {
					stack[curmenu].selected = item
					item = stack[curmenu].itemcycle(ItemNext)
				}
				stack[curmenu].selected = item
				action = actionClear | actionDraw
			case xkb.K_Return, xkb.K_Right:
				if stack[curmenu].selected != -1 {
					if !stack[curmenu].children[stack[curmenu].selected].Selectable() {
						break /* ignore separators */
					}
					if !open(stack[curmenu].children[stack[curmenu].selected]) {
						ret, werr = stack[curmenu].children[stack[curmenu].selected].Id(), nil
						break eventLoop
					}
					stack[curmenu].selected = 0
					action = actionClear | actionDraw
				}
			case xkb.K_Escape, xkb.K_Left:
				if len(stack) > 1 {
					stack[curmenu].close()
					stack = stack[:len(stack)-1]
					stack[curmenu] = stack[len(stack)-1]
					action = actionClear | actionDraw
				}
			case xkb.K_BackSpace, xkb.K_Clear, xkb.K_Delete:
				action = actionClear | actionDraw
			default:
				if !unicode.IsPrint(rune(key.Sym)) {
					break
				}
				for range 2 {
					buf = append(buf, byte(key.Sym))
					if stack[curmenu].selected = stack[curmenu].matchitem(string(buf), 0); stack[curmenu].selected != -1 {
						break
					}
					buf = buf[:0]
				}
				action = actionDraw
			}
		}
		if action&actionClear != 0 {
			buf = buf[:0]
		}
		if action&actionDraw != 0 {
			stack[curmenu].draw()
		}
	}

	for _, m := range stack {
		m.close()
	}
	if kb != nil {
		kb.Close()
	}

	return
}

func initContext(conf *Config, disp *Display) (*ContextMenu, error) {
	var ctxmenu ContextMenu
	/* initializers */
	var err error
	ctxmenu.Config = conf
	ctxmenu.Display = disp
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

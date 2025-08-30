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

	normal    ColorPair
	selected  ColorPair
	border    image.Image
	separator image.Image
	x, y      int /* initial position */
	font      font.Face

	seen bool /* if the cursor is seen above menu */

	conn       *wayland.Conn
	display    *proto.Display
	registry   *proto.Registry
	compositor *proto.Compositor
	seat       *proto.Seat
	lshell     *proto.LayerShell
	shm        *proto.Shm
	output     *proto.Output
	pointer    *proto.Pointer
	keyboard   *proto.Keyboard
	pshapeman  *proto.CursorShapeManager
	pshapedev  *proto.CursorShapeDevice

	monOffset image.Point
	monSize   image.Point
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

func (ctxmenu *ContextMenu) sync() {
	done := make(chan struct{})
	// Get display sync callback
	ctxmenu.display.Sync(&proto.CallbackHandlers{
		OnDone: func(_ wayland.Event) bool {
			done <- struct{}{}
			return true
		},
	})

	<-done
}

func (ctxmenu *ContextMenu) Monitor() image.Rectangle {
	return image.Rectangle{
		ctxmenu.monOffset,
		ctxmenu.monOffset.Add(ctxmenu.monSize),
	}
}

type QuitEvent struct {
}

func (QuitEvent) Proxy() wayland.Proxy {
	return nil
}

func (cm *ContextMenu) getPointerPosition() (*proto.WlSurface, *proto.LayerSurface) {
	surf := cm.compositor.CreateSurface(nil)

	var lsurf *proto.LayerSurface
	lsurf = cm.lshell.GetLayerSurface(surf, cm.output, proto.LayerShellLayerOverlay, "menu", &proto.LayerSurfaceHandlers{
		// Listen for configure/closed
		OnConfigure: func(ev wayland.Event) bool {
			e := ev.(*proto.LayerSurfaceConfigureEvent)
			// Ack first (required)
			lsurf.AckConfigure(e.Serial())

			img, err := NewSurfaceImage(image.Rect(0, 0, int(e.Width()), int(e.Height())), cm.shm)
			if err != nil {
				panic(err)
			}
			surf.Attach(img.Buffer(), 0, 0)
			surf.Commit()
			img.Close()
			return true
		},
	})

	lsurf.SetExclusiveZone(-1)
	lsurf.SetAnchor(proto.LayerSurfaceAnchorLeft | proto.LayerSurfaceAnchorRight | proto.LayerSurfaceAnchorTop | proto.LayerSurfaceAnchorBottom)
	surf.Commit()
	cm.sync()

	cm.sync()
	return surf, lsurf
}

/* run event loop */
func Run[T comparable](items Menu[T], conf *Config, wlDisplay string, hover func(T)) (ret T, werr error) {
	if conf == nil {
		conf = &DefaultConfig
	}

	/* event queue with a buffer of 128, we really don't want events to kill the event-thread */
	events := make(chan wayland.Event, 128)

	ctxmenu, err := initContext(conf, wlDisplay, events)
	if err != nil {
		return ret, err
	}

	pointerpos, layerpos := ctxmenu.getPointerPosition()

	rootmenu, err := items.makeMenu(ctxmenu, nil)
	if err != nil {
		return ret, err
	}

	var curmenu *menuState[T]
	var buf []byte
	var previtem itemState[T]
	// curmenu.selected := -1
	var hasleft *time.Timer
	var kb *xkb.Keyboard
	var curY int
eventLoop:
	for event := range events {
		if ev, ok := event.(*proto.PointerEnterEvent); ok && pointerpos != nil {
			if ev.Surface().ID() == pointerpos.ID() {
				ctxmenu.x = int(ev.SurfaceX())
				ctxmenu.y = int(ev.SurfaceY())
				layerpos.Destroy()
				layerpos = nil
				pointerpos.Destroy()
				pointerpos = nil
				curmenu = rootmenu
				if err := rootmenu.show(); err != nil {
					break eventLoop
				}
				rootmenu.draw()
				ctxmenu.sync()
				continue
			}
		}

		if curmenu == nil {
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
			for menu := range rootmenu.seq() {
				if menu.surface != nil && ev.Surface().ID() == menu.surface.ID() {
					curmenu = menu
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
			itemidx := curmenu.getitem(curY)
			if itemidx == -1 {
				continue
			}
			if itemidx == curmenu.selected {
				continue
			}
			item := curmenu.children[itemidx]
			if previtem == item {
				continue
			}
			rootmenu.ctxmenu.seen = true
			previtem = item
			if !item.Selectable() {
				curmenu.selected = -1
			} else {
				curmenu.selected = itemidx
			}
			curmenu.hideChildren(nil)
			if item.GetSubMenu() != nil {
				item.GetSubMenu().show()
				item.GetSubMenu().draw()
			}
			if hover != nil {
				hover(item.Id())
			}
			action = actionClear | actionDraw
		case *proto.PointerAxisEvent:
			if ev.Axis() != proto.PointerAxisHorizontalScroll {
				break
			}
			if curmenu.overflow == -1 {
				break
			}
			if ev.Value() < 0 {
				curmenu.first = max(curmenu.first-1, 0)
				action = actionClear | actionDraw
				break
			} else if ev.Value() > 0 {
				curmenu.first = min(curmenu.first+1, len(curmenu.children)-curmenu.overflow)
				action = actionClear | actionDraw
				break
			}
		case *proto.PointerButtonEvent:
			if ev.State() != proto.PointerButtonStatePressed {
				break
			}
			menu := curmenu
			item := menu.getitem(curY)
			ovitem := menu.isoverflowitem(curY)
			if item == -1 && ovitem == nil {
				curmenu.selected = -1
				menu.first = 0
				action = actionClear | actionDraw
				break
			}
			if ovitem == curmenu.overflowItemTop {
				curmenu.first = max(curmenu.first-1, 0)
				action = actionClear | actionDraw
				break
			} else if ovitem == curmenu.overflowItemBottom {
				curmenu.first = min(curmenu.first+1, len(curmenu.children)-curmenu.overflow)
				action = actionClear | actionDraw
				break
			}
			if !menu.children[item].Selectable() {
				break /* ignore separators */
			}
			if menu.children[item].GetSubMenu() != nil {
				curmenu = menu.children[item].GetSubMenu()
				curmenu.show()
			} else {
				ret, werr = menu.children[item].Id(), nil
				break eventLoop
			}
			curmenu.selected = 0
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
			if key.Sym == xkb.K_Escape && curmenu.parent == nil {
				werr = ErrExited
				break eventLoop
			}

			/* cycle through menu */
			switch key.Sym {
			case xkb.K_Home:
				curmenu.selected = curmenu.itemcycle(ItemFirst)
				action = actionClear | actionDraw
			case xkb.K_End:
				curmenu.selected = curmenu.itemcycle(ItemLast)
				action = actionClear | actionDraw
			case xkb.K_Tab:
				if key.Mod&xkb.ModShift > 0 {
					if len(buf) > 0 {
						curmenu.selected = curmenu.matchitem(string(buf), -1)
						action = actionDraw
					} else {
						curmenu.selected = curmenu.itemcycle(ItemPrev)
						action = actionClear | actionDraw
					}
				} else {
					if len(buf) > 0 {
						curmenu.selected = curmenu.matchitem(string(buf), 1)
						action = actionDraw
					} else {
						curmenu.selected = curmenu.itemcycle(ItemNext)
						action = actionClear | actionDraw
					}
				}
			case xkb.K_Up:
				curmenu.selected = curmenu.itemcycle(ItemPrev)
				action = actionClear | actionDraw
			case xkb.K_Down:
				curmenu.selected = curmenu.itemcycle(ItemNext)
				action = actionClear | actionDraw
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				item := curmenu.itemcycle(ItemFirst)
				for range key.Char - '0' {
					curmenu.selected = item
					item = curmenu.itemcycle(ItemNext)
				}
				curmenu.selected = item
				action = actionClear | actionDraw
			case xkb.K_Return, xkb.K_Right:
				if curmenu.selected != -1 {
					if !curmenu.children[curmenu.selected].Selectable() {
						break /* ignore separators */
					}
					if curmenu.children[curmenu.selected].GetSubMenu() != nil {
						curmenu = curmenu.children[curmenu.selected].GetSubMenu()
						curmenu.show()
					} else {
						ret, werr = curmenu.children[curmenu.selected].Id(), nil
						break eventLoop
					}
					curmenu.selected = 0
					action = actionClear | actionDraw
				}
			case xkb.K_Escape, xkb.K_Left:
				if curmenu.parent != nil {
					curmenu.selected = curmenu.parent.Parent().selected
					curmenu = curmenu.parent.Parent()
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
					if curmenu.selected = curmenu.matchitem(string(buf), 0); curmenu.selected != -1 {
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
			curmenu.draw()
		}
	}

	for m := range rootmenu.seq() {
		m.close()
	}
	if kb != nil {
		kb.Close()
	}

	return
}

func (ctxmenu *ContextMenu) getPointer() {
	ctxmenu.pointer = ctxmenu.seat.GetPointer(nil)
	ctxmenu.pshapedev = ctxmenu.pshapeman.GetPointer(ctxmenu.pointer)
}

func (ctxmenu *ContextMenu) getKeyboard() {
	ctxmenu.keyboard = ctxmenu.seat.GetKeyboard(nil)
}

func initContext(conf *Config, wlDisplay string, drain chan<- wayland.Event) (*ContextMenu, error) {
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

	ctxmenu.conn, err = wayland.Connect(wlDisplay)
	if err != nil {
		log.Fatalf("unable to connect to wayland server: %v", err)
		return nil, err
	}
	ctxmenu.conn.SetDrain(drain)

	// Connect to wayland server
	ctxmenu.display = proto.NewDisplay(&proto.DisplayHandlers{
		OnError: func(event wayland.Event) bool {
			ev := event.(*proto.DisplayErrorEvent)
			log.Fatalf("displayerror on %s: %s [%d]\n", ev.ObjectID().Name(), ev.Message(), ev.Code())
			return true
		},
		OnDeleteID: ctxmenu.conn.UnregisterEvent,
	})

	/* manually registing display */
	ctxmenu.conn.Register(ctxmenu.display)

	ctxmenu.compositor = proto.NewCompositor()
	ctxmenu.shm = proto.NewShm(nil)
	ctxmenu.seat = proto.NewSeat(&proto.SeatHandlers{
		OnCapabilities: func(evt wayland.Event) bool {
			e := evt.(*proto.SeatCapabilitiesEvent)

			hasPointer := e.Capabilities()&proto.SeatCapabilityPointer != 0
			if hasPointer && ctxmenu.pointer == nil {
				ctxmenu.getPointer()
			} else if !hasPointer && ctxmenu.pointer != nil {
				ctxmenu.pointer = nil
			}

			hasKeyboard := e.Capabilities()&proto.SeatCapabilityKeyboard != 0
			if hasKeyboard && ctxmenu.keyboard == nil {
				ctxmenu.getKeyboard()
			} else if !hasKeyboard && ctxmenu.keyboard != nil {
				ctxmenu.keyboard = nil
			}
			return true
		},
	})
	ctxmenu.lshell = proto.NewLayerShell()
	ctxmenu.output = proto.NewOutput(&proto.OutputHandlers{
		OnGeometry: func(evt wayland.Event) bool {
			e := evt.(*proto.OutputGeometryEvent)
			ctxmenu.monOffset = image.Point{int(e.X()), int(e.Y())}
			return true
		},
		OnMode: func(evt wayland.Event) bool {
			e := evt.(*proto.OutputModeEvent)
			ctxmenu.monSize = image.Point{int(e.Width()), int(e.Height())}
			return true
		},
	})
	ctxmenu.pshapeman = proto.NewCursorShapeManager()

	reg := wayland.Registrar{}
	reg.Add(ctxmenu.compositor, ctxmenu.shm, ctxmenu.seat, ctxmenu.lshell, ctxmenu.output, ctxmenu.pshapeman)

	// Get global interfaces registry
	ctxmenu.registry = ctxmenu.display.GetRegistry(&proto.RegistryHandlers{
		OnGlobal: reg.Handler,
	})

	// Wait for interfaces to register
	ctxmenu.sync()

	// Issue output geometry
	ctxmenu.sync()
	return &ctxmenu, err
}

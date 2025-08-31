package ctxmenu

import (
	"image"
	"log"

	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
)

type Display struct {
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

func (ctxmenu *Display) sync() {
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

func (ctxmenu *Display) Monitor() image.Rectangle {
	return image.Rectangle{
		ctxmenu.monOffset,
		ctxmenu.monOffset.Add(ctxmenu.monSize),
	}
}

func (cm *Display) getPointerPosition() (*proto.WlSurface, *proto.LayerSurface) {
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

func NewDisplay(wlDisplay string, drain chan<- wayland.Event) (*Display, error) {
	disp := &Display{}
	var err error
	disp.conn, err = wayland.Connect(wlDisplay)
	if err != nil {
		log.Fatalf("unable to connect to wayland server: %v", err)
		return nil, err
	}
	disp.conn.SetDrain(drain)

	// Connect to wayland server
	disp.display = proto.NewDisplay(&proto.DisplayHandlers{
		OnError: func(event wayland.Event) bool {
			ev := event.(*proto.DisplayErrorEvent)
			log.Fatalf("displayerror on %s: %s [%d]\n", ev.ObjectID().Name(), ev.Message(), ev.Code())
			return true
		},
		OnDeleteID: disp.conn.UnregisterEvent,
	})

	/* manually registing display */
	disp.conn.Register(disp.display)

	disp.compositor = proto.NewCompositor()
	disp.shm = proto.NewShm(nil)
	disp.seat = proto.NewSeat(&proto.SeatHandlers{
		OnCapabilities: func(evt wayland.Event) bool {
			e := evt.(*proto.SeatCapabilitiesEvent)

			hasPointer := e.Capabilities()&proto.SeatCapabilityPointer != 0
			if hasPointer && disp.pointer == nil {
				disp.pointer = disp.seat.GetPointer(nil)
				disp.pshapedev = disp.pshapeman.GetPointer(disp.pointer)
			} else if !hasPointer && disp.pointer != nil {
				disp.pointer = nil
				disp.pshapedev = nil
			}

			hasKeyboard := e.Capabilities()&proto.SeatCapabilityKeyboard != 0
			if hasKeyboard && disp.keyboard == nil {
				disp.keyboard = disp.seat.GetKeyboard(nil)
			} else if !hasKeyboard && disp.keyboard != nil {
				disp.keyboard = nil
			}
			return true
		},
	})
	disp.lshell = proto.NewLayerShell()
	disp.output = proto.NewOutput(&proto.OutputHandlers{
		OnGeometry: func(evt wayland.Event) bool {
			e := evt.(*proto.OutputGeometryEvent)
			disp.monOffset = image.Point{int(e.X()), int(e.Y())}
			return true
		},
		OnMode: func(evt wayland.Event) bool {
			e := evt.(*proto.OutputModeEvent)
			disp.monSize = image.Point{int(e.Width()), int(e.Height())}
			return true
		},
	})
	disp.pshapeman = proto.NewCursorShapeManager()

	reg := wayland.Registrar{}
	reg.Add(disp.compositor, disp.shm, disp.seat, disp.lshell, disp.output, disp.pshapeman)

	// Get global interfaces registry
	disp.registry = disp.display.GetRegistry(&proto.RegistryHandlers{
		OnGlobal: reg.Handler,
	})

	// Wait for interfaces to register
	disp.sync()

	// Issue output geometry
	disp.sync()
	return disp, nil
}

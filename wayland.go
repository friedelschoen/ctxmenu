package ctxmenu

import (
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"syscall"

	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
)

func createTmpfile(size int64) (*os.File, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return nil, errors.New("XDG_RUNTIME_DIR is not defined in env")
	}
	file, err := os.CreateTemp(dir, "wl_shm_go_*")
	if err != nil {
		return nil, err
	}
	err = file.Truncate(size)
	if err != nil {
		return nil, err
	}
	err = os.Remove(file.Name())
	if err != nil {
		return nil, err
	}
	return file, nil
}

type WaylandGlobals struct {
	conn       *wayland.Conn
	display    *proto.Display
	registry   *proto.Registry
	compositor *proto.Compositor
	seat       *proto.Seat
	layerShell *proto.LayerShell
	shm        *proto.Shm
	output     *proto.Output

	monOffset image.Point
	monSize   image.Point
}

func (gl *WaylandGlobals) InitWayland(wlDisplay string) {
	var err error
	gl.conn, err = wayland.Connect(wlDisplay)
	if err != nil {
		log.Fatalf("unable to connect to wayland server: %v", err)
	}

	// Connect to wayland server
	gl.display = proto.NewDisplay(&proto.DisplayHandlers{
		OnError: func(evt wayland.Event) {
			e := evt.(*proto.DisplayErrorEvent)
			log.Fatalf("display error event on %s: [%d] %s\n", e.ObjectId.Name(), e.Code, e.Message)
		},
	})
	/* manually registing display */
	gl.conn.Register(gl.display)

	gl.compositor = proto.NewCompositor(nil)
	gl.shm = proto.NewShm(nil)
	gl.seat = proto.NewSeat(nil)
	gl.layerShell = proto.NewLayerShell(nil)
	gl.output = proto.NewOutput(&proto.OutputHandlers{
		OnGeometry: func(evt wayland.Event) {
			e := evt.(*proto.OutputGeometryEvent)
			gl.monOffset = image.Point{int(e.X), int(e.Y)}
		},
		OnMode: func(evt wayland.Event) {
			e := evt.(*proto.OutputModeEvent)
			gl.monSize = image.Point{int(e.Width), int(e.Height)}
		},
	})
	reg := wayland.Registrar{gl.compositor, gl.shm, gl.seat, gl.layerShell, gl.output}

	// Get global interfaces registry
	gl.registry = gl.display.GetRegistry(&proto.RegistryHandlers{
		OnGlobal: reg.Handler,
	})

	// Wait for interfaces to register
	gl.sync()
}

func (app *WaylandGlobals) sync() {
	done := make(chan struct{})
	// Get display sync callback
	callback := app.display.Sync(&proto.CallbackHandlers{
		OnDone: func(_ wayland.Event) {
			done <- struct{}{}
		},
	})
	defer callback.Destroy()

	<-done
}

func (gl *WaylandGlobals) Monitor() image.Rectangle {
	return image.Rectangle{
		gl.monOffset,
		gl.monOffset.Add(gl.monSize),
	}
}

type WaylandWindow struct {
	gl *WaylandGlobals

	exit         bool
	frame        *image.RGBA
	surface      *proto.WlSurface
	layerSurface *proto.LayerSurface
	x, y         int

	file *os.File
	pool *proto.ShmPool
}

func (gl *WaylandGlobals) CreateWindow(appID string, frame *image.RGBA, x, y int) (*WaylandWindow, error) {
	win := &WaylandWindow{gl: gl, frame: frame, x: x, y: y}

	// Create a wl_surface for toplevel window
	win.surface = gl.compositor.CreateSurface(nil)

	// zwlr_layer_shell_v1.get_layer_surface(surface, output, layer, namespace)
	win.layerSurface = gl.layerShell.GetLayerSurface(win.surface, nil, proto.LayerShellLayerOverlay, appID, &proto.LayerSurfaceHandlers{
		// Listen for configure/closed
		OnConfigure: func(ev wayland.Event) {
			e := ev.(*proto.LayerSurfaceConfigureEvent)
			// Ack first (required)
			win.layerSurface.AckConfigure(e.Serial)

			// If compositor provides width/height > 0, you can resize your buffer here.
			// For now we just attach whatever frame we have.
			win.drawFrame()
			win.surface.Commit()
		},
		OnClosed: func(_ wayland.Event) {
			win.exit = true
		},
	})

	// Typical “popup” anchoring: top-left (change as you like)
	win.layerSurface.SetAnchor(proto.LayerSurfaceAnchorTop | proto.LayerSurfaceAnchorLeft)

	// Desired size — compositor may override via configure.
	// If you want the surface to size to your buffer, set 0,0 here; otherwise set a hint.
	win.layerSurface.SetSize(uint32(win.frame.Rect.Dx()), uint32(win.frame.Rect.Dy()))

	// Optional: Make it ignore struts (don’t reserve space like a panel)
	// -1 means “auto” exclusive zone; 0 means none. For a popup-like surface, 0 is typical.
	win.layerSurface.SetExclusiveZone(0)

	// Commit the state changes (title & appID) to the server
	win.surface.Commit()

	win.openFile()

	return win, nil
}

func (win *WaylandWindow) openFile() {
	if win.frame == nil {
		return
	}

	size := len(win.frame.Pix)

	var err error
	win.file, err = createTmpfile(int64(size))
	if err != nil {
		log.Fatalf("unable to create a temporary file: %v", err)
	}
	// defer file.Close()

	fmt.Printf("before: %p\n", win.frame.Pix)
	win.frame.Pix, err = syscall.Mmap(int(win.file.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Fatalf("unable to create mapping: %v", err)
	}
	fmt.Printf("after: %p\n", win.frame.Pix)

	win.pool = win.gl.shm.CreatePool(int(win.file.Fd()), int32(size), nil)
}

func (win *WaylandWindow) drawFrame() {
	buf := win.pool.CreateBuffer(0, int32(win.frame.Rect.Dx()), int32(win.frame.Rect.Dy()), int32(win.frame.Stride), proto.ShmFormatAbgr8888, &proto.BufferHandlers{
		OnRelease: func(e wayland.Event) {
			fmt.Println("released!")
			e.Proxy().(*proto.Buffer).Destroy()
		},
	})

	win.surface.Attach(buf, 0, 0)
	win.surface.Commit()
	fmt.Println("drawn!")
}

func (win *WaylandWindow) Reposition(x, y int) error {
	if win.x == x && win.y == x {
		return nil
	}
	// TODO:
	return nil
}

func (win *WaylandWindow) Close() error {
	if win == nil {
		return nil
	}
	win.surface.Destroy()
	win.layerSurface.Destroy()
	return nil
}

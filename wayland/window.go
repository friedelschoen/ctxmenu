package main

import (
	"errors"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"
	"syscall"

	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
)

// Global app state
type Window struct {
	appID string
	title string
	exit  bool

	Frame *image.RGBA

	conn       *wayland.Conn
	display    *proto.Display
	shm        *proto.Shm
	registry   *proto.Registry
	compositor *proto.Compositor
	seat       *proto.Seat
	layerShell *proto.LayerShell

	surface *proto.WlSurface

	layerSurface *proto.LayerSurface

	keyboard *proto.Keyboard
	pointer  *proto.Pointer
}

func CreateWindow(appID, title string, frame *image.RGBA) (*Window, error) {
	app := &Window{
		appID: appID,
		title: title,
		Frame: frame,
	}

	var err error
	app.conn, err = wayland.Connect("")
	if err != nil {
		log.Fatalf("unable to connect to wayland server: %v", err)
	}

	// Connect to wayland server
	app.display = proto.NewDisplay(&proto.DisplayHandlers{
		OnError: app.HandleDisplayError,
	})
	/* manually registing display */
	app.conn.Register(app.display)

	app.compositor = proto.NewCompositor(nil)
	app.shm = proto.NewShm(nil)
	app.seat = proto.NewSeat(&proto.SeatHandlers{
		OnCapabilities: app.HandleSeatCapabilities,
	})
	app.layerShell = proto.NewLayerShell(nil)
	reg := wayland.Registrar{app.compositor, app.shm, app.seat, app.layerShell}

	// Get global interfaces registry
	app.registry = app.display.GetRegistry(&proto.RegistryHandlers{
		OnGlobal: reg.Handler,
	})

	// Wait for interfaces to register
	app.displayRoundTrip()

	// NOTE: eee

	// Create a wl_surface for toplevel window
	app.surface = app.compositor.CreateSurface(nil)

	// zwlr_layer_shell_v1.get_layer_surface(surface, output, layer, namespace)
	app.layerSurface = app.layerShell.GetLayerSurface(app.surface, nil, proto.LayerShellLayerOverlay, app.appID, &proto.LayerSurfaceHandlers{
		// Listen for configure/closed
		OnConfigure: func(ev wayland.Event) {
			e := ev.(*proto.LayerSurfaceConfigureEvent)
			// Ack first (required)
			app.layerSurface.AckConfigure(e.Serial)

			// If compositor provides width/height > 0, you can resize your buffer here.
			// For now we just attach whatever frame we have.
			app.surface.Attach(app.drawFrame(), 0, 0)
			app.surface.Commit()
		},
		OnClosed: func(_ wayland.Event) {
			app.exit = true
		},
	})
	if err != nil {
		log.Fatalf("unable to get layer_surface: %v", err)
	}

	// Typical “popup” anchoring: top-left (change as you like)
	app.layerSurface.SetAnchor(
		proto.LayerSurfaceAnchorTop | proto.LayerSurfaceAnchorLeft,
	)

	// Desired size — compositor may override via configure.
	// If you want the surface to size to your buffer, set 0,0 here; otherwise set a hint.
	app.layerSurface.SetSize(uint32(app.Frame.Rect.Dx()), uint32(app.Frame.Rect.Dy()))

	// Optional: Make it ignore struts (don’t reserve space like a panel)
	// -1 means “auto” exclusive zone; 0 means none. For a popup-like surface, 0 is typical.
	app.layerSurface.SetExclusiveZone(0)

	// Optional margins/offset if you want
	// _ = app.layerSurface.SetMargin(top, right, bottom, left)

	// Commit the state changes (title & appID) to the server
	app.surface.Commit()

	return app, nil
}

func (app *Window) drawFrame() *proto.Buffer {
	if app.Frame == nil {
		return nil
	}

	size := len(app.Frame.Pix)

	file, err := createTmpfile(int64(size))
	if err != nil {
		log.Fatalf("unable to create a temporary file: %v", err)
	}
	defer file.Close()

	data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Fatalf("unable to create mapping: %v", err)
	}
	defer syscall.Munmap(data)

	pool := app.shm.CreatePool(int(file.Fd()), int32(size), nil)
	defer pool.Destroy()

	buf := pool.CreateBuffer(0, int32(app.Frame.Rect.Dx()), int32(app.Frame.Rect.Dy()), int32(app.Frame.Stride), proto.ShmFormatAbgr8888, &proto.BufferHandlers{
		OnRelease: func(e wayland.Event) {
			e.Proxy().(*proto.Buffer).Destroy()
		},
	})

	copy(data, app.Frame.Pix)

	return buf
}

func (app *Window) HandleSeatCapabilities(evt wayland.Event) {
	e := evt.(*proto.SeatCapabilitiesEvent)

	havePointer := (e.Capabilities & proto.SeatCapabilityPointer) != 0

	if havePointer && app.pointer == nil {
		app.attachPointer()
	} else if !havePointer && app.pointer != nil {
		app.releasePointer()
	}

	haveKeyboard := (e.Capabilities & proto.SeatCapabilityKeyboard) != 0

	if haveKeyboard && app.keyboard == nil {
		app.attachKeyboard()
	} else if !haveKeyboard && app.keyboard != nil {
		app.releaseKeyboard()
	}
}

func (*Window) HandleSeatName(_ wayland.Proxy, e proto.SeatNameEvent) {

}

// HandleDisplayError handles proto.Display errors
func (*Window) HandleDisplayError(evt wayland.Event) {
	e := evt.(*proto.DisplayErrorEvent)
	// Just log.Fatal for now
	log.Fatalf("display error event on %s: [%d] %s\n", e.ObjectId.Name(), e.Code, e.Message)
}

func (app *Window) HandleToplevelClose(_ wayland.Proxy, _ proto.ToplevelCloseEvent) {
	app.exit = true
}

func (app *Window) displayRoundTrip() {
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

func (app *Window) attachKeyboard() {
	app.keyboard = app.seat.GetKeyboard(nil)
}

func (app *Window) releaseKeyboard() {
	app.keyboard.Release()
	app.keyboard = nil

}

func (app *Window) attachPointer() {
	app.pointer = app.seat.GetPointer(&proto.PointerHandlers{
		OnEnter:                 func(e wayland.Event) { log.Println("Enter: ", e) },
		OnLeave:                 func(e wayland.Event) { log.Println("Leave: ", e) },
		OnMotion:                func(e wayland.Event) { log.Println("Motion: ", e) },
		OnButton:                func(e wayland.Event) { log.Println("Button: ", e) },
		OnAxis:                  func(e wayland.Event) { log.Println("Axis: ", e) },
		OnFrame:                 func(e wayland.Event) { log.Println("Frame: ", e) },
		OnAxisSource:            func(e wayland.Event) { log.Println("AxisSource: ", e) },
		OnAxisStop:              func(e wayland.Event) { log.Println("AxisStop: ", e) },
		OnAxisDiscrete:          func(e wayland.Event) { log.Println("AxisDiscrete: ", e) },
		OnAxisValue120:          func(e wayland.Event) { log.Println("AxisValue120: ", e) },
		OnAxisRelativeDirection: func(e wayland.Event) { log.Println("AxisRelativeDirection: ", e) },
	})

	log.Printf("pointer\n")

	// app.pointer.SetKeyHandler(app.HandleKeyboardKey)
	// keyboard.SetKeymapHandler(app.HandleKeyboardKeymap)
}

func (app *Window) releasePointer() {
	app.pointer.Release()
	app.pointer = nil

}

func (app *Window) HandleKeyboardKey(_ wayland.Proxy, e proto.KeyboardKeyEvent) {
	// close on "esc"
	if e.Key == 1 {
		app.exit = true
	}
}

func (app *Window) HandleKeyboardKeymap(_ wayland.Proxy, e proto.KeyboardKeymapEvent) {
	defer syscall.Close(e.Fd)

	// flags := syscall.MAP_SHARED
	// if app.seatVersion >= 7 {
	// 	flags = syscall.MAP_PRIVATE
	// }

	// buf, err := syscall.Mmap(
	// 	e.Fd,
	// 	0,
	// 	int(e.Size),
	// 	syscall.PROT_READ,
	// 	flags,
	// )
	// if err != nil {
	//
	// 	return
	// }
	// defer syscall.Munmap(buf)

	// fmt.Println(string(buf))
}

func (app *Window) Cleanup() {
	// Release the pointer if registered
	if app.pointer != nil {
		app.releasePointer()
	}

	// Release the keyboard if registered
	if app.keyboard != nil {
		app.releaseKeyboard()
	}

	if app.layerSurface != nil {
		app.layerSurface.Destroy()
		app.layerSurface = nil
	}
	if app.layerShell != nil {
		app.layerShell.Destroy()
		app.layerShell = nil
	}

	if app.surface != nil {
		app.surface.Destroy()
		app.surface = nil
	}

	// Release wl_seat handlers
	if app.seat != nil {
		app.seat.Release()
		app.seat = nil
	}

	if app.compositor != nil {
		app.compositor.Destroy()
		app.compositor = nil
	}

	if app.registry != nil {
		if err := app.registry.Destroy(); err != nil {

		}
		app.registry = nil
	}

	if app.display != nil {
		if err := app.display.Destroy(); err != nil {

		}
	}

	// Close the wayland server connection
	if err := app.conn.Close(); err != nil {

	}
}

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

func main() {
	pngfile, err := os.Open("screenshot/ctxmenu.png")
	if err != nil {
		log.Fatalln(err)
	}

	pngimg, err := png.Decode(pngfile)
	if err != nil {
		log.Fatalln(err)
	}

	destimg := image.NewRGBA(pngimg.Bounds())
	draw.Draw(destimg, destimg.Rect, pngimg, image.Point{}, draw.Over)

	win, err := CreateWindow("testwin", "testwin", destimg)
	if err != nil {
		log.Fatalln(err)
	}
	defer win.Cleanup()

	select {}
}

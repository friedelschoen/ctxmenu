package wayland

import (
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"

	"github.com/friedelschoen/ctxmenu/proto"
	client "github.com/friedelschoen/wayland"
)

type Event any

// Global app state
type Window struct {
	appID string
	title string
	exit  bool

	Frame *BGRA

	ctx         *client.Context
	display     *proto.Display
	registry    *proto.Registry
	shm         *proto.Shm
	compositor  *proto.Compositor
	output      *proto.Output
	xdgWmBase   *proto.WmBase
	seat        *proto.Seat
	seatVersion uint32

	surface     *proto.WlSurface
	xdgSurface  *proto.XdgSurface
	xdgTopLevel *proto.Toplevel

	keyboard *proto.Keyboard
	pointer  *proto.Pointer
}

func CreateWindow(appID, title string, frame *BGRA) (*Window, error) {
	app := &Window{
		appID: appID,
		title: title,
		Frame: frame,
	}

	var err error
	app.ctx, err = client.Connect("")
	if err != nil {
		log.Fatalf("unable to connect to wayland server: %v", err)
	}
	go func() {
		// Start the dispatch loop
		for !app.exit {
			evt := <-app.ctx.EventC
			switch e := evt.(type) {
			case proto.DisplayErrorEvent:
				app.HandleDisplayError(e)
			case proto.ToplevelConfigureEvent:
				app.HandleToplevelConfigure(e)
			case proto.ToplevelCloseEvent:
				app.HandleToplevelClose(e)
			case proto.XdgSurfaceConfigureEvent:
				app.HandleSurfaceConfigure(e)
			case proto.RegistryGlobalEvent:
				app.HandleRegistryGlobal(e)
			case proto.ShmFormatEvent:
				app.HandleShmFormat(e)
			case proto.WmBasePingEvent:
				app.HandleWmBasePing(e)
			case proto.SeatCapabilitiesEvent:
				app.HandleSeatCapabilities(e)
			case proto.SeatNameEvent:
				app.HandleSeatName(e)
			case proto.KeyboardKeyEvent:
				app.HandleKeyboardKey(e)
			case proto.KeyboardKeymapEvent:
				app.HandleKeyboardKeymap(e)
			default:
				fmt.Printf("dropping %T\n", e)
			}
		}
	}()

	// Connect to wayland server
	app.display = proto.NewDisplay(app.ctx)

	// Get global interfaces registry
	app.registry = proto.NewRegistry(app.ctx)
	err = app.display.GetRegistry(app.registry)
	if err != nil {
		log.Fatalf("unable to get global registry object: %v", err)
	}

	// Wait for interfaces to register
	app.displayRoundTrip()
	// Wait for handler events
	app.displayRoundTrip()

	// Create a wl_surface for toplevel window
	app.surface = proto.NewWlSurface(app.ctx)
	err = app.compositor.CreateSurface(app.surface)
	if err != nil {
		log.Fatalf("unable to create compositor surface: %v", err)
	}

	// attach wl_surface to xdg_wmbase to get toplevel
	// handle
	app.xdgSurface = proto.NewXdgSurface(app.ctx)
	err = app.xdgWmBase.GetXdgSurface(app.xdgSurface, app.surface)
	if err != nil {
		log.Fatalf("unable to get xdg_surface: %v", err)
	}

	// Get toplevel
	app.xdgTopLevel = proto.NewToplevel(app.ctx)
	err = app.xdgSurface.GetToplevel(app.xdgTopLevel)
	if err != nil {
		log.Fatalf("unable to get xdg_toplevel: %v", err)
	}

	// Set title
	if err := app.xdgTopLevel.SetTitle(app.title); err != nil {
		log.Fatalf("unable to set toplevel title: %v", err)
	}
	// Set appID
	if err := app.xdgTopLevel.SetAppId(app.appID); err != nil {
		log.Fatalf("unable to set toplevel appID: %v", err)
	}

	// Commit the state changes (title & appID) to the server
	if err := app.surface.Commit(); err != nil {
		log.Fatalf("unable to commit surface state: %v", err)
	}

	return app, nil
}

func (app *Window) HandleRegistryGlobal(e proto.RegistryGlobalEvent) {
	fmt.Printf("i: %s\n", e.Interface)
	switch e.Interface {
	case "wl_compositor":
		compositor := proto.NewCompositor(app.display.Context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, compositor)
		if err != nil {
			log.Fatalf("unable to bind wl_compositor interface: %v", err)
		}
		app.compositor = compositor
	case "wl_shm":
		shm := proto.NewShm(app.display.Context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, shm)
		if err != nil {
			log.Fatalf("unable to bind wl_shm interface: %v", err)
		}
		app.shm = shm
	case "xdg_wm_base":
		xdgWmBase := proto.NewWmBase(app.display.Context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, xdgWmBase)
		if err != nil {
			log.Fatalf("unable to bind xdg_wm_base interface: %v", err)
		}
		app.xdgWmBase = xdgWmBase
		// Add xdg_wmbase ping handler
	case "wl_seat":
		seat := proto.NewSeat(app.display.Context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, seat)
		if err != nil {
			log.Fatalf("unable to bind wl_seat interface: %v", err)
		}
		app.seat = seat
		app.seatVersion = e.Version
		// Add Keyboard & Pointer handlers
	}
}

func (app *Window) HandleShmFormat(e proto.ShmFormatEvent) {

}

func (app *Window) HandleSurfaceConfigure(e proto.XdgSurfaceConfigureEvent) {
	// Send ack to xdg_surface that we have a frame.
	if err := app.xdgSurface.AckConfigure(e.Serial); err != nil {
		log.Fatal("unable to ack xdg surface configure")
	}

	// Attach new frame to the surface
	if err := app.surface.Attach(app.drawFrame(), 0, 0); err != nil {
		log.Fatalf("unable to attach buffer to surface: %v", err)
	}
	// Commit the surface state
	if err := app.surface.Commit(); err != nil {
		log.Fatalf("unable to commit surface state: %v", err)
	}
}

func (app *Window) HandleToplevelConfigure(e proto.ToplevelConfigureEvent) {
	// width := e.Width
	// height := e.Height

	// if width == 0 || height == 0 {
	// 	return
	// }

	// if width == app.width && height == app.height {
	// 	return
	// }

	// // Resize the proxy image to new frame size
	// // and set it to frame image

	// app.frame = resize.Resize(uint(width), uint(height), app.pImage, resize.Bilinear).(*image.RGBA)

	// // Update app size
	// app.width = width
	// app.height = height
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

	pool := proto.NewShmPool(app.ctx)
	err = app.shm.CreatePool(pool, int(file.Fd()), int32(size))
	if err != nil {
		log.Fatalf("unable to create shm pool: %v", err)
	}
	defer pool.Destroy()

	buf := proto.NewBuffer(app.ctx)
	buf.OnRelease = func(_ client.Event) bool {
		buf.Destroy()
		return true
	}

	err = pool.CreateBuffer(buf, 0, int32(app.Frame.Rect.Dx()), int32(app.Frame.Rect.Dy()), int32(app.Frame.Stride), uint32(proto.ShmFormatArgb8888))
	if err != nil {
		log.Fatalf("unable to create proto.Buffer from shm pool: %v", err)
	}

	copy(data, app.Frame.Pix)

	return buf
}

func (app *Window) HandleSeatCapabilities(e proto.SeatCapabilitiesEvent) {
	havePointer := (e.Capabilities & uint32(proto.SeatCapabilityPointer)) != 0

	if havePointer && app.pointer == nil {
		app.attachPointer()
	} else if !havePointer && app.pointer != nil {
		app.releasePointer()
	}

	haveKeyboard := (e.Capabilities & uint32(proto.SeatCapabilityKeyboard)) != 0

	if haveKeyboard && app.keyboard == nil {
		app.attachKeyboard()
	} else if !haveKeyboard && app.keyboard != nil {
		app.releaseKeyboard()
	}
}

func (*Window) HandleSeatName(e proto.SeatNameEvent) {

}

// HandleDisplayError handles proto.Display errors
func (*Window) HandleDisplayError(e proto.DisplayErrorEvent) {
	// Just log.Fatal for now
	log.Fatalf("display error event: %v", e)
}

// HandleWmBasePing handles xdg ping by doing a Pong request
func (app *Window) HandleWmBasePing(e proto.WmBasePingEvent) {
	app.xdgWmBase.Pong(e.Serial)

}

func (app *Window) HandleToplevelClose(_ proto.ToplevelCloseEvent) {
	app.exit = true
}

func (app *Window) displayRoundTrip() {
	// Get display sync callback
	callback := proto.NewCallback(app.ctx)
	callback.OnDone = callback.BlockEvent

	err := app.display.Sync(callback)
	if err != nil {
		log.Fatalf("unable to get sync callback: %v", err)
	}
	defer callback.Destroy()

	callback.WaitForDone()
}

func (app *Window) attachKeyboard() {
	app.keyboard = proto.NewKeyboard(app.ctx)
	err := app.seat.GetKeyboard(app.keyboard)
	if err != nil {
		log.Fatalf("unable to register keyboard interface: %v\n", err)
	}
}

func (app *Window) releaseKeyboard() {
	if err := app.keyboard.Release(); err != nil {

	}
	app.keyboard = nil

}

func (app *Window) attachPointer() {
	app.pointer = proto.NewPointer(app.ctx)
	err := app.seat.GetPointer(app.pointer)
	if err != nil {
		log.Fatalf("unable to register keyboard interface: %v\n", err)
	}

	// app.pointer.SetKeyHandler(app.HandleKeyboardKey)
	// keyboard.SetKeymapHandler(app.HandleKeyboardKeymap)
}

func (app *Window) releasePointer() {
	if err := app.keyboard.Release(); err != nil {

	}
	app.keyboard = nil

}

func (app *Window) HandleKeyboardKey(e proto.KeyboardKeyEvent) {
	// close on "esc"
	if e.Key == 1 {
		app.exit = true
	}
}

func (app *Window) HandleKeyboardKeymap(e proto.KeyboardKeymapEvent) {
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

	if app.xdgTopLevel != nil {
		if err := app.xdgTopLevel.Destroy(); err != nil {

		}
		app.xdgTopLevel = nil
	}

	if app.xdgSurface != nil {
		if err := app.xdgSurface.Destroy(); err != nil {

		}
		app.xdgSurface = nil
	}

	if app.surface != nil {
		if err := app.surface.Destroy(); err != nil {

		}
		app.surface = nil
	}

	// Release wl_seat handlers
	if app.seat != nil {
		if err := app.seat.Release(); err != nil {

		}
		app.seat = nil
	}

	// Release xdg_wmbase
	if app.xdgWmBase != nil {
		if err := app.xdgWmBase.Destroy(); err != nil {

		}
		app.xdgWmBase = nil
	}

	if app.compositor != nil {
		if err := app.compositor.Destroy(); err != nil {

		}
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
	if err := app.display.Context().Close(); err != nil {

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

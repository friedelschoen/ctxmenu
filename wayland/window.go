package wayland

import (
	"errors"
	"image"
	"log"
	"os"
	"syscall"

	"github.com/daaku/swizzle"
	"github.com/rajveermalviya/go-wayland/wayland/client"
	"github.com/rajveermalviya/go-wayland/wayland/cursor"
	xdg_shell "github.com/rajveermalviya/go-wayland/wayland/stable/xdg-shell"
)

// Global app state
type Window struct {
	appID string
	title string
	exit  bool

	Frame *image.RGBA

	display     *client.Display
	registry    *client.Registry
	shm         *client.Shm
	compositor  *client.Compositor
	xdgWmBase   *xdg_shell.WmBase
	seat        *client.Seat
	seatVersion uint32

	surface     *client.Surface
	xdgSurface  *xdg_shell.Surface
	xdgTopLevel *xdg_shell.Toplevel

	keyboard *client.Keyboard
	pointer  *client.Pointer

	pointerEvent  pointerEvent
	cursorTheme   *cursor.Theme
	currentCursor *cursorData
}

func CreateWindow(appID, title string) (*Window, error) {
	app := &Window{
		appID: appID,
		title: title,
	}

	// Connect to wayland server
	display, err := client.Connect("")
	if err != nil {
		log.Fatalf("unable to connect to wayland server: %v", err)
	}
	app.display = display

	display.SetErrorHandler(app.HandleDisplayError)

	// Get global interfaces registry
	registry, err := app.display.GetRegistry()
	if err != nil {
		log.Fatalf("unable to get global registry object: %v", err)
	}
	app.registry = registry

	// Add global interfaces registrar handler
	registry.SetGlobalHandler(app.HandleRegistryGlobal)
	// Wait for interfaces to register
	app.displayRoundTrip()
	// Wait for handler events
	app.displayRoundTrip()

	// Create a wl_surface for toplevel window
	surface, err := app.compositor.CreateSurface()
	if err != nil {
		log.Fatalf("unable to create compositor surface: %v", err)
	}
	app.surface = surface

	// attach wl_surface to xdg_wmbase to get toplevel
	// handle
	xdgSurface, err := app.xdgWmBase.GetXdgSurface(surface)
	if err != nil {
		log.Fatalf("unable to get xdg_surface: %v", err)
	}
	app.xdgSurface = xdgSurface

	// Add xdg_surface configure handler `app.HandleSurfaceConfigure`
	xdgSurface.SetConfigureHandler(app.HandleSurfaceConfigure)

	// Get toplevel
	xdgTopLevel, err := xdgSurface.GetToplevel()
	if err != nil {
		log.Fatalf("unable to get xdg_toplevel: %v", err)
	}
	app.xdgTopLevel = xdgTopLevel

	// Add xdg_toplevel configure handler for window resizing
	xdgTopLevel.SetConfigureHandler(app.HandleToplevelConfigure)
	// Add xdg_toplevel close handler
	xdgTopLevel.SetCloseHandler(app.HandleToplevelClose)

	// Set title
	if err := xdgTopLevel.SetTitle(app.title); err != nil {
		log.Fatalf("unable to set toplevel title: %v", err)
	}
	// Set appID
	if err := xdgTopLevel.SetAppId(app.appID); err != nil {
		log.Fatalf("unable to set toplevel appID: %v", err)
	}
	// Commit the state changes (title & appID) to the server
	if err := app.surface.Commit(); err != nil {
		log.Fatalf("unable to commit surface state: %v", err)
	}

	// Load default cursor theme
	theme, err := cursor.LoadTheme("default", 24, app.shm)
	if err != nil {
		log.Fatalf("unable to load cursor theme: %v", err)
	}
	app.cursorTheme = theme

	go func() {
		// Start the dispatch loop
		for !app.exit {
			app.display.Context().Dispatch()
		}
	}()

	return app, nil
}

func (app *Window) context() *client.Context {
	return app.display.Context()
}

func (app *Window) HandleRegistryGlobal(e client.RegistryGlobalEvent) {

	switch e.Interface {
	case "wl_compositor":
		compositor := client.NewCompositor(app.context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, compositor)
		if err != nil {
			log.Fatalf("unable to bind wl_compositor interface: %v", err)
		}
		app.compositor = compositor
	case "wl_shm":
		shm := client.NewShm(app.context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, shm)
		if err != nil {
			log.Fatalf("unable to bind wl_shm interface: %v", err)
		}
		app.shm = shm

		shm.SetFormatHandler(app.HandleShmFormat)
	case "xdg_wm_base":
		xdgWmBase := xdg_shell.NewWmBase(app.context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, xdgWmBase)
		if err != nil {
			log.Fatalf("unable to bind xdg_wm_base interface: %v", err)
		}
		app.xdgWmBase = xdgWmBase
		// Add xdg_wmbase ping handler
		xdgWmBase.SetPingHandler(app.HandleWmBasePing)
	case "wl_seat":
		seat := client.NewSeat(app.context())
		err := app.registry.Bind(e.Name, e.Interface, e.Version, seat)
		if err != nil {
			log.Fatalf("unable to bind wl_seat interface: %v", err)
		}
		app.seat = seat
		app.seatVersion = e.Version
		// Add Keyboard & Pointer handlers
		seat.SetCapabilitiesHandler(app.HandleSeatCapabilities)
		seat.SetNameHandler(app.HandleSeatName)
	}
}

func (app *Window) HandleShmFormat(e client.ShmFormatEvent) {

}

func (app *Window) HandleSurfaceConfigure(e xdg_shell.SurfaceConfigureEvent) {
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

func (app *Window) HandleToplevelConfigure(e xdg_shell.ToplevelConfigureEvent) {
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

func (app *Window) drawFrame() *client.Buffer {
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

	pool, err := app.shm.CreatePool(int(file.Fd()), int32(size))
	if err != nil {
		log.Fatalf("unable to create shm pool: %v", err)
	}
	defer pool.Destroy()

	buf, err := pool.CreateBuffer(0, int32(app.Frame.Rect.Dx()), int32(app.Frame.Rect.Dy()), int32(app.Frame.Stride), uint32(client.ShmFormatArgb8888))
	if err != nil {
		log.Fatalf("unable to create client.Buffer from shm pool: %v", err)
	}

	// Convert RGBA to BGRA
	copy(data, app.Frame.Pix)
	swizzle.BGRA(data)

	buf.SetReleaseHandler(func(_ client.BufferReleaseEvent) {
		buf.Destroy()
	})

	return buf
}

func (app *Window) HandleSeatCapabilities(e client.SeatCapabilitiesEvent) {
	havePointer := (e.Capabilities & uint32(client.SeatCapabilityPointer)) != 0

	if havePointer && app.pointer == nil {
		app.attachPointer()
	} else if !havePointer && app.pointer != nil {
		app.releasePointer()
	}

	haveKeyboard := (e.Capabilities & uint32(client.SeatCapabilityKeyboard)) != 0

	if haveKeyboard && app.keyboard == nil {
		app.attachKeyboard()
	} else if !haveKeyboard && app.keyboard != nil {
		app.releaseKeyboard()
	}
}

func (*Window) HandleSeatName(e client.SeatNameEvent) {

}

// HandleDisplayError handles client.Display errors
func (*Window) HandleDisplayError(e client.DisplayErrorEvent) {
	// Just log.Fatal for now
	log.Fatalf("display error event: %v", e)
}

// HandleWmBasePing handles xdg ping by doing a Pong request
func (app *Window) HandleWmBasePing(e xdg_shell.WmBasePingEvent) {
	app.xdgWmBase.Pong(e.Serial)

}

func (app *Window) HandleToplevelClose(_ xdg_shell.ToplevelCloseEvent) {
	app.exit = true
}

func (app *Window) displayRoundTrip() {
	// Get display sync callback
	callback, err := app.display.Sync()
	if err != nil {
		log.Fatalf("unable to get sync callback: %v", err)
	}
	defer callback.Destroy()

	done := false
	callback.SetDoneHandler(func(_ client.CallbackDoneEvent) {
		done = true
	})

	// Wait for callback to return
	for !done {
		app.display.Context().Dispatch()
	}
}

func (app *Window) attachKeyboard() {
	keyboard, err := app.seat.GetKeyboard()
	if err != nil {
		log.Fatal("unable to register keyboard interface")
	}
	app.keyboard = keyboard

	keyboard.SetKeyHandler(app.HandleKeyboardKey)
	keyboard.SetKeymapHandler(app.HandleKeyboardKeymap)

}

func (app *Window) releaseKeyboard() {
	if err := app.keyboard.Release(); err != nil {

	}
	app.keyboard = nil

}

func (app *Window) HandleKeyboardKey(e client.KeyboardKeyEvent) {
	// close on "esc"
	if e.Key == 1 {
		app.exit = true
	}
}

func (app *Window) HandleKeyboardKeymap(e client.KeyboardKeymapEvent) {
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

func (app *Window) cleanup() {
	// Release the pointer if registered
	if app.pointer != nil {
		app.releasePointer()
	}

	// Release the keyboard if registered
	if app.keyboard != nil {
		app.releaseKeyboard()
	}

	if app.currentCursor != nil {
		app.currentCursor.Destory()
		app.currentCursor = nil
	}

	if app.cursorTheme != nil {
		if err := app.cursorTheme.Destroy(); err != nil {

		}
		app.cursorTheme = nil
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

	if app.shm != nil {
		if err := app.shm.Destroy(); err != nil {

		}
		app.shm = nil
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
	if err := app.context().Close(); err != nil {

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

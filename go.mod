module github.com/friedelschoen/ctxmenu

go 1.24.5

require (
	github.com/KononK/resize v0.0.0-20200801203131-21c514740ed6
	github.com/friedelschoen/wayland v0.0.0
	github.com/veandco/go-sdl2 v0.4.40
	golang.org/x/image v0.30.0
)

require golang.org/x/text v0.28.0 // indirect

replace github.com/friedelschoen/wayland => ../wayland

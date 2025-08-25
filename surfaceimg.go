package ctxmenu

import (
	"image"
	"log"
	"os"
	"syscall"

	"github.com/friedelschoen/ctxmenu/proto"
	"github.com/friedelschoen/wayland"
)

type SurfaceImage struct {
	image.RGBA
	size int
	file *os.File
	pool *proto.ShmPool
}

func NewSurfaceImage(rect image.Rectangle, shm *proto.Shm) (*SurfaceImage, error) {
	bufsize := 4 * rect.Dx() * rect.Dy()

	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	file, err := os.CreateTemp(dir, "wl_shm_go_*")
	if err != nil {
		return nil, err
	}
	err = file.Truncate(int64(bufsize))
	if err != nil {
		return nil, err
	}
	err = os.Remove(file.Name())
	if err != nil {
		return nil, err
	}

	pool := shm.CreatePool(int(file.Fd()), int32(bufsize))

	pix, err := syscall.Mmap(int(file.Fd()), 0, bufsize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Fatalf("unable to create mapping: %v", err)
	}

	return &SurfaceImage{
		RGBA: image.RGBA{
			Pix:    pix,
			Stride: 4 * rect.Dx(),
			Rect:   rect,
		},
		size: bufsize,
		pool: pool,
		file: file,
	}, nil
}

func (img *SurfaceImage) Resize(newrect image.Rectangle) {
	newsize := 4 * newrect.Dx() * newrect.Dy()
	if newsize <= img.size {
		img.size = newsize
		img.Rect = newrect
		return
	}

	syscall.Munmap(img.Pix)
	img.file.Truncate(int64(newsize))

	var err error
	img.Pix, err = syscall.Mmap(int(img.file.Fd()), 0, newsize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		log.Fatalf("unable to create mapping: %v", err)
	}
	img.pool.Resize(int32(newsize))

	img.size = newsize
	img.Rect = newrect
}

func (img *SurfaceImage) Buffer() *proto.Buffer {
	var buf *proto.Buffer
	buf = img.pool.CreateBuffer(0, int32(img.Rect.Dx()), int32(img.Rect.Dy()), int32(img.Stride), proto.ShmFormatAbgr8888, &proto.BufferHandlers{
		OnRelease: func(e wayland.Event) bool {
			buf.Destroy()
			return true
		},
	})
	return buf
}

func (img *SurfaceImage) Close() {
	if img.Pix != nil {
		syscall.Munmap(img.Pix)
		img.Pix = nil
	}
	if img.file != nil {
		img.file.Close()
		img.file = nil
	}
	if img.pool != nil {
		img.pool.Destroy()
		img.pool = nil
	}
}

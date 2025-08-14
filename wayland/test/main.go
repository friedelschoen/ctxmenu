package main

import (
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"

	"github.com/friedelschoen/ctxmenu/wayland"
)

func main() {
	pngfile, err := os.Open("screenshot/ctxmenu.png")
	if err != nil {
		log.Fatalln(err)
	}

	pngimg, err := png.Decode(pngfile)
	if err != nil {
		log.Fatalln(err)
	}

	//  = pngimg.(*image.RGBA)
	destimg := wayland.NewBGRA(pngimg.Bounds())
	draw.Draw(destimg, destimg.Rect, pngimg, image.Point{}, draw.Over)

	win, err := wayland.CreateWindow("testwin", "testwin", destimg)
	if err != nil {
		log.Fatalln(err)
	}
	defer win.Cleanup()

	select {}
}

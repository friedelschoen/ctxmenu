package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/friedelschoen/ctxmenu"
	"github.com/veandco/go-sdl2/sdl"
)

func main() {
	sdl.VideoInit("")

	xmenu, err := ctxmenu.CtxMenuInit(ctxmenu.Config{
		/* font, separate different fonts with comma */
		FontName: "monospace:size=12",

		/* colors */
		BackgroundColor:    "#FFFFFF",
		ForegroundColor:    "#2E3436",
		SelbackgroundColor: "#3584E4",
		SelforegroundColor: "#FFFFFF",
		SeparatorColor:     "#CDC7C2",
		BorderColor:        "#E6E6E6",

		/* sizes in pixels */
		MinItemWidth:    130, /* minimum width of a menu */
		BorderSize:      1,   /* menu border */
		SeperatorLength: 3,   /* space around separator */

		/* text alignment, set to LeftAlignment, CenterAlignment or RightAlignment */
		Alignment: ctxmenu.AlignLeft,

		/*
		 * The variables below cannot be set by X resources.
		 * Their values must be less than .height_pixels.
		 */

		/* the icon size is equal to .height_pixels - .iconpadding * 2 */
		IconSize: 24,

		/* area around the icon, the triangle and the separator */
		PaddingX: 4,
		PaddingY: 4,
	}, "")
	if err != nil {
		log.Fatalln(err)
	}

	rootmenu := ctxmenu.MakeMenu[string](xmenu)

	scan := bufio.NewScanner(os.Stdin)
	delim := '\t'
	for scan.Scan() {
		text := []rune(scan.Text())

		var depth int
		for len(text) > 0 && text[0] == delim {
			depth++
			text = text[1:]
		}
		var label, output, imgpath string
		var fields []string
		for f := range strings.SplitSeq(string(text), string(delim)) {
			if f != "" {
				fields = append(fields, f)
			}
		}
		switch len(fields) {
		case 0:
			/* do nothing */
		case 1:
			label = fields[0]
			output = fields[0]
		case 2:
			label = fields[0]
			output = fields[1]
		case 3:
			imgpath = fields[0]
			imgpath = strings.TrimPrefix(imgpath, "IMG:")
			label = fields[1]
			output = fields[2]
		default:
			panic("too many fields: " + string(text))
		}
		if err := rootmenu.Append(label, output, imgpath, depth); err != nil {
			panic(err)
		}
	}

	res, err := ctxmenu.Run(rootmenu, func(s string) {
		fmt.Printf("\t%s\n", s)
	})
	if err != nil {
		fmt.Printf("%s\n", res)
	}
}

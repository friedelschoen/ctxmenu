package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/friedelschoen/ctxmenu"
)

func main() {
	var rootmenu ctxmenu.Menu[string]

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
		m := &rootmenu
		for range depth {
			if len(*m) == 0 {
				panic("too deep")
			}
			m = &(*m)[len(*m)-1].SubMenu
		}
		*m = append(*m, ctxmenu.Item[string]{
			Label:     label,
			Output:    output,
			Imagefile: imgpath,
		})
	}

	res, err := ctxmenu.Run(rootmenu, nil, "", func(s string) {
		fmt.Printf("\t%s\n", s)
	})
	if err != nil && !errors.Is(err, ctxmenu.ErrExited) {
		log.Fatalf("display error: %v\n", err)
	} else if err == nil {
		fmt.Printf("%s\n", res)
	}
}

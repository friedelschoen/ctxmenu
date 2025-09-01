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
	var rootmenu []ctxmenu.Item[string]

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
			item := (*m)[len(*m)-1]
			li, ok := item.(*ctxmenu.LabelItem[string])
			if !ok {
				panic("not a regular item")
			}
			m = &li.SubMenu
		}
		if label == "" {
			*m = append(*m, &ctxmenu.SeparatorItem[string]{})
		} else {
			*m = append(*m, &ctxmenu.LabelItem[string]{
				Text:      label,
				Output:    output,
				Imagepath: imgpath,
			})
		}
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

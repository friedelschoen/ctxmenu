package main

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"path"

	"github.com/friedelschoen/ctxmenu"
)

type FileItem struct {
	ctxmenu.BaseItem
	Dir  string
	Name string
	Info os.FileInfo
}

func NewFileItem(dir string, name string) (*FileItem, error) {
	item := &FileItem{
		Dir: dir,
	}
	item.Name = name
	var err error
	item.Info, err = os.Stat(item.Id())
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (item *FileItem) Id() string {
	return path.Join(item.Dir, item.Name)
}

func (item *FileItem) Selectable() bool {
	return true
}

func (item *FileItem) HasSubMenu() bool {
	if !item.Info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(item.Id())
	if err != nil {
		return false
	}
	return len(entries) != 0
}

func (item *FileItem) Label() string {
	return item.Name
}

func (item *FileItem) Imagefile() string {
	return ""
}

func (item *FileItem) GetSubMenu() []ctxmenu.Item[string] {
	if !item.Info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(item.Id())
	if err != nil {
		log.Printf("unable to read dir %s: %v\n", item.Id(), err)
		return nil
	}
	if len(entries) == 0 {
		return nil
	}

	res := make([]ctxmenu.Item[string], len(entries))
	for i, entry := range entries {
		fitem, err := NewFileItem(item.Id(), entry.Name())
		if err != nil {
			log.Printf("unable to stat file %s: %v\n", path.Join(item.Dir, item.Name, entry.Name()), err)
			continue
		}
		res[i] = fitem
	}
	return res
}

/* Verplicht volgens je Item[T]-interface */
func (item *FileItem) Geometry(ctx *ctxmenu.ContextMenu) (int, int) {
	return ctxmenu.ItemGeometry(ctx, &item.BaseItem, item)
}

func (item *FileItem) Draw(ctx *ctxmenu.ContextMenu, dst draw.Image, bounds image.Rectangle, color ctxmenu.ColorPair) error {
	/* let the shared implementation call the correct Label()/HasSubMenu()/Imagefile() */
	return ctxmenu.ItemDraw(ctx, &item.BaseItem, item, dst, bounds, color)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "%s <path>\n", os.Args[0])
		os.Exit(1)
	}
	rootitem, err := NewFileItem(".", os.Args[1])

	res, err := ctxmenu.Run([]ctxmenu.Item[string]{rootitem}, nil, "", func(s string) {
		fmt.Printf("\t%s\n", s)
	})
	if err != nil && !errors.Is(err, ctxmenu.ErrExited) {
		log.Fatalf("display error: %v\n", err)
	} else if err == nil {
		fmt.Printf("%s\n", res)
	}
}

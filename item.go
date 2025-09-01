package ctxmenu

import (
	"image"
	"image/draw"
	"os"

	xdraw "golang.org/x/image/draw"
)

/* Item is an element inside a Menu */
type Item[T comparable] interface {
	Id() T
	GetSubMenu() []Item[T]
	Geometry(*ContextMenu) (int, int)
	Draw(*ContextMenu, draw.Image, image.Rectangle, ColorPair) error
	Selectable() bool
	Label() string
}

/* BaseItem houdt caches en afmetingen bij; geen virtuele methoden meer. */
type BaseItem struct {
	labeltex draw.Image
	icon     draw.Image
	w, h     int /* item geometry */
}

/* klein “view”-interface dat de concrete item-inhoud levert */
type itemView interface {
	Label() string
	HasSubMenu() bool
	Imagefile() string
}

/* gedeelde geometry-implementatie; gebruikt concrete self voor data */
func ItemGeometry(ctxmenu *ContextMenu, base *BaseItem, self itemView) (int, int) {
	if base.w == 0 || base.h == 0 {
		w := ctxmenu.PaddingX*2 + ctxmenu.measureText(self.Label())
		h := ctxmenu.font.Metrics().Height.Ceil() + ctxmenu.PaddingY*2

		if self.HasSubMenu() {
			w += ctxmenu.SubmenuArrowWidth
		}
		if imgpath := self.Imagefile(); imgpath != "" && !ctxmenu.DisableIcons {
			w += ctxmenu.IconSize + ctxmenu.PaddingX
			if h < ctxmenu.IconSize+ctxmenu.PaddingY*2 {
				h = ctxmenu.IconSize + ctxmenu.PaddingY*2
			}
		}
		base.w, base.h = w, h
	}
	return base.w, base.h
}

/* gedeelde draw-implementatie; gebruikt BaseItem caches + concrete self-data */
func ItemDraw(ctxmenu *ContextMenu, base *BaseItem, self itemView, dst draw.Image, bounds image.Rectangle, color ColorPair) error {
	img := &SubImage{dst, bounds}

	/* achtergrond */
	draw.Draw(img, img.Bounds(), color.Background, image.Point{}, draw.Src)

	/* icon laden/scale'n (eenmalig) */
	if base.icon == nil && self.Imagefile() != "" && !ctxmenu.DisableIcons {
		dec, err := getDecoder(self.Imagefile())
		if err != nil {
			return err
		}
		r, err := os.Open(self.Imagefile())
		if err != nil {
			return err
		}
		defer r.Close()

		srcimg, err := dec(r)
		if err != nil {
			return err
		}
		dstimg := image.NewRGBA(image.Rect(0, 0, ctxmenu.IconSize, ctxmenu.IconSize))
		xdraw.NearestNeighbor.Scale(dstimg, dstimg.Rect, srcimg, srcimg.Bounds(), draw.Src, nil)
		base.icon = dstimg
	}

	/* icon tekenen */
	x := ctxmenu.PaddingX + ctxmenu.BorderSize
	if base.icon != nil {
		iy := bounds.Dy()/2 - ctxmenu.IconSize/2
		draw.Draw(img, image.Rect(x, iy, x+ctxmenu.IconSize, iy+ctxmenu.IconSize), base.icon, image.Point{}, draw.Over)
		x += ctxmenu.IconSize + ctxmenu.PaddingX
	}

	/* tekst pre-render/cachen (rebuild als breedte verandert) */
	textH := ctxmenu.font.Metrics().Height.Ceil()
	textW := ctxmenu.measureText(self.Label())
	if base.labeltex == nil || base.labeltex.Bounds().Dx() != textW || base.labeltex.Bounds().Dy() != textH {
		base.labeltex = image.NewAlpha(image.Rect(0, 0, textW, textH))
		ctxmenu.drawText(base.labeltex, self.Label())
	}
	textY := bounds.Dy()/2 - textH/2
	draw.DrawMask(img, base.labeltex.Bounds().Add(image.Point{x, textY}), color.Foreground, image.Point{}, base.labeltex, image.Point{}, draw.Over)

	/* submenu-pijl rechts */
	if self.HasSubMenu() {
		ax := bounds.Dx() - ctxmenu.SubmenuArrowWidth - ctxmenu.SubmenuArrowMargin - ctxmenu.BorderSize - ctxmenu.PaddingX
		ay1 := ctxmenu.SubmenuArrowMargin
		ay2 := bounds.Dy() - ctxmenu.SubmenuArrowMargin
		DrawArrow(
			img,
			image.Rect(ax, ay1, ax+ctxmenu.SubmenuArrowWidth, ay2),
			color.Foreground,
			image.Point{},
			DirRight,
		)
	}
	return nil
}

/* LabelItem: concrete implementatie die de helpers gebruikt */
type LabelItem[T comparable] struct {
	BaseItem
	Text      string /* string to be drawn on menu */
	Output    T      /* string to be output when item is clicked */
	Imagepath string
	SubMenu   []Item[T]
}

func (item *LabelItem[T]) Selectable() bool { return true }
func (item *LabelItem[T]) Id() T            { return item.Output }
func (item *LabelItem[T]) GetSubMenu() []Item[T] {
	return item.SubMenu
}
func (item *LabelItem[T]) HasSubMenu() bool { return len(item.SubMenu) != 0 }
func (item *LabelItem[T]) Label() string    { return item.Text }
func (item *LabelItem[T]) Imagefile() string {
	return item.Imagepath
}

/* Verplicht volgens je Item[T]-interface */
func (item *LabelItem[T]) Geometry(ctxmenu *ContextMenu) (int, int) {
	return ItemGeometry(ctxmenu, &item.BaseItem, item)
}

func (item *LabelItem[T]) Draw(ctxmenu *ContextMenu, dst draw.Image, bounds image.Rectangle, color ColorPair) error {
	/* let the shared implementation call the correct Label()/HasSubMenu()/Imagefile() */
	return ItemDraw(ctxmenu, &item.BaseItem, item, dst, bounds, color)
}

type overflowItem[T comparable] Dir

func (item overflowItem[T]) Selectable() bool {
	return false
}
func (item overflowItem[T]) Id() (def T) {
	return
}
func (item overflowItem[T]) GetSubMenu() []Item[T] {
	return nil
}
func (item overflowItem[T]) Geometry(ctxmenu *ContextMenu) (int, int) {
	return 0, ctxmenu.OverflowArrowHeight + ctxmenu.PaddingY*2
}
func (item overflowItem[T]) Label() string {
	return ""
}
func (item overflowItem[T]) Draw(ctxmenu *ContextMenu, dst draw.Image, bounds image.Rectangle, color ColorPair) error {
	m := ctxmenu.OverflowArrowMargin
	r := bounds
	r.Min = r.Min.Add(image.Point{m, m})
	r.Max = r.Max.Sub(image.Point{m, m})

	draw.Draw(dst, bounds, color.Background, image.Point{}, draw.Src)
	DrawArrow(dst, r, color.Foreground, image.Point{}, Dir(item))
	return nil
}

type SeparatorItem[T comparable] struct{}

func (item SeparatorItem[T]) Selectable() bool {
	return false
}
func (item SeparatorItem[T]) Id() (def T) {
	return
}
func (item SeparatorItem[T]) GetSubMenu() []Item[T] {
	return nil
}
func (item SeparatorItem[T]) Geometry(ctxmenu *ContextMenu) (int, int) {
	w := ctxmenu.PaddingX * 2
	h := 1 + ctxmenu.PaddingY*2
	return w, h
}
func (item SeparatorItem[T]) Label() string {
	return ""
}
func (item SeparatorItem[T]) Draw(ctxmenu *ContextMenu, dst draw.Image, bounds image.Rectangle, color ColorPair) error {
	x := bounds.Min.X + ctxmenu.BorderSize + ctxmenu.PaddingX + ctxmenu.SeperatorLength
	y := bounds.Min.Y + ctxmenu.PaddingY
	draw.Draw(dst, bounds, color.Background, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(x, y, x+bounds.Dx()-x*2, y+1), ctxmenu.separator, image.Point{}, draw.Src)
	return nil
}

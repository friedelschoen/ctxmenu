package ctxmenu

type Alignment int

/* enum for text alignment */
const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
)

/* Config holds configurations for ctxmenu */
type Config struct {
	/* the values below are set by menu.ctxmenu.h */
	FontName           string
	BackgroundColor    string
	ForegroundColor    string
	SelbackgroundColor string
	SelforegroundColor string
	SeparatorColor     string
	BorderColor        string

	MinItemWidth       int
	BorderSize         int
	SeperatorLength    int
	IconSize           int
	PaddingX, PaddingY int
	Alignment          Alignment

	OverflowArrowHeight int
	OverflowArrowMargin int
	SubmenuArrowWidth   int
	SubmenuArrowMargin  int

	DisableIcons bool /* whether to disable icons */
}

var DefaultConfig = Config{
	/* font, separate different fonts with comma */
	FontName: "Go Mono:size=12",

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

	OverflowArrowHeight: 7,
	OverflowArrowMargin: 3,
	SubmenuArrowWidth:   10,
	SubmenuArrowMargin:  3,

	/* text alignment, set to LeftAlignment, CenterAlignment or RightAlignment */
	Alignment: AlignLeft,

	/*
	 * The variables below cannot be set by X resources.
	 * Their values must be less than .height_pixels.
	 */

	/* the icon size is equal to .height_pixels - .iconpadding * 2 */
	IconSize: 24,

	/* area around the icon, the triangle and the separator */
	PaddingX: 4,
	PaddingY: 4,
}

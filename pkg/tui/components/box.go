package components

import (
	"strings"

	"github.com/gmoigneu/gcode/pkg/tui"
)

// BoxStyle selects the border character set.
type BoxStyle int

const (
	BoxSingle BoxStyle = iota
	BoxDouble
	BoxRounded
)

type boxChars struct {
	tl, tr, bl, br string
	h, v           string
}

var boxStyleChars = map[BoxStyle]boxChars{
	BoxSingle:  {tl: "┌", tr: "┐", bl: "└", br: "┘", h: "─", v: "│"},
	BoxDouble:  {tl: "╔", tr: "╗", bl: "╚", br: "╝", h: "═", v: "║"},
	BoxRounded: {tl: "╭", tr: "╮", bl: "╰", br: "╯", h: "─", v: "│"},
}

// Box wraps a child component in a border. The child is rendered at
// width - 2 columns (to account for the left/right border) and the
// result is framed top/bottom.
type Box struct {
	child tui.Component
	title string
	style BoxStyle
}

// NewBox constructs a Box with the single-line border style.
func NewBox(child tui.Component) *Box {
	return &Box{child: child, style: BoxSingle}
}

// WithTitle sets the optional title rendered in the top border.
func (b *Box) WithTitle(title string) *Box {
	b.title = title
	return b
}

// WithStyle selects the border style.
func (b *Box) WithStyle(style BoxStyle) *Box {
	b.style = style
	return b
}

// Render draws the child inside a border.
func (b *Box) Render(width int) []string {
	if width < 2 {
		return nil
	}
	chars := boxStyleChars[b.style]
	inner := width - 2

	var rows []string
	if b.child != nil {
		rows = b.child.Render(inner)
	}

	// Top border with optional title.
	top := chars.tl
	if b.title != "" {
		title := " " + b.title + " "
		if tui.VisibleWidth(title) > inner {
			title = tui.SliceByColumn(title, 0, inner)
		}
		fill := inner - tui.VisibleWidth(title)
		if fill < 0 {
			fill = 0
		}
		top += title + strings.Repeat(chars.h, fill) + chars.tr
	} else {
		top += strings.Repeat(chars.h, inner) + chars.tr
	}

	lines := []string{top}
	for _, r := range rows {
		padded := tui.PadToWidth(r, inner)
		lines = append(lines, chars.v+padded+chars.v)
	}
	lines = append(lines, chars.bl+strings.Repeat(chars.h, inner)+chars.br)
	return lines
}

// Invalidate cascades to the child.
func (b *Box) Invalidate() {
	if b.child != nil {
		b.child.Invalidate()
	}
}

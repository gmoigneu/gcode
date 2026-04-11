package components

// Spacer emits N empty lines so callers can separate stacked components
// without wrapping them in a Box.
type Spacer struct {
	lines int
}

// NewSpacer returns a spacer with the given number of blank lines.
func NewSpacer(lines int) *Spacer {
	if lines < 0 {
		lines = 0
	}
	return &Spacer{lines: lines}
}

// SetLines updates the number of blank lines rendered.
func (s *Spacer) SetLines(lines int) {
	if lines < 0 {
		lines = 0
	}
	s.lines = lines
}

// Render returns lines empty strings.
func (s *Spacer) Render(width int) []string {
	if s.lines <= 0 {
		return nil
	}
	out := make([]string, s.lines)
	return out
}

// Invalidate is a no-op.
func (s *Spacer) Invalidate() {}

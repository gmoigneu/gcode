package components

import (
	"sync"

	"github.com/gmoigneu/gcode/pkg/tui"
)

// Container is a vertical stack of child components. Children are
// rendered top-to-bottom and their outputs concatenated.
type Container struct {
	mu       sync.RWMutex
	children []tui.Component
}

// NewContainer constructs an empty container.
func NewContainer() *Container { return &Container{} }

// AddChild appends a child to the end of the container.
func (c *Container) AddChild(child tui.Component) {
	if child == nil {
		return
	}
	c.mu.Lock()
	c.children = append(c.children, child)
	c.mu.Unlock()
}

// RemoveChild removes the first occurrence of child, if any.
func (c *Container) RemoveChild(child tui.Component) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, ch := range c.children {
		if ch == child {
			c.children = append(c.children[:i], c.children[i+1:]...)
			return
		}
	}
}

// Clear removes all children.
func (c *Container) Clear() {
	c.mu.Lock()
	c.children = nil
	c.mu.Unlock()
}

// Children returns a snapshot slice of the children.
func (c *Container) Children() []tui.Component {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]tui.Component, len(c.children))
	copy(out, c.children)
	return out
}

// Render concatenates each child's render output.
func (c *Container) Render(width int) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var lines []string
	for _, ch := range c.children {
		lines = append(lines, ch.Render(width)...)
	}
	return lines
}

// Invalidate cascades to every child.
func (c *Container) Invalidate() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, ch := range c.children {
		ch.Invalidate()
	}
}

package components

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gmoigneu/gcode/pkg/tui"
)

// SelectItem is one choice in a SelectList.
type SelectItem struct {
	Label       string
	Description string
	Value       any
}

// SelectList is a keyboard-driven list with filtering and selection.
type SelectList struct {
	mu          sync.RWMutex
	items       []SelectItem
	filtered    []int
	filter      string
	selected    int
	focused     bool
	maxVisible  int
	keybindings *tui.KeybindingsManager

	OnConfirm func(item SelectItem)
	OnCancel  func()
}

// NewSelectList constructs a SelectList populated with items.
func NewSelectList(items []SelectItem, kb *tui.KeybindingsManager) *SelectList {
	if kb == nil {
		kb = tui.NewKeybindingsManager()
	}
	s := &SelectList{
		items:       items,
		keybindings: kb,
		maxVisible:  10,
	}
	s.applyFilter()
	return s
}

// SetItems replaces the items and resets the filter + selection.
func (s *SelectList) SetItems(items []SelectItem) {
	s.mu.Lock()
	s.items = items
	s.selected = 0
	s.mu.Unlock()
	s.applyFilter()
}

// SetFilter replaces the filter string and re-indexes the items.
func (s *SelectList) SetFilter(filter string) {
	s.mu.Lock()
	s.filter = filter
	s.selected = 0
	s.mu.Unlock()
	s.applyFilter()
}

// Filter returns the current filter string.
func (s *SelectList) Filter() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filter
}

// Selected returns the currently highlighted item, if any.
func (s *SelectList) Selected() (SelectItem, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.selected < 0 || s.selected >= len(s.filtered) {
		return SelectItem{}, false
	}
	return s.items[s.filtered[s.selected]], true
}

// applyFilter recomputes the filtered-index list.
func (s *SelectList) applyFilter() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filtered = s.filtered[:0]
	needle := strings.ToLower(s.filter)
	for i, item := range s.items {
		if needle == "" || strings.Contains(strings.ToLower(item.Label), needle) {
			s.filtered = append(s.filtered, i)
		}
	}
	if s.selected >= len(s.filtered) {
		s.selected = 0
	}
}

// SetFocused marks the list focused. Only focused lists process input.
func (s *SelectList) SetFocused(f bool) {
	s.mu.Lock()
	s.focused = f
	s.mu.Unlock()
}

// IsFocused reports whether the list is focused.
func (s *SelectList) IsFocused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.focused
}

// HandleInput processes a key event. Supports up/down navigation,
// enter-to-confirm, and escape-to-cancel.
func (s *SelectList) HandleInput(data []byte) {
	if !s.IsFocused() {
		return
	}
	switch {
	case s.keybindings.Matches(data, tui.KBSelectUp):
		s.mu.Lock()
		if len(s.filtered) > 0 {
			s.selected = (s.selected - 1 + len(s.filtered)) % len(s.filtered)
		}
		s.mu.Unlock()
	case s.keybindings.Matches(data, tui.KBSelectDown):
		s.mu.Lock()
		if len(s.filtered) > 0 {
			s.selected = (s.selected + 1) % len(s.filtered)
		}
		s.mu.Unlock()
	case s.keybindings.Matches(data, tui.KBSelectConfirm):
		item, ok := s.Selected()
		if ok && s.OnConfirm != nil {
			s.OnConfirm(item)
		}
	case s.keybindings.Matches(data, tui.KBSelectCancel):
		if s.OnCancel != nil {
			s.OnCancel()
		}
	}
}

// Render draws the current view of the list.
func (s *SelectList) Render(width int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lines []string
	if len(s.filtered) == 0 {
		lines = append(lines, tui.SliceByColumn("  (no matches)", 0, width))
		return lines
	}

	start := 0
	if s.selected >= s.maxVisible {
		start = s.selected - s.maxVisible + 1
	}
	end := start + s.maxVisible
	if end > len(s.filtered) {
		end = len(s.filtered)
	}

	for i := start; i < end; i++ {
		idx := s.filtered[i]
		item := s.items[idx]
		prefix := "  "
		if i == s.selected {
			prefix = "▶ "
		}
		line := fmt.Sprintf("%s%s", prefix, item.Label)
		if item.Description != "" {
			line += "  " + dim(item.Description)
		}
		if i == s.selected {
			line = reverse(line)
		}
		if tui.VisibleWidth(line) > width {
			line = tui.SliceByColumn(line, 0, width)
		}
		lines = append(lines, line)
	}
	return lines
}

// Invalidate is a no-op; the list has no cached render state.
func (s *SelectList) Invalidate() {}

func dim(s string) string     { return "\x1b[2m" + s + tui.Reset }
func reverse(s string) string { return "\x1b[7m" + s + tui.Reset }

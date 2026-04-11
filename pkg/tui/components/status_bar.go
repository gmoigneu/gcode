package components

import (
	"fmt"
	"sync"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/tui"
)

// StatusBar is a single-line component showing model, agent status,
// token count, and cost. It is color-coded based on liveness.
type StatusBar struct {
	mu            sync.RWMutex
	model         string
	thinkingLevel string
	status        agent.AgentStatus
	elapsed       time.Duration
	tokenCount    int
	cost          float64
}

// NewStatusBar returns an empty status bar.
func NewStatusBar() *StatusBar { return &StatusBar{status: agent.StatusIdle} }

// SetModel updates the model name and thinking level shown on the left.
func (s *StatusBar) SetModel(model string, thinking string) {
	s.mu.Lock()
	s.model = model
	s.thinkingLevel = thinking
	s.mu.Unlock()
}

// SetLiveness syncs the bar's agent status and elapsed time.
func (s *StatusBar) SetLiveness(e agent.LivenessEvent) {
	s.mu.Lock()
	s.status = e.Status
	s.elapsed = e.Elapsed
	s.mu.Unlock()
}

// SetUsage updates the token count + cost shown on the right.
func (s *StatusBar) SetUsage(tokens int, cost float64) {
	s.mu.Lock()
	s.tokenCount = tokens
	s.cost = cost
	s.mu.Unlock()
}

// Render produces a single line status bar. Left segment contains
// model + thinking + status; right segment contains tokens + cost.
func (s *StatusBar) Render(width int) []string {
	if width <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	left := ""
	if s.model != "" {
		left += s.model
	}
	if s.thinkingLevel != "" {
		if left != "" {
			left += " · "
		}
		left += s.thinkingLevel
	}
	statusStr := statusLabel(s.status)
	if statusStr != "" {
		if left != "" {
			left += " · "
		}
		left += colorFor(s.status, s.elapsed) + statusStr + tui.Reset
	}

	right := ""
	if s.tokenCount > 0 {
		right = fmt.Sprintf("%d tok", s.tokenCount)
	}
	if s.cost > 0 {
		if right != "" {
			right += " · "
		}
		right += fmt.Sprintf("$%.4f", s.cost)
	}

	leftW := tui.VisibleWidth(left)
	rightW := tui.VisibleWidth(right)
	gap := width - leftW - rightW
	if gap < 1 {
		return []string{tui.SliceByColumn(left, 0, width)}
	}
	line := left + fmt.Sprintf("%*s", gap, "") + right
	return []string{line}
}

// Invalidate is a no-op.
func (s *StatusBar) Invalidate() {}

func statusLabel(status agent.AgentStatus) string {
	switch status {
	case agent.StatusThinking:
		return "thinking"
	case agent.StatusExecuting:
		return "executing"
	case agent.StatusStalled:
		return "stalled"
	case agent.StatusIdle:
		return "idle"
	}
	return ""
}

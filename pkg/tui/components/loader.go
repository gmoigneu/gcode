package components

import (
	"fmt"
	"sync"
	"time"

	"github.com/gmoigneu/gcode/pkg/agent"
	"github.com/gmoigneu/gcode/pkg/tui"
)

// spinnerFrames is the braille spinner pattern.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Loader renders a spinner + status line that reflects the agent's
// current LivenessEvent. It renders zero lines when the agent is idle.
type Loader struct {
	mu       sync.RWMutex
	status   agent.AgentStatus
	toolName string
	elapsed  time.Duration
	frame    int
}

// NewLoader returns a Loader in the idle state.
func NewLoader() *Loader { return &Loader{status: agent.StatusIdle} }

// SetLiveness updates the loader from a LivenessEvent.
func (l *Loader) SetLiveness(e agent.LivenessEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.status = e.Status
	l.toolName = e.ToolName
	l.elapsed = e.Elapsed
}

// Tick advances the spinner animation frame. Callers that run the TUI
// on a render timer can invoke this every 100ms.
func (l *Loader) Tick() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.frame = (l.frame + 1) % len(spinnerFrames)
}

// Render returns the loader line or nil when idle.
func (l *Loader) Render(width int) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.status == agent.StatusIdle {
		return nil
	}

	frame := spinnerFrames[l.frame%len(spinnerFrames)]
	elapsed := formatDuration(l.elapsed)

	var label string
	switch l.status {
	case agent.StatusThinking:
		label = fmt.Sprintf("%s Thinking... %s", frame, elapsed)
	case agent.StatusExecuting:
		tool := l.toolName
		if tool == "" {
			tool = "tool"
		}
		label = fmt.Sprintf("%s Running %s... %s", frame, tool, elapsed)
	case agent.StatusStalled:
		tool := l.toolName
		if tool == "" {
			tool = "operation"
		}
		label = fmt.Sprintf("⚠ %s still running... %s", tool, elapsed)
	}

	label = colorFor(l.status, l.elapsed) + label + tui.Reset
	if tui.VisibleWidth(label) > width {
		label = tui.SliceByColumn(label, 0, width)
	}
	return []string{label}
}

// Invalidate is a no-op (Loader has no cached lines).
func (l *Loader) Invalidate() {}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	s := int(d / time.Second)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	return fmt.Sprintf("%dm%ds", m, s%60)
}

// colorFor returns the ANSI SGR prefix for a given status + elapsed
// combination.
func colorFor(status agent.AgentStatus, elapsed time.Duration) string {
	switch status {
	case agent.StatusThinking:
		return "\x1b[32m" // green
	case agent.StatusExecuting:
		return "\x1b[34m" // blue
	case agent.StatusStalled:
		if elapsed > 60*time.Second {
			return "\x1b[31m" // red
		}
		return "\x1b[33m" // yellow
	}
	return "\x1b[2m" // dim
}

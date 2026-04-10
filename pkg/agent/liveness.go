package agent

import (
	"context"
	"sync"
	"time"
)

// livenessTickInterval is the cadence of periodic LivenessUpdate events
// while the agent is in a non-idle state. Exposed as a package variable so
// tests can accelerate it.
var livenessTickInterval = time.Second

// livenessTracker emits LivenessUpdate events for the agent's current
// status and escalates to StatusStalled when the elapsed time exceeds the
// configured threshold.
type livenessTracker struct {
	agent *Agent

	thinkingTimeout time.Duration
	toolTimeout     time.Duration

	mu         sync.Mutex
	current    AgentStatus
	toolName   string
	stateStart time.Time
	cancel     context.CancelFunc

	wg sync.WaitGroup
}

func newLivenessTracker(agent *Agent) *livenessTracker {
	return &livenessTracker{
		agent:           agent,
		thinkingTimeout: resolveThinkingStallTimeout(agent.config),
		toolTimeout:     resolveToolStallTimeout(agent.config),
	}
}

func resolveThinkingStallTimeout(cfg AgentConfig) time.Duration {
	if cfg.ThinkingStallTimeout > 0 {
		return cfg.ThinkingStallTimeout
	}
	return 60 * time.Second
}

func resolveToolStallTimeout(cfg AgentConfig) time.Duration {
	if cfg.ToolStallTimeout > 0 {
		return cfg.ToolStallTimeout
	}
	return 120 * time.Second
}

// SetStatus transitions the agent into a new state. Any previous ticker is
// cancelled before a new one is started. Emits an immediate LivenessUpdate
// for the new state (except StatusIdle).
func (lt *livenessTracker) SetStatus(ctx context.Context, status AgentStatus, toolName string) {
	lt.mu.Lock()
	if lt.cancel != nil {
		lt.cancel()
		lt.cancel = nil
	}
	lt.current = status
	lt.toolName = toolName
	lt.stateStart = time.Now()
	var tickerCtx context.Context
	if status != StatusIdle {
		tickerCtx, lt.cancel = context.WithCancel(ctx)
	}
	threshold := lt.timeoutFor(status)
	start := lt.stateStart
	name := lt.toolName
	lt.mu.Unlock()

	// Emit an initial update for non-idle transitions so consumers see the
	// new state immediately.
	if status != StatusIdle {
		lt.agent.emit(AgentEvent{
			Type: LivenessUpdate,
			Liveness: &LivenessEvent{
				Status:   status,
				ToolName: name,
				Elapsed:  0,
			},
		}, ctx)
	}

	if tickerCtx != nil && threshold > 0 {
		lt.wg.Add(1)
		go lt.tick(tickerCtx, threshold, status, name, start)
	}
}

// Stop halts any active ticker and emits a final StatusIdle update. Blocks
// until all ticker goroutines have exited.
func (lt *livenessTracker) Stop(ctx context.Context) {
	lt.mu.Lock()
	if lt.cancel != nil {
		lt.cancel()
		lt.cancel = nil
	}
	lt.current = StatusIdle
	lt.toolName = ""
	lt.mu.Unlock()

	lt.wg.Wait()

	lt.agent.emit(AgentEvent{
		Type: LivenessUpdate,
		Liveness: &LivenessEvent{
			Status:  StatusIdle,
			Elapsed: 0,
		},
	}, ctx)
}

func (lt *livenessTracker) timeoutFor(status AgentStatus) time.Duration {
	switch status {
	case StatusThinking:
		return lt.thinkingTimeout
	case StatusExecuting:
		return lt.toolTimeout
	}
	return 0
}

func (lt *livenessTracker) tick(ctx context.Context, threshold time.Duration, baseStatus AgentStatus, toolName string, start time.Time) {
	defer lt.wg.Done()
	interval := livenessTickInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			elapsed := now.Sub(start)
			status := baseStatus
			if threshold > 0 && elapsed >= threshold {
				status = StatusStalled
			}
			lt.agent.emit(AgentEvent{
				Type: LivenessUpdate,
				Liveness: &LivenessEvent{
					Status:   status,
					ToolName: toolName,
					Elapsed:  elapsed,
				},
			}, ctx)
		}
	}
}

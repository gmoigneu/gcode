package agent

import "sync"

// PendingMessageQueue is a FIFO buffer of AgentMessages. The Drain semantics
// depend on the queue's mode.
type PendingMessageQueue struct {
	mu       sync.Mutex
	messages []AgentMessage
	mode     QueueMode
}

// NewPendingMessageQueue returns an empty queue. An empty mode defaults to
// QueueOneAtATime.
func NewPendingMessageQueue(mode QueueMode) *PendingMessageQueue {
	if mode == "" {
		mode = QueueOneAtATime
	}
	return &PendingMessageQueue{mode: mode}
}

// Enqueue adds a message to the tail of the queue.
func (q *PendingMessageQueue) Enqueue(msg AgentMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, msg)
}

// Drain returns pending messages according to the queue mode.
//   - QueueAll drains everything in a single call.
//   - QueueOneAtATime drains only the head and leaves the rest.
//
// Returns nil when the queue is empty.
func (q *PendingMessageQueue) Drain() []AgentMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil
	}
	switch q.mode {
	case QueueAll:
		msgs := q.messages
		q.messages = nil
		return msgs
	case QueueOneAtATime:
		head := q.messages[0]
		q.messages = q.messages[1:]
		return []AgentMessage{head}
	}
	return nil
}

// HasItems reports whether any messages are pending.
func (q *PendingMessageQueue) HasItems() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages) > 0
}

// Clear empties the queue.
func (q *PendingMessageQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = nil
}

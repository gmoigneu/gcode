package agent

import (
	"sync"
	"testing"

	"github.com/gmoigneu/gcode/pkg/ai"
)

func msg(text string, ts int64) AgentMessage {
	return &ai.UserMessage{
		Content:   []ai.Content{&ai.TextContent{Text: text}},
		Timestamp: ts,
	}
}

func TestQueueNewDefaultsMode(t *testing.T) {
	q := NewPendingMessageQueue("")
	// Empty mode is treated as QueueOneAtATime.
	q.Enqueue(msg("a", 1))
	q.Enqueue(msg("b", 2))
	got := q.Drain()
	if len(got) != 1 {
		t.Errorf("default drain should be one-at-a-time, got %d", len(got))
	}
	if q.HasItems() == false {
		t.Error("should still have items after partial drain")
	}
}

func TestQueueEnqueueHasItems(t *testing.T) {
	q := NewPendingMessageQueue(QueueAll)
	if q.HasItems() {
		t.Error("empty queue should not report items")
	}
	q.Enqueue(msg("a", 1))
	if !q.HasItems() {
		t.Error("queue with items should report so")
	}
}

func TestQueueDrainAll(t *testing.T) {
	q := NewPendingMessageQueue(QueueAll)
	q.Enqueue(msg("a", 1))
	q.Enqueue(msg("b", 2))
	q.Enqueue(msg("c", 3))
	got := q.Drain()
	if len(got) != 3 {
		t.Errorf("got %d, want 3", len(got))
	}
	if q.HasItems() {
		t.Error("drain all should empty the queue")
	}
}

func TestQueueDrainOneAtATime(t *testing.T) {
	q := NewPendingMessageQueue(QueueOneAtATime)
	q.Enqueue(msg("a", 1))
	q.Enqueue(msg("b", 2))
	q.Enqueue(msg("c", 3))

	first := q.Drain()
	if len(first) != 1 || first[0].MessageTimestamp() != 1 {
		t.Errorf("first drain = %+v", first)
	}
	second := q.Drain()
	if len(second) != 1 || second[0].MessageTimestamp() != 2 {
		t.Errorf("second drain = %+v", second)
	}
	third := q.Drain()
	if len(third) != 1 || third[0].MessageTimestamp() != 3 {
		t.Errorf("third drain = %+v", third)
	}
	empty := q.Drain()
	if empty != nil {
		t.Errorf("fourth drain should be nil, got %+v", empty)
	}
}

func TestQueueDrainEmpty(t *testing.T) {
	q := NewPendingMessageQueue(QueueAll)
	if got := q.Drain(); got != nil {
		t.Errorf("empty drain = %+v", got)
	}
}

func TestQueueClear(t *testing.T) {
	q := NewPendingMessageQueue(QueueAll)
	q.Enqueue(msg("a", 1))
	q.Enqueue(msg("b", 2))
	q.Clear()
	if q.HasItems() {
		t.Error("Clear should empty the queue")
	}
	if q.Drain() != nil {
		t.Error("Drain after Clear should return nil")
	}
}

func TestQueueConcurrent(t *testing.T) {
	q := NewPendingMessageQueue(QueueAll)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				q.Enqueue(msg("x", int64(i*100+j)))
			}
		}(i)
	}
	wg.Wait()

	got := q.Drain()
	if len(got) != 16*50 {
		t.Errorf("got %d, want 800", len(got))
	}
}

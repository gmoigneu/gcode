package components

import "sync"

// KillRing is an Emacs-style clipboard buffer. Consecutive kills
// accumulate into the head entry; Rotate cycles through prior kills for
// yank-pop support.
type KillRing struct {
	mu      sync.Mutex
	entries []string
	index   int
	maxSize int
}

// NewKillRing constructs a kill ring with the given capacity. Zero or
// negative capacity defaults to 32.
func NewKillRing(maxSize int) *KillRing {
	if maxSize <= 0 {
		maxSize = 32
	}
	return &KillRing{maxSize: maxSize}
}

// Push records a fresh kill. When prepend is true the new text is
// prepended to the head entry (used by backward-kill commands so the
// head matches the eventual paste order).
func (k *KillRing) Push(text string, prepend bool) {
	if text == "" {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.entries) == 0 {
		k.entries = append(k.entries, text)
		k.index = 0
		return
	}
	if prepend {
		k.entries[0] = text + k.entries[0]
	} else {
		k.entries[0] += text
	}
}

// PushNew starts a new entry at the head even if the previous kill was
// contiguous. Used when the user moves the cursor between kills.
func (k *KillRing) PushNew(text string) {
	if text == "" {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.entries = append([]string{text}, k.entries...)
	if len(k.entries) > k.maxSize {
		k.entries = k.entries[:k.maxSize]
	}
	k.index = 0
}

// Peek returns the current entry without rotating. Empty string when
// the ring has no entries.
func (k *KillRing) Peek() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.entries) == 0 {
		return ""
	}
	return k.entries[k.index]
}

// Rotate advances the internal pointer and returns the next entry. For
// yank-pop after an initial yank.
func (k *KillRing) Rotate() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.entries) == 0 {
		return ""
	}
	k.index = (k.index + 1) % len(k.entries)
	return k.entries[k.index]
}

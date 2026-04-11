package tui

import "testing"

func TestKeybindingsManagerDefaults(t *testing.T) {
	m := NewKeybindingsManager()
	keys := m.Keys(KBSubmit)
	found := false
	for _, k := range keys {
		if k == KeyEnter {
			found = true
		}
	}
	if !found {
		t.Errorf("submit should include enter: %v", keys)
	}
}

func TestKeybindingsMatchesCtrlLetter(t *testing.T) {
	m := NewKeybindingsManager()
	if !m.Matches([]byte{0x03}, KBSelectCancel) {
		t.Error("ctrl+c should match SelectCancel")
	}
}

func TestKeybindingsMatchesArrow(t *testing.T) {
	m := NewKeybindingsManager()
	if !m.Matches([]byte("\x1b[A"), KBCursorUp) {
		t.Error("up arrow should match CursorUp")
	}
}

func TestKeybindingsOverride(t *testing.T) {
	m := NewKeybindingsManager()
	m.Override(KBSubmit, []KeyID{"ctrl+s"})
	if !m.Matches([]byte{0x13}, KBSubmit) {
		t.Error("ctrl+s should match after override")
	}
	// Default enter should no longer match.
	if m.Matches([]byte{0x0D}, KBSubmit) {
		t.Error("enter should not match after override")
	}
}

func TestKeybindingsOverrideClear(t *testing.T) {
	m := NewKeybindingsManager()
	m.Override(KBSubmit, []KeyID{"ctrl+s"})
	m.Override(KBSubmit, nil)
	// Back to default.
	if !m.Matches([]byte{0x0D}, KBSubmit) {
		t.Error("enter should match after override cleared")
	}
}

func TestKeybindingsUnknownID(t *testing.T) {
	m := NewKeybindingsManager()
	if m.Matches([]byte{0x0D}, "tui.nonexistent") {
		t.Error("unknown id should not match")
	}
}

func TestKeybindingsDefinitionsImmutable(t *testing.T) {
	m := NewKeybindingsManager()
	defs := m.Definitions()
	delete(defs, KBSubmit)
	if m.Keys(KBSubmit) == nil {
		t.Error("mutating snapshot should not affect manager")
	}
}

func TestKeybindingsAllDefaultsHaveDescriptions(t *testing.T) {
	for id, def := range DefaultKeybindings {
		if def.Description == "" {
			t.Errorf("%s missing description", id)
		}
		if len(def.DefaultKeys) == 0 {
			t.Errorf("%s missing default keys", id)
		}
	}
}

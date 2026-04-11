package tui

import (
	"bytes"
	"testing"
)

func TestVirtualTerminalSatisfiesInterface(t *testing.T) {
	var _ TerminalIO = (*VirtualTerminal)(nil)
}

func TestVirtualTerminalWriteRecords(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	v.Write([]byte("hello"))
	v.Write([]byte(" world"))
	if !bytes.Equal(v.Dump(), []byte("hello world")) {
		t.Errorf("dump = %q", v.Dump())
	}
	writes := v.Writes()
	if len(writes) != 2 {
		t.Errorf("writes = %d", len(writes))
	}
	if v.LastOutput() != " world" {
		t.Errorf("last = %q", v.LastOutput())
	}
}

func TestVirtualTerminalDimensions(t *testing.T) {
	v := NewVirtualTerminal(120, 40)
	if v.Width() != 120 || v.Height() != 40 {
		t.Errorf("dims = %dx%d", v.Width(), v.Height())
	}
}

func TestVirtualTerminalResize(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	v.Resize(100, 30)
	if v.Width() != 100 || v.Height() != 30 {
		t.Errorf("dims = %dx%d", v.Width(), v.Height())
	}
	select {
	case <-v.ResizeCh():
	default:
		t.Error("resize event not delivered")
	}
}

func TestVirtualTerminalSimulateInput(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	go v.SimulateInput([]byte("abc"))
	select {
	case got := <-v.InputCh():
		if string(got) != "abc" {
			t.Errorf("got %q", string(got))
		}
	}
}

func TestVirtualTerminalStopClosesChans(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	v.Stop()
	if _, ok := <-v.InputCh(); ok {
		t.Error("input channel should be closed")
	}
	if _, ok := <-v.ResizeCh(); ok {
		t.Error("resize channel should be closed")
	}
}

func TestVirtualTerminalDoubleStopSafe(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	v.Stop()
	v.Stop()
}

func TestVirtualTerminalContainsText(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	v.Write([]byte("\x1b[31mhello\x1b[0m world"))
	if !v.ContainsText("hello") {
		t.Error("should find plain text")
	}
	if !v.ContainsText("world") {
		t.Error("should find text after ANSI")
	}
	if v.ContainsText("missing") {
		t.Error("missing should not match")
	}
}

func TestVirtualTerminalReset(t *testing.T) {
	v := NewVirtualTerminal(80, 24)
	v.Write([]byte("hello"))
	v.Reset()
	if len(v.Writes()) != 0 {
		t.Error("reset should clear writes")
	}
	if v.FullOutput() != "" {
		t.Error("reset should clear output")
	}
}

func TestStripANSI(t *testing.T) {
	got := stripANSI("\x1b[31mhello\x1b[0m world \x1b]8;;url\x07link\x1b]8;;\x07")
	want := "hello world link"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

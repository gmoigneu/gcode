package providers

import (
	"strings"
	"testing"
)

func TestSSESingleEvent(t *testing.T) {
	input := "data: hello\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	if !s.Scan() {
		t.Fatal("Scan returned false")
	}
	if s.Event().Data != "hello" {
		t.Errorf("data = %q", s.Event().Data)
	}
	if s.Scan() {
		t.Error("expected end of stream")
	}
}

func TestSSEMultipleEvents(t *testing.T) {
	input := "data: first\n\ndata: second\n\ndata: third\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	var got []string
	for s.Scan() {
		got = append(got, s.Event().Data)
	}
	want := []string{"first", "second", "third"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSSEEventAndData(t *testing.T) {
	input := "event: message_start\ndata: {\"type\":\"m\"}\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	if !s.Scan() {
		t.Fatal("Scan returned false")
	}
	e := s.Event()
	if e.Event != "message_start" {
		t.Errorf("event = %q", e.Event)
	}
	if e.Data != `{"type":"m"}` {
		t.Errorf("data = %q", e.Data)
	}
}

func TestSSEMultiLineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	if !s.Scan() {
		t.Fatal("no event")
	}
	if s.Event().Data != "line1\nline2\nline3" {
		t.Errorf("data = %q", s.Event().Data)
	}
}

func TestSSEComments(t *testing.T) {
	input := ": this is a comment\ndata: hello\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	if !s.Scan() {
		t.Fatal("no event")
	}
	if s.Event().Data != "hello" {
		t.Errorf("data = %q", s.Event().Data)
	}
}

func TestSSEEmpty(t *testing.T) {
	s := NewSSEScanner(strings.NewReader(""))
	if s.Scan() {
		t.Error("empty stream should not produce events")
	}
}

func TestSSEDoneSentinel(t *testing.T) {
	// The scanner does NOT interpret [DONE] specially - callers do.
	input := "data: {\"chunk\":1}\n\ndata: [DONE]\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	var got []string
	for s.Scan() {
		got = append(got, s.Event().Data)
	}
	if len(got) != 2 || got[1] != "[DONE]" {
		t.Errorf("got %v", got)
	}
}

func TestSSEDataWithoutLeadingSpace(t *testing.T) {
	input := "data:no-leading-space\n\n"
	s := NewSSEScanner(strings.NewReader(input))
	if !s.Scan() {
		t.Fatal("no event")
	}
	if s.Event().Data != "no-leading-space" {
		t.Errorf("data = %q", s.Event().Data)
	}
}

func TestSSEPartialTrailingEvent(t *testing.T) {
	// Stream ending without a trailing blank line should still yield the
	// pending event.
	input := "data: final"
	s := NewSSEScanner(strings.NewReader(input))
	if !s.Scan() {
		t.Fatal("trailing event missing")
	}
	if s.Event().Data != "final" {
		t.Errorf("data = %q", s.Event().Data)
	}
}

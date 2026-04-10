package providers

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent is one parsed Server-Sent-Events message. Only the fields gcode
// needs (event name and data payload) are exposed; id and retry are ignored.
type SSEEvent struct {
	Event string
	Data  string
}

// SSEScanner reads Server-Sent-Events from an io.Reader. It handles blank
// lines as message terminators, multi-line data fields, comments, and
// trailing messages that are not followed by a blank line.
//
// The scanner does not interpret "[DONE]" sentinel payloads specially;
// callers are responsible for breaking out of their loop when they see one.
type SSEScanner struct {
	s     *bufio.Scanner
	event SSEEvent
	done  bool
}

// NewSSEScanner wraps r. The scanner's line buffer is sized to 1 MiB to
// accommodate large JSON chunks.
func NewSSEScanner(r io.Reader) *SSEScanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 1<<20)
	return &SSEScanner{s: s}
}

// Scan advances to the next SSE message. It returns false when the stream
// is exhausted or the underlying reader errors. The most recent event is
// available via Event.
func (sc *SSEScanner) Scan() bool {
	if sc.done {
		return false
	}

	var eventName strings.Builder
	var data strings.Builder
	haveField := false

	for sc.s.Scan() {
		line := sc.s.Text()
		if line == "" {
			if haveField {
				sc.event = SSEEvent{Event: eventName.String(), Data: data.String()}
				return true
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, ":"):
			// comment — ignore
		case strings.HasPrefix(line, "event:"):
			eventName.WriteString(strings.TrimSpace(line[len("event:"):]))
			haveField = true
		case strings.HasPrefix(line, "data:"):
			rest := line[len("data:"):]
			if strings.HasPrefix(rest, " ") {
				rest = rest[1:]
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(rest)
			haveField = true
		}
	}

	if haveField {
		sc.event = SSEEvent{Event: eventName.String(), Data: data.String()}
		sc.done = true
		return true
	}
	sc.done = true
	return false
}

// Event returns the most recently scanned event. It is only valid after a
// successful Scan call.
func (sc *SSEScanner) Event() SSEEvent { return sc.event }

// Err returns any error from the underlying reader.
func (sc *SSEScanner) Err() error { return sc.s.Err() }

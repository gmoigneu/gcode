package ai

import (
	"encoding/json"
	"strings"
)

// ParseStreamingJSON parses a possibly-truncated JSON object and returns a
// best-effort map. It never returns an error. On total failure (empty input,
// garbage, non-object values, unparseable fragments) it returns an empty
// non-nil map.
//
// For complete input it falls through to encoding/json. For truncated input
// it scans the prefix, records the longest position at which the input can
// be closed into a valid JSON value, and attempts to parse that closure.
func ParseStreamingJSON(partial string) map[string]any {
	if partial == "" {
		return map[string]any{}
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(partial), &m); err == nil {
		if m == nil {
			return map[string]any{}
		}
		return m
	}

	closed := closePartialJSON(partial)
	if closed != "" {
		if err := json.Unmarshal([]byte(closed), &m); err == nil {
			if m == nil {
				return map[string]any{}
			}
			return m
		}
	}
	return map[string]any{}
}

// closePartialJSON walks partial input and returns the longest prefix that
// can be closed into a valid JSON document, with the appropriate trailing
// brackets appended. Returns "" when no valid closure exists.
func closePartialJSON(s string) string {
	var stack []byte // '{' or '['
	lastSafe := ""

	markSafe := func(pos int) {
		b := make([]byte, 0, pos+len(stack))
		b = append(b, s[:pos]...)
		for j := len(stack) - 1; j >= 0; j-- {
			if stack[j] == '{' {
				b = append(b, '}')
			} else {
				b = append(b, ']')
			}
		}
		lastSafe = string(b)
	}

	// Parser states:
	//   v = expect value
	//   V = inside array, expect value or ]
	//   k = inside object, expect key or }
	//   : = expect :
	//   , = after value, expect , or container close
	//   D = top-level done, nothing more allowed
	expect := byte('v')
	i := 0
	n := len(s)

loop:
	for i < n {
		// Skip whitespace.
		for i < n {
			c := s[i]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				i++
				continue
			}
			break
		}
		if i >= n {
			break
		}
		c := s[i]

		switch expect {
		case 'v':
			switch {
			case c == '{':
				stack = append(stack, '{')
				i++
				expect = 'k'
				markSafe(i)
			case c == '[':
				stack = append(stack, '[')
				i++
				expect = 'V'
				markSafe(i)
			case c == '"':
				if !scanJSONString(s, &i) {
					break loop
				}
				expect = ','
				markSafe(i)
			case c == 't' || c == 'f' || c == 'n':
				if !scanJSONLiteral(s, &i) {
					break loop
				}
				expect = ','
				markSafe(i)
			case c == '-' || (c >= '0' && c <= '9'):
				if !scanJSONNumber(s, &i) {
					break loop
				}
				expect = ','
				markSafe(i)
			default:
				break loop
			}
		case 'V':
			if c == ']' {
				if len(stack) == 0 || stack[len(stack)-1] != '[' {
					break loop
				}
				stack = stack[:len(stack)-1]
				i++
				if len(stack) == 0 {
					markSafe(i)
					expect = 'D'
				} else {
					expect = ','
					markSafe(i)
				}
				continue
			}
			expect = 'v'
			// reprocess c as a value in the next iteration
			continue
		case 'k':
			switch c {
			case '}':
				if len(stack) == 0 || stack[len(stack)-1] != '{' {
					break loop
				}
				stack = stack[:len(stack)-1]
				i++
				if len(stack) == 0 {
					markSafe(i)
					expect = 'D'
				} else {
					expect = ','
					markSafe(i)
				}
			case '"':
				if !scanJSONString(s, &i) {
					break loop
				}
				expect = ':'
			default:
				break loop
			}
		case ':':
			if c != ':' {
				break loop
			}
			i++
			expect = 'v'
		case ',':
			switch c {
			case ',':
				i++
				if len(stack) == 0 {
					break loop
				}
				if stack[len(stack)-1] == '{' {
					expect = 'k'
				} else {
					expect = 'V'
				}
			case '}':
				if len(stack) == 0 || stack[len(stack)-1] != '{' {
					break loop
				}
				stack = stack[:len(stack)-1]
				i++
				if len(stack) == 0 {
					markSafe(i)
					expect = 'D'
				} else {
					expect = ','
					markSafe(i)
				}
			case ']':
				if len(stack) == 0 || stack[len(stack)-1] != '[' {
					break loop
				}
				stack = stack[:len(stack)-1]
				i++
				if len(stack) == 0 {
					markSafe(i)
					expect = 'D'
				} else {
					expect = ','
					markSafe(i)
				}
			default:
				break loop
			}
		case 'D':
			break loop
		}
	}

	return lastSafe
}

// scanJSONString advances *i past a complete JSON string, returning true on
// success. It returns false if the input ends mid-string.
func scanJSONString(s string, i *int) bool {
	p := *i
	if p >= len(s) || s[p] != '"' {
		return false
	}
	p++
	for p < len(s) {
		c := s[p]
		if c == '\\' {
			p++
			if p >= len(s) {
				return false
			}
			p++
			continue
		}
		if c == '"' {
			p++
			*i = p
			return true
		}
		p++
	}
	return false
}

// scanJSONNumber advances *i past a complete JSON number, returning true on
// success. It returns false if the number is incomplete (e.g. "1." or "1e")
// or if no digits are present.
func scanJSONNumber(s string, i *int) bool {
	p := *i
	start := p
	if p < len(s) && s[p] == '-' {
		p++
	}
	if p >= len(s) {
		return false
	}
	switch {
	case s[p] == '0':
		p++
	case s[p] >= '1' && s[p] <= '9':
		for p < len(s) && s[p] >= '0' && s[p] <= '9' {
			p++
		}
	default:
		return false
	}
	if p < len(s) && s[p] == '.' {
		p++
		fracStart := p
		for p < len(s) && s[p] >= '0' && s[p] <= '9' {
			p++
		}
		if p == fracStart {
			return false
		}
	}
	if p < len(s) && (s[p] == 'e' || s[p] == 'E') {
		p++
		if p < len(s) && (s[p] == '+' || s[p] == '-') {
			p++
		}
		expStart := p
		for p < len(s) && s[p] >= '0' && s[p] <= '9' {
			p++
		}
		if p == expStart {
			return false
		}
	}
	if p == start {
		return false
	}
	*i = p
	return true
}

// scanJSONLiteral advances *i past one of the literals true, false, or null.
func scanJSONLiteral(s string, i *int) bool {
	for _, want := range []string{"true", "false", "null"} {
		if strings.HasPrefix(s[*i:], want) {
			*i += len(want)
			return true
		}
	}
	return false
}

// Package agent runs a rendered prompt against a model: a conversation that remembers its history,
// streams the answer, checks it against the prompt's contract, and applies the safety rules of
// docs/AGENT_RUNTIME.md (permissions on what may be read or written, a sanitised terminal).
package agent

import "strings"

// Sanitizer removes terminal control sequences from text as it streams in. A model's reply is
// untrusted: printed raw to a terminal, escape sequences can retitle the window, move the cursor
// to hide text, or write to the clipboard (OSC 52). Newlines and tabs are kept.
//
// It is stateful because a sequence can be split across two chunks of a stream.
type Sanitizer struct {
	state sanitizeState
}

type sanitizeState int

const (
	stText      sanitizeState = iota
	stEsc                     // saw ESC
	stCSI                     // ESC [ ... until a final byte
	stString                  // ESC ] / P / _ / ^ / X ... until BEL or ST (ESC \)
	stStringEsc               // saw ESC inside a string: "\" ends it
)

// Filter returns chunk with control sequences and control characters removed.
func (s *Sanitizer) Filter(chunk string) string {
	var out strings.Builder
	for _, r := range chunk {
		switch s.state {
		case stText:
			switch {
			case r == 0x1b:
				s.state = stEsc
			case r == '\n' || r == '\t':
				out.WriteRune(r)
			case r == '\r':
				// a bare CR overwrites the line; treat it as a newline instead
				out.WriteByte('\n')
			case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
				// other C0, DEL and C1 controls (0x9b is an 8-bit CSI, 0x9d an 8-bit OSC)
			default:
				out.WriteRune(r)
			}
		case stEsc:
			switch r {
			case '[':
				s.state = stCSI
			case ']', 'P', '_', '^', 'X':
				s.state = stString
			default:
				s.state = stText // a two-character sequence such as ESC c; swallowed
			}
		case stCSI:
			if r >= 0x40 && r <= 0x7e { // the final byte ends the sequence
				s.state = stText
			}
		case stString:
			switch r {
			case 0x07: // BEL ends an OSC
				s.state = stText
			case 0x1b:
				s.state = stStringEsc
			}
		case stStringEsc:
			if r == '\\' {
				s.state = stText
			} else {
				s.state = stString
			}
		}
	}
	return out.String()
}

// Sanitize is Filter for a complete string.
func Sanitize(s string) string {
	var f Sanitizer
	return f.Filter(s)
}

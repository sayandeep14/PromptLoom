package locker

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	icrypto "github.com/sayandeep14/PromptLoom/loomlocker/internal/crypto"
)

func isJSON(file string) bool {
	return strings.EqualFold(filepath.Ext(file), ".json")
}

// ---- JSON ----

// lockJSONText locks the string at a dotted path (object keys, and array positions written as
// numbers: "servers.0.password"). Like YAML, the file is edited as TEXT at the value's exact
// byte span, so formatting, key order, indentation and every other value are untouched. The
// value must be a JSON string: a number or boolean would change type when replaced by a token.
func lockJSONText(plan *filePlan, dotPath string) error {
	const bom = "\xef\xbb\xbf"
	text, base := plan.text, 0
	if strings.HasPrefix(text, bom) {
		text, base = text[len(bom):], len(bom)
	}
	if !json.Valid([]byte(text)) {
		return fmt.Errorf("parse JSON: the file is not valid JSON (comments and trailing commas are not supported)")
	}

	w := &jsonWalker{dec: json.NewDecoder(strings.NewReader(text)), text: text}
	w.dec.UseNumber()
	sp, err := w.find(strings.Split(dotPath, "."))
	if err != nil {
		return err
	}
	if !sp.isString {
		return fmt.Errorf("value at %q is %s, and only strings can be locked (write it as a JSON string, \"...\")", dotPath, sp.kind)
	}
	if sp.value == "" || icrypto.IsToken(sp.value) {
		return nil // nothing to hide, or already locked
	}

	start, end := base+sp.start, base+sp.end
	raw := plan.text[start:end]
	locked := `"` + icrypto.RandomToken() + `"`
	plan.text = plan.text[:start] + locked + plan.text[end:]
	plan.mapping[locked] = raw
	return nil
}

// span is where a value sits in the text.
type span struct {
	start, end int
	isString   bool
	value      string // the decoded string
	kind       string // for errors: "an object", "a number", ...
}

type jsonWalker struct {
	dec  *json.Decoder
	text string
}

// next reads one token and its byte span (delimiters and separators between tokens are skipped).
func (w *jsonWalker) next() (tok json.Token, start, end int, err error) {
	before := int(w.dec.InputOffset())
	tok, err = w.dec.Token()
	if err != nil {
		return nil, 0, 0, err
	}
	end = int(w.dec.InputOffset())
	start = before
	for start < end && strings.IndexByte(" \t\r\n,:", w.text[start]) >= 0 {
		start++
	}
	return tok, start, end, nil
}

func kindOf(tok json.Token) string {
	switch v := tok.(type) {
	case json.Delim:
		if v == '{' {
			return "an object"
		}
		return "an array"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	}
	return "a string"
}

// find reads the value at the current position and returns the span of the value at the path.
func (w *jsonWalker) find(parts []string) (span, error) {
	tok, start, end, err := w.next()
	if err != nil {
		return span{}, err
	}
	d, isDelim := tok.(json.Delim)

	if len(parts) == 0 {
		// this IS the target
		if isDelim {
			return span{}, fmt.Errorf("the path leads to %s, not a value you can lock", kindOf(tok))
		}
		s, isStr := tok.(string)
		return span{start: start, end: end, isString: isStr, value: s, kind: kindOf(tok)}, nil
	}
	if !isDelim {
		return span{}, fmt.Errorf("JSON key %q not found (its parent is %s, not an object or array)", parts[0], kindOf(tok))
	}

	switch d {
	case '{':
		var found *span
		for w.dec.More() {
			keyTok, _, _, err := w.next()
			if err != nil {
				return span{}, err
			}
			key, _ := keyTok.(string)
			if key != parts[0] {
				if err := w.skip(); err != nil {
					return span{}, err
				}
				continue
			}
			if found != nil {
				return span{}, fmt.Errorf("JSON key %q appears more than once; which one to lock is ambiguous", parts[0])
			}
			sp, err := w.find(parts[1:])
			if err != nil {
				return span{}, err
			}
			found = &sp
		}
		if _, _, _, err := w.next(); err != nil { // the closing }
			return span{}, err
		}
		if found == nil {
			return span{}, fmt.Errorf("JSON key %q not found", parts[0])
		}
		return *found, nil

	default: // '['
		idx, convErr := strconv.Atoi(parts[0])
		var found *span
		for i := 0; w.dec.More(); i++ {
			if convErr == nil && i == idx {
				sp, err := w.find(parts[1:])
				if err != nil {
					return span{}, err
				}
				found = &sp
				continue
			}
			if err := w.skip(); err != nil {
				return span{}, err
			}
		}
		if _, _, _, err := w.next(); err != nil { // the closing ]
			return span{}, err
		}
		if convErr != nil {
			return span{}, fmt.Errorf("%q is not a position in an array (use a number, as in servers.0.password)", parts[0])
		}
		if found == nil {
			return span{}, fmt.Errorf("array position %d not found", idx)
		}
		return *found, nil
	}
}

// skip consumes one whole value.
func (w *jsonWalker) skip() error {
	tok, _, _, err := w.next()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || (d != '{' && d != '[') {
		return nil
	}
	for depth := 1; depth > 0; {
		t, _, _, err := w.next()
		if err == io.EOF {
			return fmt.Errorf("unexpected end of JSON")
		}
		if err != nil {
			return err
		}
		if dd, isD := t.(json.Delim); isD {
			if dd == '{' || dd == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
}

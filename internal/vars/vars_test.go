package vars

import (
	"reflect"
	"testing"
)

func TestTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"no tokens here", nil},
		{"{{a}}", []string{"a"}},
		{"{{ a }} and {{b}} and {{  c  }}", []string{"a", "b", "c"}},
		{"{{b}} {{a}} {{b}}", []string{"a", "b"}}, // sorted, de-duplicated
		{"{{repo_name}} {{team-name}} {{X9}}", []string{"X9", "repo_name", "team-name"}},
		{"{{ not valid }} {{a.b}} {{}} { {a} } {{a", nil},
		{"multi\nline {{x}}\n{{y}}", []string{"x", "y"}},
	}
	for _, c := range cases {
		if got := Tokens(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Tokens(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSubstituteString(t *testing.T) {
	out, unresolved := SubstituteString("Hi {{ name }}, welcome to {{place}}. {{name}}!", map[string]string{"name": "Ana", "place": "Loom"})
	if out != "Hi Ana, welcome to Loom. Ana!" || unresolved != nil {
		t.Errorf("out=%q unresolved=%v", out, unresolved)
	}

	out, unresolved = SubstituteString("{{a}} {{b}} {{a}} {{c}}", map[string]string{"a": "1"})
	if out != "1 {{b}} 1 {{c}}" {
		t.Errorf("unresolved tokens are left in place: %q", out)
	}
	if !reflect.DeepEqual(unresolved, []string{"b", "c"}) {
		t.Errorf("unresolved (sorted, once each): %v", unresolved)
	}
}

func TestSubstituteValuesAreNotReExpanded(t *testing.T) {
	// a value that itself looks like a token must be inserted literally, once
	out, _ := SubstituteString("{{a}}", map[string]string{"a": "{{b}}", "b": "SECRET"})
	if out != "{{b}}" {
		t.Errorf("substitution must not recurse: %q", out)
	}
	out, _ = SubstituteString("{{a}}", map[string]string{"a": "$1 \\n ${x} %s"})
	if out != "$1 \\n ${x} %s" {
		t.Errorf("special characters in values must be inserted verbatim: %q", out)
	}
}

// Pinned behaviour: an explicitly EMPTY value counts as "not provided", so the placeholder
// stays and is reported as unresolved (this is what surfaces a forgotten required slot).
func TestEmptyValueCountsAsUnresolved(t *testing.T) {
	out, unresolved := SubstituteString("x {{a}} y", map[string]string{"a": ""})
	if out != "x {{a}} y" || !reflect.DeepEqual(unresolved, []string{"a"}) {
		t.Errorf("out=%q unresolved=%v", out, unresolved)
	}
}

func TestSubstituteWithoutTokensOrValues(t *testing.T) {
	for _, vals := range []map[string]string{nil, {}} {
		out, unresolved := SubstituteString("plain text", vals)
		if out != "plain text" || unresolved != nil {
			t.Errorf("%q %v", out, unresolved)
		}
	}
}

package semantic

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/diff"
)

func changed(field string, mutate func(*diff.FieldDiff)) diff.FieldDiff {
	d := diff.FieldDiff{Field: field, Changed: true}
	mutate(&d)
	return d
}

func labels(cs []ChangeClass) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Label+":"+string(c.Risk))
	}
	return strings.Join(out, ",")
}

func TestUnchangedFieldsProduceNothing(t *testing.T) {
	if got := Classify([]diff.FieldDiff{{Field: "constraints"}, {Field: "persona"}}); len(got) != 0 {
		t.Errorf("%v", got)
	}
	if got := Classify(nil); got != nil {
		t.Errorf("%v", got)
	}
}

func TestClassificationAndRisk(t *testing.T) {
	cases := []struct {
		name string
		in   diff.FieldDiff
		want string
	}{
		{"constraint added", changed("constraints", func(d *diff.FieldDiff) { d.Added = []string{"x"} }), "constraint-added:medium"},
		{"constraint removed is high risk", changed("constraints", func(d *diff.FieldDiff) { d.Removed = []string{"x"} }), "constraint-removed:high"},
		{"both directions", changed("constraints", func(d *diff.FieldDiff) { d.Added = []string{"a"}; d.Removed = []string{"b"} }),
			"constraint-added:medium,constraint-removed:high"},
		{"format", changed("format", func(d *diff.FieldDiff) { d.Added = []string{"a"} }), "format-changed:low"},
		{"objective", changed("objective", func(d *diff.FieldDiff) { d.Before, d.After = "a", "b" }), "objective-changed:medium"},
		{"persona", changed("persona", func(d *diff.FieldDiff) { d.Before, d.After = "a", "b" }), "persona-changed:low"},
		{"instruction added", changed("instructions", func(d *diff.FieldDiff) { d.Added = []string{"a"} }), "capability-added:low"},
		{"instruction removed", changed("instructions", func(d *diff.FieldDiff) { d.Removed = []string{"a"} }), "capability-removed:medium"},
		{"inheritance", changed("inheritance", func(d *diff.FieldDiff) { d.Before, d.After = "A", "B" }), "inheritance-changed:high"},
		{"summary", changed("summary", func(d *diff.FieldDiff) { d.Before, d.After = "a", "b" }), "notes-updated:low"},
		{"notes", changed("notes", func(d *diff.FieldDiff) { d.Before, d.After = "a", "b" }), "notes-updated:low"},
		{"context", changed("context", func(d *diff.FieldDiff) { d.Before, d.After = "a", "b" }), "notes-updated:low"},
		{"examples", changed("examples", func(d *diff.FieldDiff) { d.Added = []string{"a"} }), "examples-changed:low"},
		{"a field with no rule is ignored", changed("todo", func(d *diff.FieldDiff) { d.Added = []string{"a"} }), ""},
	}
	for _, c := range cases {
		if got := labels(Classify([]diff.FieldDiff{c.in})); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestItemsCarryTheChange(t *testing.T) {
	cs := Classify([]diff.FieldDiff{
		changed("constraints", func(d *diff.FieldDiff) { d.Removed = []string{"never x"} }),
		changed("objective", func(d *diff.FieldDiff) { d.Before, d.After = "old", "new" }),
		changed("format", func(d *diff.FieldDiff) { d.Removed = []string{"A"}; d.Added = []string{"B"} }),
	})
	if len(cs) != 3 || cs[0].Items[0] != "never x" || cs[1].Items[0] != "old" || cs[1].Items[1] != "new" {
		t.Fatalf("%+v", cs)
	}
	// removed items are marked with "- " so the renderer can colour them
	if strings.Join(cs[2].Items, "|") != "- A|B" {
		t.Errorf("format items: %v", cs[2].Items)
	}
}

func TestOrderFollowsTheDiff(t *testing.T) {
	cs := Classify([]diff.FieldDiff{
		changed("persona", func(d *diff.FieldDiff) { d.Before = "a" }),
		changed("inheritance", func(d *diff.FieldDiff) { d.Before = "a" }),
	})
	if labels(cs) != "persona-changed:low,inheritance-changed:high" {
		t.Errorf("%s", labels(cs))
	}
}

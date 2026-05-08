package semantic

import "github.com/sayandeepgiri/promptloom/internal/diff"

// RiskLevel classifies the impact of a semantic change.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// ChangeClass describes one classified semantic change.
type ChangeClass struct {
	Label string    // "constraint-added", "constraint-removed", etc.
	Risk  RiskLevel
	Items []string // the specific lines that changed
}

// Classify maps a slice of FieldDiff into semantic change class labels.
func Classify(diffs []diff.FieldDiff) []ChangeClass {
	var classes []ChangeClass

	for _, fd := range diffs {
		if !fd.Changed {
			continue
		}

		switch fd.Field {
		case "constraints":
			if len(fd.Added) > 0 {
				classes = append(classes, ChangeClass{
					Label: "constraint-added",
					Risk:  RiskMedium,
					Items: fd.Added,
				})
			}
			if len(fd.Removed) > 0 {
				classes = append(classes, ChangeClass{
					Label: "constraint-removed",
					Risk:  RiskHigh,
					Items: fd.Removed,
				})
			}

		case "format":
			classes = append(classes, ChangeClass{
				Label: "format-changed",
				Risk:  RiskLow,
				Items: markedItems(fd.Removed, fd.Added),
			})

		case "objective":
			classes = append(classes, ChangeClass{
				Label: "objective-changed",
				Risk:  RiskMedium,
				Items: []string{fd.Before, fd.After},
			})

		case "persona":
			classes = append(classes, ChangeClass{
				Label: "persona-changed",
				Risk:  RiskLow,
				Items: []string{fd.Before, fd.After},
			})

		case "instructions":
			if len(fd.Added) > 0 {
				classes = append(classes, ChangeClass{
					Label: "capability-added",
					Risk:  RiskLow,
					Items: fd.Added,
				})
			}
			if len(fd.Removed) > 0 {
				classes = append(classes, ChangeClass{
					Label: "capability-removed",
					Risk:  RiskMedium,
					Items: fd.Removed,
				})
			}

		case "inheritance":
			classes = append(classes, ChangeClass{
				Label: "inheritance-changed",
				Risk:  RiskHigh,
				Items: []string{fd.Before, fd.After},
			})

		case "summary", "notes", "context":
			classes = append(classes, ChangeClass{
				Label: "notes-updated",
				Risk:  RiskLow,
				Items: []string{fd.Before, fd.After},
			})

		case "examples":
			classes = append(classes, ChangeClass{
				Label: "examples-changed",
				Risk:  RiskLow,
				Items: markedItems(fd.Removed, fd.Added),
			})
		}
	}

	return classes
}

// markedItems builds a single slice with removed items prefixed by "- " and
// added items left as-is. The renderer uses the "- " prefix to colour-code them.
func markedItems(removed, added []string) []string {
	out := make([]string, 0, len(removed)+len(added))
	for _, r := range removed {
		out = append(out, "- "+r)
	}
	out = append(out, added...)
	return out
}

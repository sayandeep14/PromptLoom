// Package tokens provides a lightweight token-count estimator for prompt content.
// It approximates the cl100k_base tokenizer at ~1.3 tokens per word.
package tokens

import "strings"

// Estimate returns a rough token count for the given text.
func Estimate(text string) int {
	if text == "" {
		return 0
	}
	words := len(strings.Fields(text))
	return int(float64(words)*1.3 + 0.5)
}

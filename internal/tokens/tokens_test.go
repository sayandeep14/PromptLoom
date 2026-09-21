package tokens

import "testing"

func TestEstimate(t *testing.T) {
	cases := map[string]int{
		"":                        0,
		"   \n\t ":                0,
		"one":                     1, // 1.3 rounds to 1
		"two words":               3, // 2.6 -> 3
		"a b c d e f g h i j":     13,
		"  leading   and\ttabs\n": 4, // 3 words -> 3.9 -> 4
	}
	for in, want := range cases {
		if got := Estimate(in); got != want {
			t.Errorf("Estimate(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestEstimateGrowsWithLength(t *testing.T) {
	prev := 0
	text := ""
	for i := 0; i < 50; i++ {
		text += "word "
		got := Estimate(text)
		if got < prev {
			t.Fatalf("the estimate must never decrease as text grows: %d -> %d", prev, got)
		}
		prev = got
	}
}

// Known limitation, pinned so a change is deliberate: the estimate counts whitespace-separated
// words, so text without spaces (Chinese, Japanese) is badly underestimated.
func TestEstimateUnderCountsUnspacedScripts(t *testing.T) {
	if got := Estimate("これは長い日本語の文章ですがスペースがありません"); got != 1 {
		t.Errorf("got %d; if this changed, update the docs on token estimates", got)
	}
}

package extract

import "testing"

func TestIsJunkClaim(t *testing.T) {
	junk := []string{
		// Greetings
		"Good morning",
		"good morning!",
		"Hi there",
		"Hey Felix",
		"Hello",
		"Cheers",
		"Thanks",
		"Thank you",
		// Acknowledgements
		"Done",
		"OK",
		"Yes",
		"No",
		"Sure",
		"Got it",
		"Noted",
		// Emoji-led acks
		"✅ Done",
		"👍",
		"✅",
		// Section labels (end in colon, short)
		"So you need:",
		"The event details are:",
		"Description:",
		"Time:",
		"Items:",
		// Empty / whitespace
		"",
		"   ",
		"   .  ",
	}
	for _, s := range junk {
		t.Run("junk:"+s, func(t *testing.T) {
			if !isJunkClaim(s) {
				t.Fatalf("expected isJunkClaim(%q) = true", s)
			}
		})
	}

	keep := []string{
		"Revenue grew 15% in Q3",
		"We will migrate to PostgreSQL",
		"Users might prefer dark mode",
		"The kindergarten flea market is on Saturday",
		"Felix needs coffee and tongs",
		"Time: 8:30 AM - 2:00 PM (CEST)", // colon present but content-bearing payload
		"The team decided to use PostgreSQL after evaluating three databases",
		"Good morning sets the tone for productive standups", // greeting word in content position
	}
	for _, s := range keep {
		t.Run("keep:"+s, func(t *testing.T) {
			if isJunkClaim(s) {
				t.Fatalf("expected isJunkClaim(%q) = false", s)
			}
		})
	}
}

func TestStripDecorations(t *testing.T) {
	cases := map[string]string{
		"✅ Done":       "Done",
		"👍 noted":      "noted",
		"plain text":   "plain text",
		"":             "",
		"✅":            "",
		"100% revenue": "100% revenue", // % is punct, not symbol
	}
	for in, want := range cases {
		if got := stripDecorations(in); got != want {
			t.Errorf("stripDecorations(%q) = %q, want %q", in, got, want)
		}
	}
}

package extract

import (
	"fmt"
	"strings"
)

const systemPrompt = `You are Mnemos, a knowledge extraction engine. Your job is to extract discrete, evidence-backed claims from source text.

Rules:
1. Each claim must be a single, self-contained statement of fact, decision, or hypothesis.
2. Preserve the original meaning — do not infer, speculate, or add information not present in the source.
3. Classify each claim:
   - "fact": An objective statement or observation (e.g., "Revenue grew 15% in Q3")
   - "decision": A choice, plan, or commitment (e.g., "We will migrate to PostgreSQL")
   - "hypothesis": A belief, assumption, or uncertain prediction (e.g., "Users might prefer dark mode")
4. Assign confidence (0.50–0.95):
   - Higher (0.80–0.95) for statements with data, measurements, confirmed observations, or explicit decisions
   - Medium (0.65–0.79) for general facts or stated plans without strong evidence
   - Lower (0.50–0.64) for hedged language, speculation, or hypotheses
5. Return ONLY valid JSON — no markdown fences, no commentary.
6. If the text contains no extractable claims, return an empty array: []

Output format — a JSON array of objects:
[
  {
    "text": "the claim text",
    "type": "fact|decision|hypothesis",
    "confidence": 0.85
  }
]`

// buildExtractionPrompt creates the user-turn prompt for claim extraction.
func buildExtractionPrompt(texts []string) string {
	var b strings.Builder
	b.WriteString("Extract all claims from the following source text(s).\n\n")

	for i, text := range texts {
		if len(texts) > 1 {
			fmt.Fprintf(&b, "--- Source %d ---\n", i+1)
		}
		b.WriteString(text)
		b.WriteString("\n\n")
	}

	return b.String()
}

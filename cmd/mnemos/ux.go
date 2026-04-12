package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

func printWelcome() {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Mnemos - Local-first knowledge engine")
	fmt.Println("  Eliminating AI hallucination through evidence-backed claims")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("")
	fmt.Println("  Quick start:")
	fmt.Println("    mnemos process --text \"Your text here\"")
	fmt.Println("    mnemos query \"Your question\"")
	fmt.Println("")
	fmt.Println("  Documentation: https://github.com/felixgeelhaar/mnemos")
	fmt.Println("")
}

func printExtractionSummary(claims []domain.Claim, rels []domain.Relationship) {
	facts := 0
	decisions := 0
	hypotheses := 0
	contested := 0
	contradictions := 0

	for _, c := range claims {
		switch c.Type {
		case domain.ClaimTypeFact:
			facts++
		case domain.ClaimTypeDecision:
			decisions++
		case domain.ClaimTypeHypothesis:
			hypotheses++
		}
		if c.Status == domain.ClaimStatusContested {
			contested++
		}
	}

	for _, r := range rels {
		if r.Type == domain.RelationshipTypeContradicts {
			contradictions++
		}
	}

	fmt.Println("")
	fmt.Println("  Extraction Summary:")
	fmt.Println("  ┌─────────────────────────────────────────┐")
	fmt.Printf("  │ Facts:       %-5d                      │\n", facts)
	fmt.Printf("  │ Decisions:   %-5d                      │\n", decisions)
	fmt.Printf("  │ Hypotheses: %-5d                      │\n", hypotheses)
	if contested > 0 {
		fmt.Printf("  │ Contested:  %-5d ⚠️                     │\n", contested)
	}
	if contradictions > 0 {
		fmt.Printf("  │ Contradictions: %-3d ⚠️                │\n", contradictions)
	}
	fmt.Println("  └─────────────────────────────────────────┘")
	fmt.Println("")

	if contested > 0 || contradictions > 0 {
		fmt.Println("  Tip: Run 'mnemos query --run <run-id> \"What contradicts?\"' to see details")
	}
}

func printFirstRunHints() {
	fmt.Println("  💡 Tips:")
	fmt.Println("    • Use 'mnemos process --text <text>' for quick extraction")
	fmt.Println("    • Use 'mnemos query <question>' to ask about your knowledge")
	fmt.Println("    • Use '--run <id>' with query to scope to a specific run")
	fmt.Println("")
}

func isFirstRun(dbPath string) bool {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		dir := filepath.Dir(dbPath)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return true
		}
		return true
	}
	return false
}

func formatHint(cmd string) string {
	hints := map[string]string{
		"ingest":    "Tip: After ingest, run 'mnemos extract <event-id>' to extract claims",
		"extract":   "Tip: After extract, run 'mnemos relate' to detect relationships",
		"relate":    "Tip: After relate, run 'mnemos query <question>' to get answers",
		"process":   "Tip: Run 'mnemos query --run <run-id> <question>' to query this run",
		"query":     "Tip: Use 'mnemos process --text <text>' to add more knowledge",
		"no_claims": "Tip: Try a longer text with more complete sentences",
		"need_more": "Tip: Need at least 2 claims to detect contradictions",
		"not_found": "Tip: Run 'mnemos ingest' first to add content",
	}
	if hint, ok := hints[cmd]; ok {
		return fmt.Sprintf("\n  %s\n", hint)
	}
	return ""
}

func printClaimPreview(claims []domain.Claim, maxDisplay int) {
	if len(claims) == 0 {
		return
	}

	fmt.Println("  Recent Claims:")
	for i := 0; i < len(claims) && i < maxDisplay; i++ {
		c := claims[i]
		typeIcon := "•"
		switch c.Type {
		case domain.ClaimTypeDecision:
			typeIcon = "✓"
		case domain.ClaimTypeHypothesis:
			typeIcon = "?"
		}
		status := ""
		if c.Status == domain.ClaimStatusContested {
			status = " [CONFLICT]"
		}
		text := c.Text
		if len(text) > 50 {
			text = text[:47] + "..."
		}
		fmt.Printf("    %s %s%s\n", typeIcon, text, status)
	}
	if len(claims) > maxDisplay {
		fmt.Printf("    ... and %d more\n", len(claims)-maxDisplay)
	}
}

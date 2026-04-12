package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/extract"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"gopkg.in/yaml.v3"
)

type TestCase struct {
	ID                string          `yaml:"id"`
	Description       string          `yaml:"description"`
	Tags              []string        `yaml:"tags"`
	Input             string          `yaml:"input"`
	ExpectedClaims    []ExpectedClaim `yaml:"expected_claims"`
	NotExpectedClaims []string        `yaml:"not_expected_claims"`
	ExpectedCount     *int            `yaml:"expected_count"`
	ExpectedMinCount  *int            `yaml:"expected_min_count"`
}

type ExpectedClaim struct {
	Text          string  `yaml:"text"`
	Type          string  `yaml:"type"`
	Status        string  `yaml:"status"`
	MinConfidence float64 `yaml:"min_confidence"`
	MaxConfidence float64 `yaml:"max_confidence"`
}

type TestFile struct {
	TestCases []TestCase `yaml:"test_cases"`
}

func loadTestFiles(t *testing.T) []TestFile {
	evalDir, err := os.Getwd()
	if err != nil {
		t.Skipf("eval directory not found: %v", err)
	}

	files, err := os.ReadDir(evalDir)
	if err != nil {
		t.Skipf("eval directory not found: %v", err)
	}

	var testFiles []TestFile
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") || f.Name() == "schema.yaml" {
			continue
		}
		path := filepath.Join(evalDir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("Failed to read %s: %v", path, err)
			continue
		}

		var tf TestFile
		if err := yaml.Unmarshal(data, &tf); err != nil {
			t.Logf("Failed to parse %s: %v", path, err)
			continue
		}
		testFiles = append(testFiles, tf)
	}
	return testFiles
}

func toClaimType(s string) domain.ClaimType {
	switch s {
	case "decision":
		return domain.ClaimTypeDecision
	case "hypothesis":
		return domain.ClaimTypeHypothesis
	default:
		return domain.ClaimTypeFact
	}
}

func toClaimStatus(s string) domain.ClaimStatus {
	if s == "contested" {
		return domain.ClaimStatusContested
	}
	return domain.ClaimStatusActive
}

func testCaseToEvent(tc TestCase) domain.Event {
	return domain.Event{
		ID:            "test-event-" + tc.ID,
		SchemaVersion: "1.0",
		Content:       tc.Input,
		Timestamp:     time.Now(),
	}
}

func runTestCase(t *testing.T, tc TestCase, engine extract.Engine) {
	event := testCaseToEvent(tc)
	claims, _, err := engine.Extract([]domain.Event{event})
	if err != nil {
		t.Errorf("%s: extraction failed: %v", tc.ID, err)
		return
	}

	if tc.ExpectedCount != nil && len(claims) != *tc.ExpectedCount {
		t.Errorf("%s: expected %d claims, got %d", tc.ID, *tc.ExpectedCount, len(claims))
	}

	if tc.ExpectedMinCount != nil && len(claims) < *tc.ExpectedMinCount {
		t.Errorf("%s: expected at least %d claims, got %d", tc.ID, *tc.ExpectedMinCount, len(claims))
	}

	for _, expected := range tc.ExpectedClaims {
		found := false
		isRealWorld := containsTag(tc.Tags, "real_world")
		for _, claim := range claims {
			normalizedClaim := normalizeClaimText(claim.Text)
			normalizedExpected := normalizeClaimText(expected.Text)

			// Exact match for unit tests, substring match for real-world tests
			match := normalizedClaim == normalizedExpected
			if isRealWorld && strings.Contains(normalizedClaim, normalizedExpected) {
				match = true
			}

			if match {
				found = true

				if claim.Type != toClaimType(expected.Type) {
					t.Errorf("%s: claim '%s' expected type '%s', got '%s'",
						tc.ID, expected.Text, expected.Type, claim.Type)
				}

				if expected.Status != "" && claim.Status != toClaimStatus(expected.Status) {
					t.Errorf("%s: claim '%s' expected status '%s', got '%s'",
						tc.ID, expected.Text, expected.Status, claim.Status)
				}

				if expected.MinConfidence > 0 && claim.Confidence < expected.MinConfidence {
					t.Errorf("%s: claim '%s' confidence %.2f below minimum %.2f",
						tc.ID, expected.Text, claim.Confidence, expected.MinConfidence)
				}

				if expected.MaxConfidence > 0 && claim.Confidence > expected.MaxConfidence {
					t.Errorf("%s: claim '%s' confidence %.2f above maximum %.2f",
						tc.ID, expected.Text, claim.Confidence, expected.MaxConfidence)
				}
				break
			}
		}
		if !found {
			t.Logf("%s: DEBUG actual claims: %v", tc.ID, claims)
			t.Errorf("%s: expected claim not found: '%s'", tc.ID, expected.Text)
		}
	}
}

func normalizeClaimText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "\"")
	text = strings.TrimSuffix(text, "\"")
	text = strings.TrimSuffix(text, ".")
	text = strings.TrimSuffix(text, "!")
	text = strings.TrimSuffix(text, "?")
	return strings.ToLower(text)
}

func TestClaimTypes(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "claim_types") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
			})
		}
	}
}

func TestDeduplication(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "deduplication") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
			})
		}
	}
}

func TestContestedDetection(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "contested_detection") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
			})
		}
	}
}

func TestConfidenceScoring(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "confidence_scoring") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
			})
		}
	}
}

func TestEdgeCases(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "edge_cases") && !containsTag(tc.Tags, "boundaries") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
			})
		}
	}
}

func TestRealWorld(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "real_world") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
			})
		}
	}
}

func TestAllCases(t *testing.T) {
	testFiles := loadTestFiles(t)
	engine := extract.NewEngine()
	passed := 0
	failed := 0

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			// Skip LLM-specific cases in the rule-based suite.
			if containsTag(tc.Tags, "llm") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runTestCase(t, tc, engine)
				if t.Failed() {
					failed++
				} else {
					passed++
				}
			})
		}
	}

	t.Logf("\n=== EVALUATION SUMMARY ===")
	t.Logf("Passed: %d", passed)
	t.Logf("Failed: %d", failed)
	t.Logf("Total:  %d", passed+failed)
	if passed+failed > 0 {
		t.Logf("Pass Rate: %.1f%%", float64(passed)/float64(passed+failed)*100)
	}
}

// TestLLMExtraction runs LLM-specific eval cases against the LLM-powered engine.
// Skipped unless MNEMOS_LLM_PROVIDER is set (requires real API keys).
func TestLLMExtraction(t *testing.T) {
	provider := os.Getenv("MNEMOS_LLM_PROVIDER")
	if provider == "" {
		t.Skip("MNEMOS_LLM_PROVIDER not set; skipping LLM eval")
	}

	cfg, err := llm.ConfigFromEnv()
	if err != nil {
		t.Skipf("LLM config error: %v", err)
	}
	client, err := llm.NewClient(cfg)
	if err != nil {
		t.Skipf("LLM client error: %v", err)
	}

	engine := extract.NewLLMEngine(client)
	testFiles := loadTestFiles(t)
	passed := 0
	failed := 0

	for _, tf := range testFiles {
		for _, tc := range tf.TestCases {
			if !containsTag(tc.Tags, "llm") {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				runLLMTestCase(t, tc, engine)
				if t.Failed() {
					failed++
				} else {
					passed++
				}
			})
		}
	}

	t.Logf("\n=== LLM EVAL SUMMARY ===")
	t.Logf("Provider: %s / %s", cfg.Provider, cfg.Model)
	t.Logf("Passed: %d", passed)
	t.Logf("Failed: %d", failed)
	t.Logf("Total:  %d", passed+failed)
	if passed+failed > 0 {
		t.Logf("Pass Rate: %.1f%%", float64(passed)/float64(passed+failed)*100)
	}
}

// runLLMTestCase validates LLM extraction with looser matching (substring)
// since LLMs rephrase claims rather than extracting verbatim text.
func runLLMTestCase(t *testing.T, tc TestCase, engine extract.LLMEngine) {
	event := testCaseToEvent(tc)
	claims, _, err := engine.Extract([]domain.Event{event})
	if err != nil {
		t.Errorf("%s: extraction failed: %v", tc.ID, err)
		return
	}

	if tc.ExpectedCount != nil && len(claims) != *tc.ExpectedCount {
		t.Errorf("%s: expected %d claims, got %d", tc.ID, *tc.ExpectedCount, len(claims))
	}

	if tc.ExpectedMinCount != nil && len(claims) < *tc.ExpectedMinCount {
		t.Errorf("%s: expected at least %d claims, got %d", tc.ID, *tc.ExpectedMinCount, len(claims))
	}

	for _, expected := range tc.ExpectedClaims {
		found := false
		for _, claim := range claims {
			normalizedClaim := normalizeClaimText(claim.Text)
			normalizedExpected := normalizeClaimText(expected.Text)

			// LLM tests use substring matching since LLMs rephrase.
			if strings.Contains(normalizedClaim, normalizedExpected) || strings.Contains(normalizedExpected, normalizedClaim) {
				found = true

				if expected.Type != "" && claim.Type != toClaimType(expected.Type) {
					t.Logf("%s: claim '%s' type mismatch: want '%s', got '%s'",
						tc.ID, expected.Text, expected.Type, claim.Type)
				}

				if expected.MinConfidence > 0 && claim.Confidence < expected.MinConfidence {
					t.Logf("%s: claim '%s' confidence %.2f below min %.2f",
						tc.ID, expected.Text, claim.Confidence, expected.MinConfidence)
				}
				break
			}
		}
		if !found {
			t.Logf("%s: expected claim not found (LLM may have rephrased): '%s'", tc.ID, expected.Text)
			t.Logf("%s: actual claims:", tc.ID)
			for _, c := range claims {
				t.Logf("  - [%s] %s (%.2f)", c.Type, c.Text, c.Confidence)
			}
			t.Errorf("%s: expected claim not found: '%s'", tc.ID, expected.Text)
		}
	}

	for _, notExpected := range tc.NotExpectedClaims {
		normalizedNot := normalizeClaimText(notExpected)
		for _, claim := range claims {
			if strings.Contains(normalizeClaimText(claim.Text), normalizedNot) {
				t.Errorf("%s: unexpected claim found: '%s' matches '%s'", tc.ID, claim.Text, notExpected)
			}
		}
	}
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

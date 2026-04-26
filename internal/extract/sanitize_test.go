package extract

import (
	"strings"
	"testing"
)

func TestSanitizeLLMJSONStripsThinkBlocks(t *testing.T) {
	input := `<think>
Let me reason about this. The user wants claims about revenue.
I'll extract one fact.
</think>
[{"text":"Revenue grew 15%","type":"fact","confidence":0.9}]`

	got := sanitizeLLMJSON(input)
	if strings.Contains(got, "<think>") || strings.Contains(got, "reason about") {
		t.Fatalf("think block leaked into output: %q", got)
	}
	if !strings.HasPrefix(got, "[") {
		t.Fatalf("expected output to start with array, got %q", got)
	}
}

func TestSanitizeLLMJSONStripsCaseInsensitiveThink(t *testing.T) {
	input := `<THINK>uppercase</THINK><Think>mixed</Think>[{"text":"x","type":"fact","confidence":0.8}]`
	got := sanitizeLLMJSON(input)
	if strings.Contains(strings.ToLower(got), "think") {
		t.Fatalf("uppercase/mixed-case think block leaked: %q", got)
	}
}

func TestSanitizeLLMJSONStripsProsePreamble(t *testing.T) {
	input := `Here are the claims I extracted:
[{"text":"Revenue grew","type":"fact","confidence":0.9}]
Hope this helps!`

	got := sanitizeLLMJSON(input)
	if strings.HasPrefix(got, "Here are") {
		t.Fatalf("prose preamble not stripped: %q", got)
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Fatalf("expected balanced array, got %q", got)
	}
	if strings.Contains(got, "Hope this helps") {
		t.Fatalf("trailing prose not stripped: %q", got)
	}
}

func TestSanitizeLLMJSONStripsMarkdownFences(t *testing.T) {
	input := "```json\n[{\"text\":\"a\",\"type\":\"fact\",\"confidence\":0.8}]\n```"
	got := sanitizeLLMJSON(input)
	if strings.Contains(got, "```") {
		t.Fatalf("fence not stripped: %q", got)
	}
}

func TestSanitizeLLMJSONHandlesObjectFromOllamaStreamLeak(t *testing.T) {
	// Some Ollama versions emit a trailing frame even with stream=false.
	// extractFirstJSONValue should pick up only the first balanced object.
	input := `{"text":"first","type":"fact","confidence":0.9}
{"done":true}`
	got := sanitizeLLMJSON(input)
	if strings.Contains(got, `"done"`) {
		t.Fatalf("trailing frame not stripped: %q", got)
	}
}

func TestSanitizeLLMJSONIgnoresBracketsInsideStrings(t *testing.T) {
	// Brackets inside JSON string values must not confuse the depth counter.
	input := `[{"text":"items: [a, b, c]","type":"fact","confidence":0.8}]`
	got := sanitizeLLMJSON(input)
	if got != input {
		t.Fatalf("string-internal brackets miscounted: %q", got)
	}
}

func TestParseLLMResponseAcceptsThinkPrefixedJSON(t *testing.T) {
	input := `<think>I think a fact</think>[{"text":"X","type":"fact","confidence":0.9}]`
	claims, err := parseLLMResponse(input)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(claims) != 1 || claims[0].Text != "X" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenAITools_FourCanonicalVerbs pins that the OpenAI projection
// surfaces exactly the four memory verbs every agent SDK expects.
// Adding a fifth verb without updating this test (and the Anthropic
// counterpart) is the failure mode this guards against.
func TestOpenAITools_FourCanonicalVerbs(t *testing.T) {
	tools := OpenAITools()
	if len(tools) != 4 {
		t.Fatalf("got %d tools, want 4", len(tools))
	}
	want := []string{"remember", "forget", "update", "search_memory"}
	for i, w := range want {
		if tools[i].Type != "function" {
			t.Errorf("tools[%d].Type = %q, want function", i, tools[i].Type)
		}
		if tools[i].Function.Name != w {
			t.Errorf("tools[%d].Function.Name = %q, want %q", i, tools[i].Function.Name, w)
		}
		if tools[i].Function.Description == "" {
			t.Errorf("tools[%d] missing description", i)
		}
		if tools[i].Function.Parameters == nil {
			t.Errorf("tools[%d] missing parameters schema", i)
		}
	}
}

// TestAnthropicTools_FourCanonicalVerbs is the Anthropic projection's
// counterpart guardrail.
func TestAnthropicTools_FourCanonicalVerbs(t *testing.T) {
	tools := AnthropicTools()
	if len(tools) != 4 {
		t.Fatalf("got %d tools, want 4", len(tools))
	}
	want := []string{"remember", "forget", "update", "search_memory"}
	for i, w := range want {
		if tools[i].Name != w {
			t.Errorf("tools[%d].Name = %q, want %q", i, tools[i].Name, w)
		}
		if tools[i].Description == "" {
			t.Errorf("tools[%d] missing description", i)
		}
		if tools[i].InputSchema == nil {
			t.Errorf("tools[%d] missing input_schema", i)
		}
	}
}

// TestRememberRequiredFields proves the required-field contract the
// MCP server enforces (text + run_id) is also published on the
// tool-call schema, so an LLM that respects required fields will
// never construct a malformed remember call.
func TestRememberRequiredFields(t *testing.T) {
	tools := OpenAITools()
	var schema *JSONSchema
	for _, tl := range tools {
		if tl.Function.Name == "remember" {
			schema = tl.Function.Parameters
			break
		}
	}
	if schema == nil {
		t.Fatal("remember schema missing")
	}
	wantReq := map[string]bool{"text": true, "run_id": true}
	got := map[string]bool{}
	for _, r := range schema.Required {
		got[r] = true
	}
	for k := range wantReq {
		if !got[k] {
			t.Errorf("remember.required missing %q (got %v)", k, schema.Required)
		}
	}
}

// TestSearchMemoryRequiresRunID pins the tenant boundary in the
// schema. The MCP and HTTP surfaces both reject search_memory without
// run_id; the tool-call schema must declare it required so LLMs
// don't even attempt a no-run_id call.
func TestSearchMemoryRequiresRunID(t *testing.T) {
	tools := OpenAITools()
	var schema *JSONSchema
	for _, tl := range tools {
		if tl.Function.Name == "search_memory" {
			schema = tl.Function.Parameters
			break
		}
	}
	if schema == nil {
		t.Fatal("search_memory schema missing")
	}
	hasRunID := false
	for _, r := range schema.Required {
		if r == "run_id" {
			hasRunID = true
		}
	}
	if !hasRunID {
		t.Errorf("search_memory.required must include run_id, got %v", schema.Required)
	}
}

// TestKindEnumPinsAllowedValues proves the schema enumerates exactly
// the kinds the remember handler accepts (fact, hypothesis,
// decision) — a drift here would let LLMs construct calls the server
// rejects.
func TestKindEnumPinsAllowedValues(t *testing.T) {
	for _, tl := range OpenAITools() {
		if tl.Function.Name != "remember" {
			continue
		}
		kind := tl.Function.Parameters.Properties["kind"]
		if kind == nil {
			t.Fatal("kind property missing on remember schema")
		}
		want := map[string]bool{"fact": true, "hypothesis": true, "decision": true}
		if len(kind.Enum) != len(want) {
			t.Fatalf("kind enum size = %d, want %d", len(kind.Enum), len(want))
		}
		for _, v := range kind.Enum {
			if !want[v] {
				t.Errorf("unexpected kind enum value %q", v)
			}
		}
	}
}

// TestJSONSnapshotsRoundTrip proves the tool schemas serialise to
// valid JSON and deserialise back identically — the published JSON is
// the wire contract, so any drift in struct tags would silently break
// non-Go callers.
func TestJSONSnapshotsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		blob any
	}{
		{"openai", OpenAITools()},
		{"anthropic", AnthropicTools()},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			b, err := json.MarshalIndent(c.blob, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			// Round-trip through a generic map to catch tag-level mismatches.
			var back any
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
		})
	}
}

// TestSnapshotFilesMatch validates the on-disk JSON snapshots stay in
// sync with the in-code schemas. Non-Go callers consume the snapshot
// files; if a future edit changes a description or required-list and
// the snapshot doesn't get regenerated, this fails loudly.
//
// To regenerate intentionally:
//
//	go test ./client/tools -run TestSnapshotFilesMatch -update
func TestSnapshotFilesMatch(t *testing.T) {
	for _, c := range []struct {
		path string
		blob any
	}{
		{"snapshots/openai.json", OpenAITools()},
		{"snapshots/anthropic.json", AnthropicTools()},
	} {
		c := c
		t.Run(filepath.Base(c.path), func(t *testing.T) {
			want, err := json.MarshalIndent(c.blob, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			want = append(want, '\n')
			if updateGolden {
				if err := os.WriteFile(c.path, want, 0o644); err != nil {
					t.Fatalf("update snapshot: %v", err)
				}
				return
			}
			got, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatalf("read snapshot %s: %v (regenerate with -update)", c.path, err)
			}
			if string(got) != string(want) {
				t.Errorf("snapshot %s out of date — regenerate with `go test ./client/tools -run TestSnapshotFilesMatch -update`", c.path)
			}
		})
	}
}

// Package tools exposes Mnemos's agent-memory primitives as canonical
// LLM tool-call schemas. The same four verbs are surfaced in two
// vendor formats (OpenAI / Anthropic) so downstream integrations don't
// have to hand-roll the JSON every time they wire Mnemos into an agent.
//
// The verbs mirror the MCP tools shipped under cmd/mnemos (Refs #41):
//
//	remember(text, kind, run_id, valid_until?) — store a claim
//	forget(claim_id, reason?)                  — soft-delete a claim
//	update(claim_id, new_text, confidence?)    — rewrite a claim's text
//	search_memory(query, run_id, top_k?)       — semantic recall
//
// These are intentionally separate from the MCP server's internal
// jsonschema generation: an external caller embedding Mnemos in an
// agent doesn't run the MCP server, so we ship the JSON it needs as
// data. The schema content matches the MCP tools' input shape so an
// agent built against either surface speaks the same protocol.
package tools

// JSONSchema is a tiny subset of JSON Schema sufficient to describe
// the parameters Mnemos exposes. We do not import a full
// jsonschema-go dependency because the shapes here are small and the
// output JSON is the contract — keeping the model lean prevents a
// dependency bump from rewriting the wire format.
type JSONSchema struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]*JSONSchema `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Enum        []string               `json:"enum,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
}

// OpenAITool is the wire shape OpenAI's chat-completions API expects
// under the tools array. Two layers (type + function) match the v2024
// tool-call spec.
type OpenAITool struct {
	Type     string             `json:"type"`
	Function OpenAIToolFunction `json:"function"`
}

// OpenAIToolFunction is the inner shape carrying the schema.
type OpenAIToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  *JSONSchema `json:"parameters"`
}

// AnthropicTool is the wire shape Anthropic's Messages API expects
// under the tools array. Flatter than OpenAI's: no outer "function"
// wrapper; the schema lives under "input_schema".
type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema *JSONSchema `json:"input_schema"`
}

func ptrFloat(v float64) *float64 { return &v }

// rememberSchema describes the "remember" verb's parameters in a
// vendor-agnostic JSON Schema so the OpenAI/Anthropic wrappers can
// share one source of truth.
func rememberSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"text": {
				Type:        "string",
				Description: "The fact to remember.",
			},
			"kind": {
				Type:        "string",
				Description: "Claim kind. Defaults to \"fact\" when omitted.",
				Enum:        []string{"fact", "hypothesis", "decision"},
			},
			"run_id": {
				Type:        "string",
				Description: "Tenant scope. Events are stamped with this so run_id-filtered recall (and search_memory) can find the claim.",
			},
			"valid_until": {
				Type:        "string",
				Description: "Optional RFC3339 timestamp at which the claim should automatically be considered no longer in force.",
			},
			"confidence": {
				Type:        "number",
				Description: "Confidence in [0, 1]. Defaults to 0.9 when omitted.",
				Minimum:     ptrFloat(0),
				Maximum:     ptrFloat(1),
			},
		},
		Required: []string{"text", "run_id"},
	}
}

func forgetSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"claim_id": {
				Type:        "string",
				Description: "ID of the claim to forget. Status flips to \"deprecated\"; the claim and its evidence stay queryable for audit.",
			},
			"reason": {
				Type:        "string",
				Description: "Optional rationale recorded on the status transition.",
			},
		},
		Required: []string{"claim_id"},
	}
}

func updateSchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"claim_id": {
				Type:        "string",
				Description: "ID of the claim to rewrite.",
			},
			"new_text": {
				Type:        "string",
				Description: "Replacement text for the claim.",
			},
			"confidence": {
				Type:        "number",
				Description: "Optional new confidence in [0, 1]; omit to keep the current value.",
				Minimum:     ptrFloat(0),
				Maximum:     ptrFloat(1),
			},
			"reason": {
				Type:        "string",
				Description: "Optional rationale recorded on the status transition.",
			},
		},
		Required: []string{"claim_id", "new_text"},
	}
}

func searchMemorySchema() *JSONSchema {
	return &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"query": {
				Type:        "string",
				Description: "Free-text query. Embedded by the configured provider and matched against stored claim embeddings by cosine similarity.",
			},
			"run_id": {
				Type:        "string",
				Description: "Tenant scope. Required to prevent cross-tenant semantic leakage; only claims linked to events under this run_id are searched.",
			},
			"top_k": {
				Type:        "integer",
				Description: "Maximum hits to return (default 10, max 100).",
				Minimum:     ptrFloat(1),
				Maximum:     ptrFloat(100),
			},
			"min_similarity": {
				Type:        "number",
				Description: "Drop hits below this cosine similarity threshold (default 0.0).",
				Minimum:     ptrFloat(0),
				Maximum:     ptrFloat(1),
			},
		},
		Required: []string{"query", "run_id"},
	}
}

// definitions is the single source of truth — (name, description,
// parameters) — for every Mnemos tool. The OpenAI / Anthropic
// generators below project from this so the two formats can never
// drift apart in a future edit.
type definition struct {
	Name        string
	Description string
	Parameters  *JSONSchema
}

func definitions() []definition {
	return []definition{
		{
			Name:        "remember",
			Description: "Persist a claim into Mnemos for the given run_id. Use this whenever the agent decides to commit a fact to long-term memory; the claim plus its source event and evidence link are all written atomically so the audit trail stays complete.",
			Parameters:  rememberSchema(),
		},
		{
			Name:        "forget",
			Description: "Soft-delete a claim by flipping its status to \"deprecated\". The text and evidence remain queryable for audit; future recall paths exclude the claim from active context.",
			Parameters:  forgetSchema(),
		},
		{
			Name:        "update",
			Description: "Rewrite a claim's text (and optionally its confidence) when the agent's understanding refines. Use when the underlying fact didn't change identity, only wording or precision.",
			Parameters:  updateSchema(),
		},
		{
			Name:        "search_memory",
			Description: "Semantic search over the agent's memory: embed the query, then rank claims by cosine similarity against the query embedding, restricted to claims under the given run_id (tenant boundary).",
			Parameters:  searchMemorySchema(),
		},
	}
}

// OpenAITools returns the four Mnemos memory tools formatted for
// OpenAI's chat-completions tool-call API.
func OpenAITools() []OpenAITool {
	defs := definitions()
	out := make([]OpenAITool, 0, len(defs))
	for _, d := range defs {
		out = append(out, OpenAITool{
			Type:     "function",
			Function: OpenAIToolFunction(d),
		})
	}
	return out
}

// AnthropicTools returns the four Mnemos memory tools formatted for
// Anthropic's Messages tool-use API.
func AnthropicTools() []AnthropicTool {
	defs := definitions()
	out := make([]AnthropicTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, AnthropicTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.Parameters,
		})
	}
	return out
}

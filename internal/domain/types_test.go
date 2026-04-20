package domain

import (
	"testing"
	"time"
)

func TestClaimValidate(t *testing.T) {
	tests := []struct {
		name      string
		claim     Claim
		wantError bool
	}{
		{
			name: "valid claim",
			claim: Claim{
				ID:         "c-1",
				Text:       "Revenue dropped after feature launch",
				Type:       ClaimTypeFact,
				Status:     ClaimStatusActive,
				Confidence: 0.9,
			},
		},
		{
			name: "missing id",
			claim: Claim{
				Text:       "x",
				Type:       ClaimTypeFact,
				Status:     ClaimStatusActive,
				Confidence: 0.7,
			},
			wantError: true,
		},
		{
			name: "confidence out of range",
			claim: Claim{
				ID:         "c-2",
				Text:       "x",
				Type:       ClaimTypeFact,
				Status:     ClaimStatusActive,
				Confidence: 1.1,
			},
			wantError: true,
		},
		{
			name: "invalid type",
			claim: Claim{
				ID:         "c-3",
				Text:       "x",
				Type:       "unknown",
				Status:     ClaimStatusActive,
				Confidence: 0.5,
			},
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.claim.Validate()
			if (err != nil) != tc.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestClaimEvidenceValidate(t *testing.T) {
	valid := ClaimEvidence{ClaimID: "c-1", EventID: "e-1"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	missingEvent := ClaimEvidence{ClaimID: "c-1"}
	if err := missingEvent.Validate(); err == nil {
		t.Fatal("Validate() expected error for missing event_id")
	}
}

func TestEventValidate(t *testing.T) {
	base := Event{
		ID:            "ev_1",
		SourceInputID: "in_1",
		Content:       "a meaningful sentence",
		Timestamp:     time.Unix(1, 0),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}

	cases := []struct {
		name  string
		mut   func(e *Event)
		wantB bool
	}{
		{"missing id", func(e *Event) { e.ID = "" }, true},
		{"empty content", func(e *Event) { e.Content = "" }, true},
		{"whitespace content", func(e *Event) { e.Content = "   " }, true},
		{"missing source_input_id", func(e *Event) { e.SourceInputID = "" }, true},
		{"zero timestamp", func(e *Event) { e.Timestamp = time.Time{} }, true},
		{"oversize content", func(e *Event) {
			e.Content = make_string('x', MaxEventContentBytes+1)
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.mut(&e)
			err := e.Validate()
			if tc.wantB && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantB && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// make_string is a tiny helper to build a string of n copies of c
// without pulling in strings.Repeat (which we use elsewhere).
func make_string(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestRelationshipValidate(t *testing.T) {
	base := Relationship{
		ID:          "r_1",
		Type:        RelationshipTypeSupports,
		FromClaimID: "cl_a",
		ToClaimID:   "cl_b",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid relationship rejected: %v", err)
	}

	cases := []struct {
		name string
		mut  func(r *Relationship)
	}{
		{"missing id", func(r *Relationship) { r.ID = "" }},
		{"missing from", func(r *Relationship) { r.FromClaimID = "" }},
		{"missing to", func(r *Relationship) { r.ToClaimID = "" }},
		{"self reference", func(r *Relationship) { r.ToClaimID = r.FromClaimID }},
		{"bad type", func(r *Relationship) { r.Type = "undermines" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mut(&r)
			if err := r.Validate(); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

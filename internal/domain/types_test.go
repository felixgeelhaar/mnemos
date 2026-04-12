package domain

import "testing"

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

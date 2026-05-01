package domain

import (
	"strings"
	"testing"
	"time"
)

func TestActionValidate(t *testing.T) {
	now := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		action  Action
		wantErr string
	}{
		{
			name: "valid",
			action: Action{
				ID:      "ac_1",
				Kind:    ActionKindDeploy,
				Subject: "payments",
				At:      now,
			},
		},
		{
			name:    "missing id",
			action:  Action{Kind: ActionKindDeploy, Subject: "payments", At: now},
			wantErr: "action id is required",
		},
		{
			name:    "missing kind",
			action:  Action{ID: "ac_1", Subject: "payments", At: now},
			wantErr: "action kind is required",
		},
		{
			name:    "missing subject",
			action:  Action{ID: "ac_1", Kind: ActionKindDeploy, At: now},
			wantErr: "action subject is required",
		},
		{
			name:    "missing at",
			action:  Action{ID: "ac_1", Kind: ActionKindDeploy, Subject: "payments"},
			wantErr: "action at is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.action.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestOutcomeValidate(t *testing.T) {
	now := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		outcome Outcome
		wantErr string
	}{
		{
			name: "valid success",
			outcome: Outcome{
				ID:         "oc_1",
				ActionID:   "ac_1",
				Result:     OutcomeResultSuccess,
				ObservedAt: now,
			},
		},
		{
			name:    "missing id",
			outcome: Outcome{ActionID: "ac_1", Result: OutcomeResultSuccess, ObservedAt: now},
			wantErr: "outcome id is required",
		},
		{
			name:    "missing action id",
			outcome: Outcome{ID: "oc_1", Result: OutcomeResultSuccess, ObservedAt: now},
			wantErr: "outcome action_id is required",
		},
		{
			name:    "invalid result",
			outcome: Outcome{ID: "oc_1", ActionID: "ac_1", Result: "weird", ObservedAt: now},
			wantErr: "invalid outcome result",
		},
		{
			name:    "missing observed_at",
			outcome: Outcome{ID: "oc_1", ActionID: "ac_1", Result: OutcomeResultSuccess},
			wantErr: "outcome observed_at is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.outcome.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

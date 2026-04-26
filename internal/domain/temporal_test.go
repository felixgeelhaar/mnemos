package domain

import (
	"testing"
	"time"
)

func TestClaim_IsValidAt(t *testing.T) {
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	may := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	july := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		c    Claim
		at   time.Time
		want bool
	}{
		{"open ended valid since march, asked at july", Claim{ValidFrom: march}, july, true},
		{"open ended valid since may, asked before may", Claim{ValidFrom: may}, march, false},
		{"closed interval, asked inside", Claim{ValidFrom: march, ValidTo: july}, may, true},
		{"closed interval, asked at upper bound", Claim{ValidFrom: march, ValidTo: july}, july, false},
		{"closed interval, asked after upper bound", Claim{ValidFrom: march, ValidTo: may}, july, false},
		{"zero valid_from treated as -inf", Claim{ValidTo: july}, march, true},
		{"zero valid_from + zero valid_to", Claim{}, may, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsValidAt(tc.at); got != tc.want {
				t.Fatalf("IsValidAt(%s) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestClaim_IsSuperseded(t *testing.T) {
	c := Claim{}
	if c.IsSuperseded() {
		t.Fatal("zero ValidTo should not be superseded")
	}
	c.ValidTo = time.Now()
	if !c.IsSuperseded() {
		t.Fatal("non-zero ValidTo should be superseded")
	}
}

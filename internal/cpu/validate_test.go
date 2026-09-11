package cpu

import (
	"math"
	"testing"
)

func TestQuotaValidation(t *testing.T) {
	for _, value := range []float64{0, -1, 0.004, 0.125, math.NaN(), math.Inf(1)} {
		if ValidateQuota(value) == nil {
			t.Errorf("accepted unrepresentable quota %v", value)
		}
	}
	for _, value := range []float64{0.01, 0.25, 0.5, 1, 2} {
		if err := ValidateQuota(value); err != nil {
			t.Errorf("quota %v: %v", value, err)
		}
	}
}

func TestCPUNumbersAreNotInterpretedAsCPUCounts(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		quota       float64
		wantErr     bool
	}{
		{"0", "0-0", 0.5, false}, {"1", "1-1", 1, false}, {"0-1", "0-0,1-1", 2, false},
		{"3", "", 0.5, true}, {"1-0", "", 0.5, true}, {"1", "", 2, true}, {"", "", 1, false},
	} {
		got, err := CanonicalPin(tc.input, []int{0, 1}, tc.quota)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("pin %q: got %q, %v", tc.input, got, err)
		}
	}
}

package cpu

import "testing"

func TestAllowance(t *testing.T) {
	cases := map[float64]string{
		0.5: "50ms/100ms",
		1:   "100ms/100ms",
		2:   "200ms/100ms",
		0.25: "25ms/100ms",
	}
	for cores, want := range cases {
		if got := Allowance(cores); got != want {
			t.Fatalf("Allowance(%v)=%q want %q", cores, got, want)
		}
	}
}

func TestCPUMax(t *testing.T) {
	if got := CPUMax(1.5); got != "150000 100000" {
		t.Fatalf("CPUMax(1.5)=%q", got)
	}
	if got := CPUMax(2); got != "200000 100000" {
		t.Fatalf("CPUMax(2)=%q", got)
	}
}

func TestDefaultAggregateCap(t *testing.T) {
	if got := DefaultAggregateCap(2); got != 1.5 {
		t.Fatalf("got %v", got)
	}
	if got := DefaultAggregateCap(4); got != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestOvercommitRatio(t *testing.T) {
	if got := OvercommitRatio(8, 2); got != 4 {
		t.Fatalf("got %v", got)
	}
}

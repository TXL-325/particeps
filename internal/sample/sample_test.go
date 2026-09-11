package sample

import "testing"

func TestDeltaFirstAndReset(t *testing.T) {
	a := Point{CPUNs: 100, Rx: 10, Tx: 10, OK: true}
	b := Point{CPUNs: 100 + 5e8, Rx: 110, Tx: 210, OK: true}
	r := Delta(Point{}, b, 1, false)
	if r.Quality != "first" {
		t.Fatalf("quality %s", r.Quality)
	}
	r = Delta(a, b, 1, true)
	if r.Quality != "ok" || r.Cores < 0.4 || r.Cores > 0.6 {
		t.Fatalf("cores %v quality %s", r.Cores, r.Quality)
	}
	wrap := Point{CPUNs: 1, Rx: 1, Tx: 1, OK: true}
	r = Delta(b, wrap, 1, true)
	if r.Quality != "reset" {
		t.Fatalf("want reset got %s", r.Quality)
	}
}

func TestQuotaPercent(t *testing.T) {
	if p := QuotaPercent(0.25, 0.5); p < 49 || p > 51 {
		t.Fatalf("%v", p)
	}
}

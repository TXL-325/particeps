package hostmetrics

import (
	"sync"
	"testing"
	"time"
)

func TestCPUAccountingExcludesDuplicateGuestTime(t *testing.T) {
	total, idle, ok := parseCPULine("cpu 100 20 30 400 50 6 7 8 90 10")
	if !ok || total != 621 || idle != 450 {
		t.Fatalf("CPU accounting: total=%d idle=%d ok=%v", total, idle, ok)
	}
}

func TestCPUContinuesWhenNetworkIsMissingOrReset(t *testing.T) {
	s := &Sampler{}
	at := time.Unix(100, 0)
	first := Snapshot{LogicalCPUs: 2}
	s.updateRates(&first, at, 100, 50, true, 0, 0, false)
	if first.CPUQuality != "first" || first.NetworkQuality != "missing" {
		t.Fatalf("first sample: %+v", first)
	}
	for i, network := range []struct {
		rx, tx  uint64
		ok      bool
		quality string
	}{
		{0, 0, false, "missing"}, {100, 200, true, "first"}, {0, 0, true, "reset"}, {100, 200, true, "ok"},
	} {
		out := Snapshot{LogicalCPUs: 2}
		s.updateRates(&out, at.Add(time.Duration(i+1)*time.Second), uint64(200+i*100), uint64(100+i*50), true, network.rx, network.tx, network.ok)
		if out.CPUQuality != "ok" || out.CPUUsedCores != 1 || out.NetworkQuality != network.quality {
			t.Fatalf("CPU depended on network validity: %+v", out)
		}
	}
}

func TestLatestDoesNotAdvanceSamplingBaseline(t *testing.T) {
	s := NewSampler(DefaultUplink())
	before := s.prevAt
	var readers sync.WaitGroup
	for i := 0; i < 20; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 50; j++ {
				_ = s.Latest()
			}
		}()
	}
	readers.Wait()
	if !s.prevAt.Equal(before) {
		t.Fatal("HTTP snapshot reads changed the sampler baseline")
	}
}

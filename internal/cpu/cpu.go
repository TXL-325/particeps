package cpu

import (
	"fmt"
	"math"
)

const periodUS = 100_000

// Allowance returns Incus limits.cpu.allowance for a cores quota.
// 0.5 -> "50ms/100ms". Not a percentage soft limit.
func Allowance(cores float64) string {
	if cores <= 0 {
		return "1ms/100ms"
	}
	ms := int(math.Round(cores * 100))
	if ms < 1 {
		ms = 1
	}
	return fmt.Sprintf("%dms/100ms", ms)
}

// CPUMax returns cgroup v2 cpu.max quota/period for a cores cap.
// 1.5 -> "150000 100000".
func CPUMax(cores float64) string {
	if cores <= 0 {
		return "max 100000"
	}
	quota := int(math.Round(cores * periodUS))
	if quota < 1000 {
		quota = 1000
	}
	return fmt.Sprintf("%d %d", quota, periodUS)
}

// DefaultAggregateCap is 75% of host logical CPUs.
func DefaultAggregateCap(hostCPUs int) float64 {
	if hostCPUs < 1 {
		hostCPUs = 1
	}
	cap := float64(hostCPUs) * 0.75
	if cap < 0.25 {
		cap = 0.25
	}
	return math.Round(cap*100) / 100
}

func OvercommitRatio(configured, capacity float64) float64 {
	if capacity <= 0 {
		return 0
	}
	return configured / capacity
}

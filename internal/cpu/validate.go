package cpu

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Incus' fixed 100ms allowance is expressed in whole milliseconds. Reject
// quotas that cannot be represented instead of silently rounding the hard cap.
func ValidateQuota(cores float64) error {
	if math.IsNaN(cores) || math.IsInf(cores, 0) || cores < 0.01 || cores > 65536 || math.Abs(cores*100-math.Round(cores*100)) > 1e-7 {
		return fmt.Errorf("CPU quota must be positive and representable in 0.01-core steps")
	}
	return nil
}

func AvailableCPUs() []int {
	if data, err := os.ReadFile("/sys/devices/system/cpu/online"); err == nil {
		if ids, err := parseSet(strings.TrimSpace(string(data))); err == nil {
			return ids
		}
	}
	ids := make([]int, runtime.NumCPU())
	for i := range ids {
		ids[i] = i
	}
	return ids
}

func parseSet(input string) ([]int, error) {
	seen := map[int]bool{}
	for _, part := range strings.Split(input, ",") {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("invalid CPU set")
		}
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 0 || start > 65535 {
			return nil, fmt.Errorf("invalid CPU number")
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(bounds[1])
			if err != nil || end < start || end > 65535 {
				return nil, fmt.Errorf("invalid CPU range")
			}
		}
		for id := start; id <= end; id++ {
			seen[id] = true
		}
	}
	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}

// A bare integer in Incus limits.cpu is a CPU count. Even a single selected CPU
// therefore uses an explicit N-N range, so selecting CPU 1 cannot mean one CPU.
func CanonicalPin(input string, available []int, quota float64) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	ids, err := parseSet(input)
	if err != nil {
		return "", err
	}
	allowed := map[int]bool{}
	for _, id := range available {
		allowed[id] = true
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if !allowed[id] {
			return "", fmt.Errorf("CPU %d is not available", id)
		}
		parts = append(parts, fmt.Sprintf("%d-%d", id, id))
	}
	if quota > float64(len(ids)) {
		return "", fmt.Errorf("CPU quota exceeds selected CPU capacity")
	}
	return strings.Join(parts, ","), nil
}

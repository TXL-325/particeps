//go:build !linux

package hostmetrics

func readDisk(string) (uint64, uint64) { return 0, 0 }

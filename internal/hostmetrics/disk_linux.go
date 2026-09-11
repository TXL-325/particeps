//go:build linux

package hostmetrics

import "golang.org/x/sys/unix"

func readDisk(path string) (uint64, uint64) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize)
	return st.Blocks * bs, st.Bavail * bs
}

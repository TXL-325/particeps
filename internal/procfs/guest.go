package procfs

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Proc struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	User    string  `json:"user"`
	State   string  `json:"state"`
	Start   uint64  `json:"start"`
	CPU     float64 `json:"cpu"`
	RSS     uint64  `json:"rss"`
}

func GuestProcs(hostPID int) []Proc {
	if hostPID <= 1 {
		return nil
	}
	root := filepath.Join("/proc", strconv.Itoa(hostPID), "root")
	dir := filepath.Join(root, "proc")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Proc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, err := parseStat(filepath.Join(dir, e.Name(), "stat"))
		if err != nil {
			continue
		}
		st.User = lookupUID(filepath.Join(dir, e.Name(), "status"))
		st.RSS = rssKB(filepath.Join(dir, e.Name(), "status")) * 1024
		_ = pid
		out = append(out, st)
	}
	return out
}

func parseStat(path string) (Proc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Proc{}, err
	}
	s := string(b)
	lb := strings.IndexByte(s, '(')
	rb := strings.LastIndexByte(s, ')')
	if lb < 0 || rb < 0 {
		return Proc{}, os.ErrInvalid
	}
	p := Proc{Name: s[lb+1 : rb]}
	fields := strings.Fields(s[:lb] + s[rb+1:])
	if len(fields) < 21 {
		return Proc{}, os.ErrInvalid
	}
	p.PID, _ = strconv.Atoi(fields[0])
	p.State = fields[1]
	p.Start, _ = strconv.ParseUint(fields[20], 10, 64)
	utime, _ := strconv.ParseUint(fields[12], 10, 64)
	stime, _ := strconv.ParseUint(fields[13], 10, 64)
	p.CPU = float64(utime+stime) / 100.0
	return p, nil
}

func lookupUID(statusPath string) string {
	f, err := os.Open(statusPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "Uid:") {
			fs := strings.Fields(sc.Text())
			if len(fs) > 1 {
				return fs[1]
			}
		}
	}
	return ""
}

func rssKB(statusPath string) uint64 {
	f, err := os.Open(statusPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "VmRSS:") {
			fs := strings.Fields(sc.Text())
			if len(fs) > 1 {
				n, _ := strconv.ParseUint(fs[1], 10, 64)
				return n
			}
		}
	}
	return 0
}

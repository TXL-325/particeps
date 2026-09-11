package cgroupcap

import (
	"os"
	"path/filepath"
	"strings"

	"particeps/internal/cpu"
)

const dirName = "particeps-guests"

type Status struct {
	Path    string  `json:"path"`
	Applied bool    `json:"applied"`
	CPUMax  string  `json:"cpuMax"`
	Cores   float64 `json:"cores"`
	Note    string  `json:"note"`
}

func Apply(cores float64) Status {
	return applyAt("/sys/fs/cgroup", cores)
}

func applyAt(root string, cores float64) Status {
	st := Status{Cores: cores, CPUMax: cpu.CPUMax(cores)}
	if err := cpu.ValidateQuota(cores); err != nil {
		st.Note = err.Error()
		return st
	}
	parent := filepath.Join(root, dirName)
	st.Path = parent
	if _, err := os.Stat(root); err != nil {
		st.Note = "cgroup fs missing"
		return st
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		st.Note = "mkdir: " + err.Error()
		return st
	}
	if err := writeControl(filepath.Join(root, "cgroup.subtree_control"), "+cpu"); err != nil {
		st.Note = "root subtree_control: " + err.Error()
		return st
	}
	if err := writeControl(filepath.Join(parent, "cgroup.subtree_control"), "+cpu"); err != nil {
		st.Note = "subtree_control: " + err.Error()
		return st
	}
	if err := writeControl(filepath.Join(parent, "cpu.max"), st.CPUMax+"\n"); err != nil {
		st.Note = strings.TrimSpace(st.Note + " cpu.max: " + err.Error())
		return st
	}
	actual, err := os.ReadFile(filepath.Join(parent, "cpu.max"))
	if err != nil || strings.Join(strings.Fields(string(actual)), " ") != st.CPUMax {
		st.Note = "cpu.max readback failed"
		return st
	}
	st.Applied = true
	st.Note = "parent cpu.max applied"
	return st
}

func writeControl(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = file.WriteString(value)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func Dir() string { return filepath.Join("/sys/fs/cgroup", dirName) }

func LXCRaw() string {
	// Parent directory only. Do not set lxc.cgroup.dir.container to this
	// path: LXC would try to use the existing directory as the payload
	// cgroup and fail with EEXIST/ERANGE.
	return "lxc.cgroup.dir = " + dirName + "\n"
}

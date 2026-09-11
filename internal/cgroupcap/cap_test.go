package cgroupcap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRequiresControllersAndVerifiedQuota(t *testing.T) {
	for _, missing := range []string{"root-controller", "child-controller", "quota", "none"} {
		t.Run(missing, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, dirName)
			if err := os.MkdirAll(parent, 0755); err != nil {
				t.Fatal(err)
			}
			for name, path := range map[string]string{"root-controller": filepath.Join(root, "cgroup.subtree_control"), "child-controller": filepath.Join(parent, "cgroup.subtree_control"), "quota": filepath.Join(parent, "cpu.max")} {
				if name != missing {
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			status := applyAt(root, 1.5)
			if status.Applied != (missing == "none") {
				t.Fatalf("missing %s, applied %v (%s)", missing, status.Applied, status.Note)
			}
		})
	}
}

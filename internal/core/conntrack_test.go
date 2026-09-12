package core

import (
	"errors"
	"reflect"
	"testing"
)

type conntrackExit int

func (e conntrackExit) Error() string { return "command failed" }
func (e conntrackExit) ExitCode() int { return int(e) }

func TestConntrackCleanupRequiresPreciseFilterAndEmptyReadback(t *testing.T) {
	for _, tc := range []struct {
		name               string
		deleteErr, listErr error
		output             string
		wantError          bool
	}{
		{"deleted", nil, nil, "", false},
		{"already-empty", conntrackExit(1), nil, "", false},
		{"permission-denied", conntrackExit(1), conntrackExit(1), "", true},
		{"invalid-command", conntrackExit(2), nil, "", true},
		{"tool-missing", errors.New("not installed"), nil, "", true},
		{"binding-remains", nil, nil, "udp active binding", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := clearNATConnectionsWith([]Port{{ListenIP: "192.0.2.1", Number: 22000, Proto: "udp"}}, func(args ...string) (string, error) {
				calls++
				if !reflect.DeepEqual(args[1:], []string{"-f", "ipv4", "-p", "udp", "--orig-dst", "192.0.2.1", "--orig-port-dst", "22000", "--dst-nat"}) {
					t.Errorf("overbroad cleanup filter: %v", args)
				}
				if args[0] == "-D" {
					return "", tc.deleteErr
				}
				if args[0] != "-L" {
					t.Errorf("unexpected command: %v", args)
				}
				return tc.output, tc.listErr
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("cleanup result %v", err)
			}
			if !tc.wantError && calls != 2 {
				t.Fatal("missing independent verification")
			}
		})
	}
}

func TestDataDirectoryAllowsOnlyOneAgent(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDataDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockDataDirectory(dir); err == nil {
		_ = second.Close()
		t.Fatal("two Agents can write the same data directory")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := lockDataDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
}

func TestConntrackEditCleanupFiltersPreviousReplyTuple(t *testing.T) {
	port := Port{ListenIP: "192.0.2.1", Number: 22000, Proto: "udp", replyAddress: "10.80.0.10", Target: 8080}
	err := clearNATConnectionsWith([]Port{port}, func(args ...string) (string, error) {
		want := []string{"--reply-src", "10.80.0.10", "--reply-port-src", "8080"}
		if !reflect.DeepEqual(args[len(args)-4:], want) {
			t.Errorf("new NAT sessions could be removed: %v", args)
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDirectBindingCleanupExcludesLiveGuestNAT(t *testing.T) {
	port := Port{ListenIP: "192.0.2.1", Number: 22000, Proto: "udp", replyAddress: "192.0.2.1", Target: 22000, directBinding: true}
	err := clearNATConnectionsWith([]Port{port}, func(args ...string) (string, error) {
		want := []string{"-f", "ipv4", "-p", "udp", "--orig-dst", "192.0.2.1", "--orig-port-dst", "22000", "--reply-src", "192.0.2.1", "--reply-port-src", "22000"}
		if !reflect.DeepEqual(args[1:], want) {
			t.Errorf("direct binding filter could touch a guest or another listener: %v", args)
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

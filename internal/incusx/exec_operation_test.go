package incusx

import (
	"fmt"
	"net/http"
	"testing"
)

func TestExecReportsConfirmedAndUnconfirmedOperationOutcomes(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		status   int
		exit     int
		terminal bool
	}{
		{"still-running", 103, 0, false}, {"command-failure", 200, 9, true}, {"operation-failure", 400, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					fmt.Fprint(w, `{"type":"async","status_code":100,"operation":"/1.0/operations/exec-fixture"}`)
					return
				}
				operationResponse(w, scenario.status, "", map[string]any{"return": scenario.exit})
			})
			_, err := client.Exec("guest", []string{"true"}, nil)
			if err == nil {
				t.Fatal("failure reported success")
			}
			state := ExecOperationState(err)
			if state.ID != "/1.0/operations/exec-fixture" || state.Terminal != scenario.terminal {
				t.Fatalf("wrong operation state: %+v", state)
			}
		})
	}
}
func TestExecRetainsOperationWhenStreamSetupFails(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"type":"async","status_code":100,"operation":"/1.0/operations/stream-fixture","metadata":{"metadata":{"fds":{}}}}`)
	})
	_, err := client.Exec("guest", []string{"chpasswd"}, []byte("root:fixture-only\n"))
	state := ExecOperationState(err)
	if err == nil || state.Terminal || state.ID != "/1.0/operations/stream-fixture" {
		t.Fatal("stream setup failure discarded an active operation")
	}
}
func TestExecExplicitRejectionIsTerminal(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"type":"error","error_code":400,"error":"not running"}`)
	})
	_, err := client.Exec("guest", []string{"true"}, nil)
	if err == nil || !ExecOperationState(err).Terminal {
		t.Fatal("explicit rejection left an unknown operation")
	}
}

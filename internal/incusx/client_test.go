package incusx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Client{http: server.Client(), base: server.URL, project: "particeps"}
}

func operationResponse(w http.ResponseWriter, code int, message string, metadata any) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "sync", "status": "Success", "status_code": 200,
		"metadata": map[string]any{"status_code": code, "err": message, "metadata": metadata},
	})
}

func TestWaitChecksFinalOperationResult(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		err       string
		wantError bool
	}{
		{"success", 200, "", false},
		{"failed-inside-success-envelope", 400, "storage is full", true},
		{"still-running-after-timeout", 103, "", true},
		{"missing-final-status", 0, "", true},
		{"error-with-success-code", 200, "backend failure", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				operationResponse(w, tc.code, tc.err, nil)
			})
			err := client.Wait("/1.0/operations/test-operation")
			if (err != nil) != tc.wantError {
				t.Fatalf("Wait error = %v, want error %v", err, tc.wantError)
			}
		})
	}
}

func TestWaitPreservesOperationQuery(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/operations/test-operation/wait" || r.URL.Query().Get("project") != "particeps" || r.URL.Query().Get("timeout") != "600" {
			t.Errorf("invalid wait URL: %s", r.URL.String())
		}
		operationResponse(w, 200, "", nil)
	})
	if err := client.Wait("/1.0/operations/test-operation?project=particeps"); err != nil {
		t.Fatal(err)
	}
}

func TestDoRejectsHTTPFailureWithoutErrorEnvelope(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"type":"sync","status_code":200,"metadata":{}}`)
	})
	if _, err := client.do(http.MethodGet, "/1.0", nil); err == nil {
		t.Fatal("HTTP 503 was reported as success")
	}
}

func TestExecChecksCommandExitStatus(t *testing.T) {
	for _, code := range []int{0, 17} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					_, _ = fmt.Fprint(w, `{"type":"async","status_code":100,"operation":"/1.0/operations/test-exec"}`)
					return
				}
				operationResponse(w, 200, "", map[string]any{"return": code})
			})
			_, err := client.Exec("guest", []string{"false"}, nil)
			if (err != nil) != (code != 0) {
				t.Fatalf("exec exit %d, error = %v", code, err)
			}
		})
	}
}

func TestPasswordRejectsAdditionalInputRecords(t *testing.T) {
	for _, password := range []string{"one\nother:two", "one\rname:two", "one\x00two"} {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("invalid password must be rejected before contacting Incus")
			http.Error(w, "unexpected request", http.StatusBadRequest)
		})
		if err := client.SetRootPassword("guest", password); err == nil {
			t.Fatal("invalid password was accepted")
		} else if strings.Contains(err.Error(), password) {
			t.Fatal("password was included in the error")
		}
	}
}

func TestConfigUpdateExposesOperationBeforeWaiting(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Error("operation waited before its reference could be persisted")
		}
		_, _ = fmt.Fprint(w, `{"type":"async","status_code":100,"operation":"/1.0/operations/resource?project=particeps"}`)
	})
	op, err := client.BeginConfigUpdate("guest", map[string]string{"limits.cpu.allowance": "150ms/100ms"}, nil)
	if err != nil || op.Terminal || op.ID != "/1.0/operations/resource?project=particeps" {
		t.Fatalf("operation: %+v %v", op, err)
	}
}

func TestConfigOperationDistinguishesRunningAndTerminalStates(t *testing.T) {
	for _, code := range []int{0, 103, 200, 400, 401} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/1.0/operations/resource" || r.URL.Query().Get("project") != "particeps" {
					t.Errorf("operation URL: %s", r.URL)
				}
				operationResponse(w, code, "", nil)
			})
			finished, err := client.ConfigOperationFinished("/1.0/operations/resource?project=particeps")
			if err != nil || finished != (code == 200 || code == 400 || code == 401) {
				t.Fatalf("state %d: finished=%t error=%v", code, finished, err)
			}
		})
	}
}

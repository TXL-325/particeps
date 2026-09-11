package incusx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestExecStreamsStdinAndChecksResult(t *testing.T) {
	for _, exitCode := range []int{0, 9} {
		t.Run(fmt.Sprint(exitCode), func(t *testing.T) {
			var mu sync.Mutex
			var input string
			inputDone := make(chan struct{})
			upgrader := websocket.Upgrader{}
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost:
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["wait-for-websocket"] != true || body["record-output"] != false {
						t.Errorf("unexpected streaming flags: %v", body)
					}
					_, _ = fmt.Fprint(w, `{"type":"async","status_code":100,"operation":"/1.0/operations/exec-test","metadata":{"metadata":{"fds":{"0":"stdin","1":"stdout","2":"stderr"}}}}`)
				case strings.HasSuffix(r.URL.Path, "/websocket"):
					conn, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					if r.URL.Query().Get("secret") == "stdin" {
						kind, data, err := conn.ReadMessage()
						if err != nil || kind != websocket.BinaryMessage {
							t.Errorf("stdin frame: %d %v", kind, err)
							return
						}
						mu.Lock()
						input = string(data)
						mu.Unlock()
						kind, data, err = conn.ReadMessage()
						if err != nil || kind != websocket.TextMessage || len(data) != 0 {
							t.Errorf("stdin EOF: %d %v", kind, err)
						}
						close(inputDone)
						return
					}
					<-inputDone
					if r.URL.Query().Get("secret") == "stdout" {
						_ = conn.WriteMessage(websocket.BinaryMessage, []byte("probe output"))
					}
					_ = conn.WriteMessage(websocket.TextMessage, nil)
				case strings.HasSuffix(r.URL.Path, "/wait"):
					<-inputDone
					operationResponse(w, 200, "", map[string]any{"return": exitCode})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			output, err := client.Exec("guest", []string{"cat"}, []byte("probe input"))
			if (err != nil) != (exitCode != 0) {
				t.Fatalf("exit %d, error %v", exitCode, err)
			}
			mu.Lock()
			got := input
			mu.Unlock()
			if got != "probe input" || output != "probe output" {
				t.Fatalf("input %q, output %q", got, output)
			}
		})
	}
}

// Opt-in checks execute only read-only commands inside an existing test guest.
func TestIncusReadOnlyIntegration(t *testing.T) {
	socket := os.Getenv("PARTICEPS_TEST_INCUS_SOCKET")
	guest := os.Getenv("PARTICEPS_TEST_GUEST")
	if socket == "" || guest == "" {
		t.Skip("requires explicit test Incus socket and existing guest")
	}
	c := Connect(socket, "particeps")
	if err := c.Ready(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetState(guest); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(guest, []string{"sh", "-c", "exit 7"}, nil); err == nil {
		t.Fatal("real nonzero command exit was accepted")
	}
	const probe = "particeps read-only stdin probe\n"
	output, err := c.Exec(guest, []string{"cat"}, []byte(probe))
	if err != nil {
		t.Fatal(err)
	}
	if output != probe {
		t.Fatalf("unexpected round-trip output: %q", output)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"particeps/internal/core"
	"particeps/internal/incusx"
)

// An instance with only disabled reservations never needs a live forward or
// conntrack command. Unexpected backend mutations still fail the test.
type reservedPortBackend struct{ core.IncusBackend }

func (reservedPortBackend) GetState(string) (*incusx.InstanceState, error) {
	return &incusx.InstanceState{Status: "Stopped"}, nil
}

func (reservedPortBackend) GetForward(string, string) (incusx.Forward, string, error) {
	return incusx.Forward{}, "", &incusx.StatusError{Code: http.StatusNotFound}
}

func portAPIServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, session := testServer(t)
	s.App.Incus = reservedPortBackend{}
	s.App.Metrics = s.App.Store
	_, err := s.App.Store.DB.Exec(`
INSERT INTO instances(id,name,incus_name,display_name,image,cpu_cores,memory_mib,disk_gib,stack_mode,desired_power,nat_ipv4,created_at)
VALUES('guest','guest','p-guest','guest','alpine',0.5,128,1,'v4','stopped','192.0.2.1',0);
INSERT INTO ports(instance_id,number,proto,listen_ip,target,enabled) VALUES('guest',20001,'tcp','192.0.2.1',20001,0);
INSERT INTO instance_network(instance_id,status,updated_at) VALUES('guest','inactive',0);`)
	if err != nil {
		t.Fatal(err)
	}
	return s, session
}

func patchPortRequest(s *Server, session, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPatch, "http://agent.test/api/v1/instances/guest/ports/20001/tcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://agent.test")
	r.AddCookie(&http.Cookie{Name: "particeps_session", Value: session})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestPortPatchAcceptsOptionalFieldsWithoutImplicitActivation(t *testing.T) {
	s, session := portAPIServer(t)
	for _, body := range []string{`{"target":8080}`, `{"enabled":false}`, `{"target":8443,"enabled":false}`} {
		w := patchPortRequest(s, session, body)
		if w.Code != http.StatusOK {
			t.Fatalf("valid port patch %s: %d %s", body, w.Code, w.Body.String())
		}
		var instance struct {
			Ports []struct {
				Target  int   `json:"target"`
				Enabled *bool `json:"enabled"`
			} `json:"ports"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &instance); err != nil {
			t.Fatal(err)
		}
		if len(instance.Ports) != 1 || instance.Ports[0].Enabled == nil || *instance.Ports[0].Enabled {
			t.Fatalf("patch omitted or changed the disabled state: %s", w.Body.String())
		}
		wantTarget := 8080
		if strings.Contains(body, "8443") {
			wantTarget = 8443
		}
		if instance.Ports[0].Target != wantTarget {
			t.Fatalf("omitted target was not preserved: %s", w.Body.String())
		}
	}
}

func TestPortPatchRejectsInvalidChangesWithoutMutatingReservations(t *testing.T) {
	s, session := portAPIServer(t)
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"target":null}`, `{"target":0}`, `{"target":65536}`, `{"enabled":"false"}`, `{"target":8080,"enable":true}`} {
		w := patchPortRequest(s, session, body)
		if w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
			t.Fatalf("invalid port patch %s: %d %s", body, w.Code, w.Body.String())
		}
		var target int
		var enabled bool
		if err := s.App.Store.DB.QueryRow(`SELECT target,enabled FROM ports WHERE number=20001`).Scan(&target, &enabled); err != nil {
			t.Fatal(err)
		}
		if target != 20001 || enabled {
			t.Fatalf("invalid patch changed a reservation: %d %t", target, enabled)
		}
	}
}

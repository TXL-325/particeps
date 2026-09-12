package incusx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestForwardUpdateUsesETagAndPreservesWritableFields(t *testing.T) {
	want := Forward{ListenAddress: "192.0.2.1", Description: "shared", Config: map[string]string{"user.note": "keep"}, Ports: []ForwardPort{
		{Description: "owner-a", Protocol: "tcp", ListenPort: "22000", TargetPort: "22", TargetAddress: "10.80.0.2"},
		{Description: "owner-b", Protocol: "udp", ListenPort: "22001-22002", TargetPort: "53", TargetAddress: "10.80.0.3"},
	}}
	updates := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("project") != "particeps" {
			t.Errorf("missing project scope: %s", r.URL)
		}
		w.Header().Set("ETag", `"revision-1"`)
		if r.Method == http.MethodPut {
			updates++
			if r.Header.Get("If-Match") != `"revision-1"` {
				t.Error("conditional write did not use read ETag")
			}
			var body Forward
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.ListenAddress != "" || body.Config["user.note"] != "keep" || len(body.Ports) != 2 || body.Ports[1].Description != "owner-b" {
				t.Errorf("lost writable fields or submitted read-only address: %+v", body)
			}
			_, _ = fmt.Fprint(w, `{"type":"sync","status_code":200,"metadata":{}}`)
			return
		}
		metadata := any(want)
		if r.URL.Query().Get("recursion") == "1" {
			metadata = []Forward{want}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "status_code": 200, "metadata": metadata})
	})
	list, err := client.ListForwards("particepsbr0")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	f, etag, err := client.GetForward("particepsbr0", want.ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	f.Ports[0].TargetPort = "2222"
	if err := client.UpdateForward("particepsbr0", f, etag); err != nil {
		t.Fatal(err)
	}
	if updates != 1 {
		t.Fatalf("updates=%d", updates)
	}
	if err := client.UpdateForward("particepsbr0", f, ""); err == nil || updates != 1 {
		t.Fatal("empty ETag did not prevent write")
	}
}

func TestForwardConflictAndNotFoundHaveTypedStatus(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error_code": code, "error": "test failure"})
			})
			err := client.CreateForward("bridge", Forward{ListenAddress: "192.0.2.1"})
			if !IsStatus(err, code) || IsForwardUncertain(err) {
				t.Fatalf("status was lost: %v", err)
			}
		})
	}
}

func TestForwardTransportFailureIsUncertain(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	})
	err := client.CreateForward("bridge", Forward{ListenAddress: "192.0.2.1"})
	if !IsForwardUncertain(err) || IsStatus(err, http.StatusNotFound) {
		t.Fatalf("lost response was treated as a confirmed outcome: %v", err)
	}
}

func TestForwardReadRejectsMissingETagAndWrongIdentity(t *testing.T) {
	for _, missingETag := range []bool{false, true} {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			listen := "192.0.2.2"
			if missingETag {
				listen = "192.0.2.1"
			} else {
				w.Header().Set("ETag", `"revision-1"`)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "sync", "metadata": Forward{ListenAddress: listen}})
		})
		if _, _, err := client.GetForward("bridge", "192.0.2.1"); err == nil {
			t.Fatal("unusable forward read accepted")
		}
	}
}

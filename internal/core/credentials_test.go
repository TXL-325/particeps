package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func saveTestCredential(t *testing.T, a *App, extra string) {
	t.Helper()
	_, err := a.Store.DB.Exec(`INSERT INTO tasks(id,kind,request_hash,status,created_at,updated_at) VALUES('credentials','create','h','ok',0,0);
		INSERT INTO task_items(task_id,instance_id,name,status,step,result_json) VALUES('credentials','guest','guest','ok','done',?)`, extra)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInitialCredentialsAreEncryptedAndConsumedOnce(t *testing.T) {
	a, _ := testApp(t)
	const password = "unique-test-password"
	sealed, err := a.sealCredential("credentials", "guest", password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, password) {
		t.Fatal("stored credential is plaintext")
	}
	saveTestCredential(t, a, sealed)
	task, err := a.GetTask("credentials")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(task)
	if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), `"password"`) {
		t.Fatal("ordinary task response contains a password")
	}
	if !task.Items[0].CredentialAvailable {
		t.Fatal("initial credential is unavailable")
	}
	claimed, err := a.ClaimInitialCredentials("credentials")
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].Password != password {
		t.Fatal("initial credential delivery failed")
	}
	again, err := a.ClaimInitialCredentials("credentials")
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatal("credential was delivered twice")
	}
	var remaining string
	if err := a.Store.DB.QueryRow(`SELECT result_json FROM task_items WHERE task_id='credentials'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != "" {
		t.Fatal("claimed encrypted credential was not cleared")
	}
}

func TestExpiredOrSwappedCredentialsCannotBeDelivered(t *testing.T) {
	for _, scenario := range []string{"expired", "wrong-instance"} {
		t.Run(scenario, func(t *testing.T) {
			a, _ := testApp(t)
			sealed, err := a.sealCredential("credentials", "guest", "test-password")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				var result taskResult
				_ = json.Unmarshal([]byte(sealed), &result)
				result.Credential.ExpiresAt = time.Now().Add(-time.Minute).Unix()
				data, _ := json.Marshal(result)
				sealed = string(data)
			}
			saveTestCredential(t, a, sealed)
			if scenario == "wrong-instance" {
				if _, err := a.Store.DB.Exec(`UPDATE task_items SET instance_id='different'`); err != nil {
					t.Fatal(err)
				}
			}
			result, err := a.ClaimInitialCredentials("credentials")
			if scenario == "wrong-instance" && err == nil {
				t.Fatal("credential was decrypted for a different instance")
			}
			if len(result) != 0 {
				t.Fatal("invalid or expired credential was delivered")
			}
		})
	}
}

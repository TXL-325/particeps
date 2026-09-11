package core

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentTaskCompletionKeepsTerminalSummary(t *testing.T) {
	a, _ := testApp(t)
	if _, err := a.Store.DB.Exec(`INSERT INTO tasks(id,kind,request_hash,status,created_at,updated_at) VALUES('batch','create','h','running',0,0)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := a.Store.DB.Exec(`INSERT INTO task_items(task_id,name,status) VALUES('batch',?,'running')`, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			a.refreshTask("batch")
			if _, err := a.Store.DB.Exec(`UPDATE task_items SET status='ok' WHERE task_id='batch' AND name=?`, fmt.Sprint(index)); err != nil {
				t.Error(err)
				return
			}
			a.refreshTask("batch")
		}(i)
	}
	workers.Wait()
	task, err := a.GetTask("batch")
	if err != nil || task.Status != "ok" || len(task.Items) != 20 {
		t.Fatalf("completed task was not terminal: %+v %v", task, err)
	}
	for _, change := range []struct{ query, want string }{
		{`UPDATE task_items SET status='failed' WHERE task_id='batch' AND name='0'`, "partial"},
		{`UPDATE task_items SET status='failed' WHERE task_id='batch'`, "failed"},
	} {
		if _, err := a.Store.DB.Exec(change.query); err != nil {
			t.Fatal(err)
		}
		a.refreshTask("batch")
		task, err := a.GetTask("batch")
		if err != nil || task.Status != change.want {
			t.Fatalf("summary: %+v %v", task, err)
		}
	}
}

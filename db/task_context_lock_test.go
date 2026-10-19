package db

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// BEGIN IMMEDIATE replaces row/advisory locks: a writer must acquire its
// reservation before reading references, while WAL readers remain available.
func TestSQLiteWALReadersAndReferenceMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "business.sqlite")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	profile, err := d.SaveProfile(&LLMProfile{Name: "sqlite-reference-profile", Format: "openai", Model: "test", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := d.CreateTaskWithOptions("sqlite task", "goal", TaskCreateOptions{Name: "sqlite task", LLMProfileIDs: []int64{profile}})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := d.CreateAgent("sqlite_reference_agent", "agent", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.SetAgentLLMProfile(agent.Key, &profile); err != nil {
		t.Fatal(err)
	}
	conv, err := d.CreateConversation("planner", "conversation", &profile)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.Exec(`UPDATE tasks SET name='uncommitted' WHERE id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var name string
	if err = other.QueryRowContext(ctx, `SELECT name FROM tasks WHERE id=$1`, task.ID).Scan(&name); err != nil || name != "sqlite task" {
		t.Fatalf("WAL reader: name=%q err=%v", name, err)
	}
	done := make(chan error, 1)
	go func() { done <- other.DeleteProfile(profile) }()
	select {
	case err := <-done:
		t.Fatalf("writer escaped active transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not resume")
	}
	got, err := d.GetTask(task.ID)
	if err != nil || got == nil || got.ActiveLLMProfileID != nil || len(got.LLMProfileIDs) != 0 {
		t.Fatalf("task references %+v err=%v", got, err)
	}
	a, err := d.GetAgentByKey(agent.Key)
	if err != nil || a.LLMProfileID != nil {
		t.Fatalf("agent references %+v err=%v", a, err)
	}
	c, err := d.GetConversation(conv.ID)
	if err != nil || c.LLMProfileID != nil {
		t.Fatalf("conversation references %+v err=%v", c, err)
	}
	var invalid int
	rows, err := d.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		invalid++
	}
	rows.Close()
	if invalid != 0 {
		t.Fatal(fmt.Sprintf("%d foreign key violations", invalid))
	}
}

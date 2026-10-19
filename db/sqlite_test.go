package db

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestSQLiteMixedWorkload(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "业务 数据#", "artex.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.EnsureLLMRecordsTable(); err != nil {
		t.Fatal(err)
	}
	if err = d.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	const workers = 4
	const iterations = 30
	tasks := make([]*Task, workers)
	for i := range tasks {
		tasks[i], err = d.CreateTaskWithOptions("mixed workload", "test", TaskCreateOptions{Name: fmt.Sprintf("task %d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	errors := make(chan error, workers+2)
	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Go(func() {
			for n := 0; n < iterations; n++ {
				_, err := d.Assets().UpsertRootDomain(UpsertRootDomainReq{Domain: fmt.Sprintf("worker-%d-%d.example", i, n), TaskID: task.ID})
				if err != nil {
					errors <- err
					return
				}
				if err = d.InsertLLMUsage(&LLMUsage{TaskID: fmt.Sprint(task.ID), ExplorationID: task.ExplorationID, Worker: "worker", Model: "test", InputTokens: 10, OutputTokens: 3, Status: "ok"}); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	wg.Go(func() {
		for n := 0; n < iterations; n++ {
			if _, err := d.TaskListMetricsAll(); err != nil {
				errors <- err
				return
			}
		}
	})
	wg.Go(func() {
		for n := 0; n < 3; n++ {
			snapshot, err := d.SnapshotTaskArchive(tasks[0].ID)
			if err != nil {
				errors <- err
				return
			}
			if !json.Valid(snapshot.Tables["assets"]) {
				errors <- fmt.Errorf("invalid asset snapshot")
				return
			}
		}
	})
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	var assets, usage int
	if err = d.QueryRow(`SELECT count(*) FROM assets`).Scan(&assets); err != nil {
		t.Fatal(err)
	}
	if err = d.QueryRow(`SELECT count(*) FROM llm_usage`).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if assets != workers*iterations || usage != workers*iterations {
		t.Fatalf("lost writes: assets=%d usage=%d", assets, usage)
	}
}

func TestSQLiteJSONIntegersAndUnicodeSearch(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var raw []byte
	if err = d.QueryRow(`SELECT json_array_union($1,$2)`, `[9007199254740993]`, `[9007199254740994]`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	if err = json.Unmarshal(raw, &ids); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 9007199254740993 || ids[1] != 9007199254740994 {
		t.Fatalf("integer precision lost: %s", raw)
	}
	var match bool
	if err = d.QueryRow(`SELECT $1 LIKE $2`, "École", "é%").Scan(&match); err != nil || !match {
		t.Fatalf("Unicode case search: %v %v", match, err)
	}
	conns := make([]interface{ Close() error }, 0, 4)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		conn, err := d.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		var enabled int
		if err = conn.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
			t.Fatalf("connection %d missing foreign keys: %d %v", i, enabled, err)
		}
	}
}

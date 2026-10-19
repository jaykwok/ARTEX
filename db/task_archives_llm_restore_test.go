package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func streamedRestoreFixture(t *testing.T, d *DB) (*TaskArchive, *TaskArchiveSnapshot, []byte) {
	t.Helper()
	task, err := d.CreateTaskWithOptions("streaming restore", "test", TaskCreateOptions{Name: "restore batches"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetPaused(task.ID, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < archiveLLMBatchRows*2+3; i++ {
		if err := d.InsertLLMRecord(&LLMRecord{TaskID: fmt.Sprint(task.ID), Model: "restore-test", RawResponse: fmt.Sprintf("record %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	job, err := d.QueueTaskArchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ClaimTaskArchiveJob(t.Context()); err != nil {
		t.Fatal(err)
	}
	var stream bytes.Buffer
	snapshot, err := d.SnapshotTaskArchiveWithLLMRecords(task.ID, &stream)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CompleteTaskArchive(job.ID, snapshot, "test.tar.zst", "checksum", 100, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := d.QueueTaskArchiveRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ClaimTaskArchiveJob(t.Context()); err != nil {
		t.Fatal(err)
	}
	return job, snapshot, stream.Bytes()
}

type pausedArchiveReader struct {
	reader  io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *pausedArchiveReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		close(r.entered)
		<-r.release
	})
	return r.reader.Read(p)
}

func TestStreamedArchiveRestoreReleasesWriterWhileReading(t *testing.T) {
	d := openSQLiteRegressionDB(t)
	job, snapshot, stream := streamedRestoreFixture(t, d)
	lines := bytes.SplitAfter(stream, []byte("\n"))
	prefix := bytes.Join(lines[:archiveLLMBatchRows], nil)
	paused := &pausedArchiveReader{
		reader: bytes.NewReader(stream[len(prefix):]), entered: make(chan struct{}), release: make(chan struct{}),
	}
	// Keep a dedicated connection with a short busy timeout, reproducing the
	// unrelated writer that would fail while the former restore held its lock.
	writer, err := d.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(t.Context(), `PRAGMA busy_timeout=100`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, io.MultiReader(bytes.NewReader(prefix), paused))
		result <- err
	}()
	var release sync.Once
	defer func() {
		release.Do(func() { close(paused.release) })
		select {
		case <-result:
		case <-time.After(5 * time.Second):
			t.Error("restore did not stop")
		}
	}()
	select {
	case <-paused.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("restore did not reach paused reader")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, err := writer.ExecContext(ctx, `INSERT INTO llm_usage(task_id,model,input_tokens) VALUES('unrelated','writer-test',17)`); err != nil {
		t.Fatalf("unrelated usage write blocked by archive read: %v", err)
	}
	committed, _, err := archiveLLMCheckpoint(d, job.ID, snapshot)
	if err != nil || committed != archiveLLMBatchRows {
		t.Fatalf("checkpoint = %d, %v", committed, err)
	}
	if live, err := d.GetTask(job.TaskID); err != nil || live != nil {
		t.Fatalf("partially restored task became visible: %+v, %v", live, err)
	}
	release.Do(func() { close(paused.release) })
	err = <-result
	// Put the result back so cleanup can always join the goroutine.
	result <- err
	if err != nil {
		t.Fatal(err)
	}
	assertStreamedRestoreComplete(t, d, job, snapshot)
}

type archiveReadError struct{}

func (archiveReadError) Read([]byte) (int, error) { return 0, errors.New("interrupted archive read") }

func TestStreamedArchiveRestoreResumesCommittedBatchesAfterRestart(t *testing.T) {
	d := openSQLiteRegressionDB(t)
	job, snapshot, stream := streamedRestoreFixture(t, d)
	lines := bytes.SplitAfter(stream, []byte("\n"))
	prefix := bytes.Join(lines[:archiveLLMBatchRows+2], nil)
	_, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, io.MultiReader(bytes.NewReader(prefix), archiveReadError{}))
	if err == nil {
		t.Fatal("read failure was ignored")
	}
	committed, _, err := archiveLLMCheckpoint(d, job.ID, snapshot)
	if err != nil || committed != archiveLLMBatchRows {
		t.Fatalf("checkpoint = %d, %v", committed, err)
	}
	if live, err := d.GetTask(job.TaskID); err != nil || live != nil {
		t.Fatalf("failed restore exposed task: %+v, %v", live, err)
	}
	var path string
	if err := d.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.RecoverTaskArchiveJobs(); err != nil {
		t.Fatal(err)
	}
	if claimed, err := d.ClaimTaskArchiveJob(t.Context()); err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("restart claim: %+v, %v", claimed, err)
	}
	if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(stream)); err != nil {
		t.Fatal(err)
	}
	assertStreamedRestoreComplete(t, d, job, snapshot)
}

func TestStreamedArchiveRestoreRejectsInvalidHistory(t *testing.T) {
	for _, scenario := range []string{"truncated", "extra", "wrong task", "duplicate ID", "ID conflict"} {
		t.Run(scenario, func(t *testing.T) {
			d := openSQLiteRegressionDB(t)
			job, snapshot, stream := streamedRestoreFixture(t, d)
			lines := bytes.SplitAfter(stream, []byte("\n"))
			switch scenario {
			case "truncated":
				stream = bytes.Join(lines[:archiveLLMBatchRows+2], nil)
			case "extra":
				snapshot.DataCounts["llm_records"]--
			case "wrong task":
				stream = bytes.Replace(stream, []byte(`"task_id":"`+fmt.Sprint(job.TaskID)+`"`), []byte(`"task_id":"unrelated"`), 1)
			case "duplicate ID":
				stream = append(append([]byte{}, lines[0]...), stream...)
			case "ID conflict":
				if _, err := d.Exec(`INSERT INTO llm_records(id,task_id,raw_response) VALUES(1,'unrelated','keep me')`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(stream)); err == nil {
				t.Fatal("invalid history accepted")
			}
			if live, err := d.GetTask(job.TaskID); err != nil || live != nil {
				t.Fatalf("invalid restore exposed task: %+v, %v", live, err)
			}
			if scenario == "ID conflict" {
				var body string
				if err := d.QueryRow(`SELECT raw_response FROM llm_records WHERE id=1`).Scan(&body); err != nil || body != "keep me" {
					t.Fatalf("conflicting record changed: %q, %v", body, err)
				}
			}
		})
	}
}

func TestStreamedArchiveRestoreRetriesAfterManifestRollback(t *testing.T) {
	d := openSQLiteRegressionDB(t)
	job, snapshot, stream := streamedRestoreFixture(t, d)
	nodes := snapshot.Tables["exploration_nodes"]
	snapshot.Tables["exploration_nodes"] = []byte(`[{"id":null}]`)
	if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(stream)); err == nil {
		t.Fatal("invalid manifest accepted")
	}
	committed, _, err := archiveLLMCheckpoint(d, job.ID, snapshot)
	if err != nil || committed != snapshot.DataCounts["llm_records"] {
		t.Fatalf("completed history checkpoint lost: %d, %v", committed, err)
	}
	if live, err := d.GetTask(job.TaskID); err != nil || live != nil {
		t.Fatalf("rolled-back manifest exposed task: %+v, %v", live, err)
	}
	snapshot.Tables["exploration_nodes"] = nodes
	if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(stream)); err != nil {
		t.Fatal(err)
	}
	assertStreamedRestoreComplete(t, d, job, snapshot)
}

func TestStreamedArchiveRestoreProtectsHistoryFromDeletion(t *testing.T) {
	d := openSQLiteRegressionDB(t)
	job, snapshot, stream := streamedRestoreFixture(t, d)
	prefix := bytes.Join(bytes.SplitAfter(stream, []byte("\n"))[:archiveLLMBatchRows+2], nil)
	if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, io.MultiReader(bytes.NewReader(prefix), archiveReadError{})); err == nil {
		t.Fatal("read failure was ignored")
	}
	assertProtected := func(want int64) {
		t.Helper()
		if n, err := d.DeleteLLMRecords(fmt.Sprint(job.TaskID)); !errors.Is(err, ErrTaskArchiveState) || n != 0 {
			t.Fatalf("delete partial restore = %d, %v", n, err)
		}
		if _, err := d.DeleteTaskCascade(job.TaskID, true, true, true); !errors.Is(err, ErrTaskArchiveState) {
			t.Fatalf("cascade deletion bypassed restore protection: %v", err)
		}
		var count int64
		if err := d.QueryRow(`SELECT count(*) FROM llm_records WHERE task_id=$1`, fmt.Sprint(job.TaskID)).Scan(&count); err != nil || count != want {
			t.Fatalf("protected history = %d, want %d: %v", count, want, err)
		}
		if err := d.InsertLLMRecord(&LLMRecord{TaskID: "unrelated", Model: "other"}); err != nil {
			t.Fatal(err)
		}
		if n, err := d.DeleteLLMRecords("unrelated"); err != nil || n != 1 {
			t.Fatalf("unrelated deletion = %d, %v", n, err)
		}
	}
	assertProtected(32)
	if err := d.FailTaskArchiveJob(job.ID, Restoring, errors.New("interrupted")); err != nil {
		t.Fatal(err)
	}
	assertProtected(32)
	if _, err := d.QueueTaskArchiveRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	assertProtected(32)
	if _, err := d.ClaimTaskArchiveJob(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(stream)); err != nil {
		t.Fatal(err)
	}
	assertStreamedRestoreComplete(t, d, job, snapshot)
	assertProtected(67) // Still protected while the worker consumes the package.
	if err := d.CompleteTaskArchiveRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := d.DeleteLLMRecords(fmt.Sprint(job.TaskID)); err != nil || n != 67 {
		t.Fatalf("delete completed restore = %d, %v", n, err)
	}
}

func TestStreamedArchiveRestoreRejectsCheckpointDataLoss(t *testing.T) {
	for _, replaceID := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace=%v", replaceID), func(t *testing.T) {
			d := openSQLiteRegressionDB(t)
			job, snapshot, stream := streamedRestoreFixture(t, d)
			prefix := bytes.Join(bytes.SplitAfter(stream, []byte("\n"))[:archiveLLMBatchRows+2], nil)
			if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, io.MultiReader(bytes.NewReader(prefix), archiveReadError{})); err == nil {
				t.Fatal("read failure was ignored")
			}
			// Bypass the protected API to simulate pre-existing loss or a manual
			// database edit. A valid checkpoint alone must never finalize restore.
			if _, err := d.Exec(`DELETE FROM llm_records WHERE task_id=$1`, fmt.Sprint(job.TaskID)); err != nil {
				t.Fatal(err)
			}
			if replaceID {
				for i := 0; i < archiveLLMBatchRows; i++ {
					if _, err := d.Exec(`INSERT INTO llm_records(id,task_id) VALUES($1,$2)`, 1000+i, fmt.Sprint(job.TaskID)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(stream)); err == nil || !strings.Contains(err.Error(), "actual records do not match") {
				t.Fatalf("restore accepted damaged history: %v", err)
			}
			if restored, err := d.IsTaskArchiveRestored(job.ID); err != nil || restored {
				t.Fatalf("damaged restore marked complete: %v, %v", restored, err)
			}
			if live, err := d.GetTask(job.TaskID); err != nil || live != nil {
				t.Fatalf("damaged restore exposed task: %+v, %v", live, err)
			}
			// The server uses IsTaskArchiveRestored to decide whether it may
			// consume the package; failed validation must retain its metadata.
			if err := d.CompleteTaskArchiveRestore(job.ID); !errors.Is(err, ErrTaskArchiveState) {
				t.Fatalf("damaged restore consumed metadata: %v", err)
			}
			if archive, err := d.GetTaskArchive(job.ID); err != nil || archive == nil {
				t.Fatalf("archive metadata lost: %+v, %v", archive, err)
			}
		})
	}
}

func assertStreamedRestoreComplete(t *testing.T, d *DB, job *TaskArchive, snapshot *TaskArchiveSnapshot) {
	t.Helper()
	var count int64
	if err := d.QueryRow(`SELECT count(*) FROM llm_records WHERE task_id=$1`, fmt.Sprint(job.TaskID)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != snapshot.DataCounts["llm_records"] {
		t.Fatalf("restored records = %d, want %d", count, snapshot.DataCounts["llm_records"])
	}
	if live, err := d.GetTask(job.TaskID); err != nil || live == nil {
		t.Fatalf("completed restore hidden: %+v, %v", live, err)
	}
}

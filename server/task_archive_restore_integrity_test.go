package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestTaskArchiveRestoreRetainsPackageOnHistoryLoss(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTaskWithOptions("restore integrity", "test", db.TaskCreateOptions{Name: "protected restore"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := fmt.Sprint(task.ID)
	if err := d.SetPaused(task.ID, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 67; i++ {
		if err := d.InsertLLMRecord(&db.LLMRecord{TaskID: taskID, RawResponse: fmt.Sprintf("record %d", i)}); err != nil {
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
	payload := filepath.Join(dir, "payload")
	streamFile := filepath.Join(payload, filepath.FromSlash(db.TaskArchiveLLMRecordsPath))
	if err := os.MkdirAll(filepath.Dir(streamFile), archiveDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(streamFile, stream.Bytes(), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	archivePath := taskArchivePath(dir, job.ID, taskID)
	original, compressed, checksum, err := writeTaskArchivePackage(archivePath, payload, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CompleteTaskArchive(job.ID, snapshot, archivePath, checksum, original, compressed); err != nil {
		t.Fatal(err)
	}
	if _, err := d.QueueTaskArchiveRestore(job.ID); err != nil {
		t.Fatal(err)
	}
	job, err = d.ClaimTaskArchiveJob(t.Context())
	if err != nil || job == nil {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	prefix := bytes.Join(bytes.SplitAfter(stream.Bytes(), []byte("\n"))[:34], nil)
	if _, err := d.RestoreTaskArchiveWithLLMRecords(job.ID, snapshot, 0, bytes.NewReader(prefix)); err == nil {
		t.Fatal("truncated history accepted")
	}
	s := &Server{ctx: context.Background(), m: &Manager{dir: dir, pg: d}}
	response := httptest.NewRecorder()
	s.pgDeleteLLMRecords(response, httptest.NewRequest(http.MethodDelete, "/api/llm/records?task="+taskID, nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("delete during restore: %d %s", response.Code, response.Body.String())
	}
	var count int64
	if err := d.QueryRow(`SELECT count(*) FROM llm_records WHERE task_id=$1`, taskID).Scan(&count); err != nil || count != 32 {
		t.Fatalf("protected history: %d, %v", count, err)
	}
	// Simulate data already lost before protection was installed. Retrying
	// through the real package restore path must fail before consuming it.
	if _, err := d.Exec(`DELETE FROM llm_records WHERE task_id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreTaskArchive(job); err == nil || !strings.Contains(err.Error(), "actual records do not match") {
		t.Fatalf("history loss not detected by package restore: %v", err)
	}
	if info, err := os.Stat(archivePath); err != nil || info.Size() != compressed {
		t.Fatalf("original package was consumed or changed: %+v, %v", info, err)
	}
	if restored, err := d.IsTaskArchiveRestored(job.ID); err != nil || restored {
		t.Fatalf("damaged task marked restored: %v, %v", restored, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM llm_records WHERE task_id=$1`, taskID).Scan(&count); err != nil || count != 35 {
		t.Fatalf("fixture did not reproduce missing prefix: %d, %v", count, err)
	}
}

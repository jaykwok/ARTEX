package db

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

const (
	archiveLLMBatchRows  = 32
	archiveLLMBatchBytes = 1 << 20
	archiveLLMWriteSlice = 50 * time.Millisecond
)

// The package is checksum-verified by the archive worker and streamed in ID
// order. A committed prefix is retained on failure and skipped on retry. Never
// read from the package while holding SQLite's sole writer lock.
func (d *DB) restoreArchiveLLMRecords(archiveID int64, snapshot *TaskArchiveSnapshot, reader io.Reader) ([]byte, error) {
	expected, ok := snapshot.DataCounts["llm_records"]
	if !ok || expected < 0 {
		return nil, errors.New("missing or invalid LLM record count")
	}
	committed, lastCommittedID, err := archiveLLMCheckpoint(d, archiveID, snapshot)
	if err != nil {
		return nil, err
	}
	if committed > expected {
		return nil, errors.New("LLM restore checkpoint exceeds manifest count")
	}
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	ids := sha256.New()
	var count, lastID int64
	var batch []map[string]any
	batchBytes := 0
	flush := func() error {
		for len(batch) > 0 {
			n, err := d.restoreArchiveLLMBatch(archiveID, snapshot, committed, lastCommittedID, batch)
			if err != nil {
				return err
			}
			committed += int64(n)
			lastCommittedID, _ = jsonInt64(batch[n-1]["id"])
			batch = batch[n:]
		}
		batchBytes = 0
		return nil
	}
	for {
		// Decode and validate outside the write transaction, including retries.
		var row map[string]any
		if err := decoder.Decode(&row); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		id, valid := jsonInt64(row["id"])
		if !valid || id <= lastID || row["task_id"] != strconv.FormatInt(snapshot.TaskID, 10) {
			return nil, errors.New("streamed LLM record has invalid ID, order or task")
		}
		_, _ = ids.Write(binary.BigEndian.AppendUint64(nil, uint64(id)))
		lastID = id
		count++
		if count > expected {
			return nil, fmt.Errorf("LLM record count exceeds manifest %d", expected)
		}
		if count <= committed {
			if count == committed && id != lastCommittedID {
				return nil, errors.New("LLM restore checkpoint does not match package")
			}
			continue
		}
		batch = append(batch, row)
		// Large request/response strings dominate record size. Counting all text
		// fields bounds buffering without serializing another copy of each body.
		for _, value := range row {
			if text, ok := value.(string); ok {
				batchBytes += len(text)
			}
		}
		if len(batch) >= archiveLLMBatchRows || batchBytes >= archiveLLMBatchBytes {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if count != expected {
		return nil, fmt.Errorf("LLM record count %d does not match manifest %d", count, expected)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return ids.Sum(nil), nil
}

// Check actual IDs as well as the persisted checkpoint before making the task
// live. Scan only the covering task/id index, never the large request bodies.
// Holding the final write transaction closes the check-to-commit deletion race.
func verifyArchiveLLMRecords(tx *sql.Tx, taskID, expected int64, expectedIDs []byte) error {
	rows, err := tx.Query(`SELECT id FROM llm_records WHERE task_id=$1 ORDER BY id`, strconv.FormatInt(taskID, 10))
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := sha256.New()
	var count int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		_, _ = ids.Write(binary.BigEndian.AppendUint64(nil, uint64(id)))
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != expected || !bytes.Equal(ids.Sum(nil), expectedIDs) {
		return fmt.Errorf("LLM restore actual records do not match package: got %d rows, expected %d (IDs must also match)", count, expected)
	}
	return nil
}

func archiveLLMCheckpoint(q interface{ QueryRow(string, ...any) *sql.Row }, archiveID int64, snapshot *TaskArchiveSnapshot) (int64, int64, error) {
	var count, lastID int64
	err := q.QueryRow(`SELECT archive.restored_llm_records,archive.restored_llm_last_id
FROM task_archives archive JOIN tasks task ON task.id=archive.task_id
WHERE archive.id=$1 AND archive.state='restoring' AND task.id=$2 AND task.exploration_id=$3
AND task.deleted_at IS NOT NULL AND task.archived_at IS NOT NULL`, archiveID, snapshot.TaskID, snapshot.ExplorationID).Scan(&count, &lastID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrTaskArchiveState
	}
	return count, lastID, err
}

func (d *DB) restoreArchiveLLMBatch(archiveID int64, snapshot *TaskArchiveSnapshot, committed, lastID int64, rows []map[string]any) (int, error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	current, currentID, err := archiveLLMCheckpoint(tx, archiveID, snapshot)
	if err != nil {
		return 0, err
	}
	if current != committed || currentID != lastID {
		return 0, errors.New("concurrent LLM archive restore")
	}
	started := time.Now()
	n := 0
	for _, row := range rows {
		if _, err := insertJSONRow(tx, "llm_records", row, false); err != nil {
			return 0, err
		}
		n++
		lastID, _ = jsonInt64(row["id"])
		if time.Since(started) >= archiveLLMWriteSlice {
			break
		}
	}
	if _, err := tx.Exec(`UPDATE task_archives SET updated_at=now(),restored_llm_records=$2,restored_llm_last_id=$3 WHERE id=$1`, archiveID, committed+int64(n), lastID); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

package db

import (
	"fmt"
)

// ProveGoal atomically records the evidence edge and changes the goal state.
// Expected payloads prevent concurrent edits from silently changing what was checked.
func (s *ExplorationStore) ProveGoal(goal, evidence *Node, reason string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE exploration_nodes SET state='met',updated_at=now(),content_version=content_version+1
WHERE id=$1 AND exploration_id=$2 AND kind='goal' AND payload=$3 AND state IN ('open','met')
AND EXISTS (SELECT 1 FROM exploration_nodes e WHERE e.id=$4 AND e.payload=$5 AND e.state='confirmed'
  AND e.kind IN ('fact','finding')
  AND (e.exploration_id=$2 OR e.exploration_id IN (
    SELECT source.exploration_id FROM tasks owner JOIN task_relations r ON r.task_id=owner.id
    JOIN tasks source ON source.id=r.source_task_id AND source.deleted_at IS NULL
    WHERE owner.exploration_id=$2 AND owner.deleted_at IS NULL))
  AND (e.kind='fact' OR EXISTS (SELECT 1 FROM findings f WHERE f.node_id=e.id AND f.status='confirmed' AND length(trim(f.evidence))>0)))`, goal.ID, s.expID, string(goal.Payload), evidence.ID, string(evidence.Payload))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("goal or evidence changed; reload before proving")
	}
	if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, s.expID, evidence.ID, RelProves, goal.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ExplorationStore) GoalsMet() (bool, error) {
	var total, open int
	err := s.db.QueryRow(`SELECT count(*),count(*) FILTER (WHERE state <> 'met') FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal'`, s.expID).Scan(&total, &open)
	return total > 0 && open == 0, err
}

func (s *ExplorationStore) FindingConfirmed(nodeID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM findings WHERE node_id=$1 AND status='confirmed' AND length(trim(evidence))>0`, nodeID).Scan(&n)
	return n > 0, err
}

func (s *ExplorationStore) GoalEvidenceIDs(goalID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT src_id FROM exploration_edges WHERE exploration_id=$1 AND dst_id=$2 AND rel=$3`, s.expID, goalID, RelProves)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

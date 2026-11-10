package db

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestGoalProofRollsBackStateWhenEdgeFails(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	eid, _ := d.CreateExploration("fixture", "fixture")
	s := d.Exploration(eid)
	gid, _ := s.AddGoal(map[string]any{"text": "fixture"}, "human")
	fid, _ := s.AddNode(KindFact, map[string]any{"summary": "fixture"}, 0, "confirmed", "worker", nil)
	g, _ := s.GetNode(gid)
	f, _ := s.GetNode(fid)
	if _, err = d.Exec(fmt.Sprintf(`CREATE TRIGGER fail_proof BEFORE INSERT ON exploration_edges WHEN NEW.dst_id=%d BEGIN SELECT RAISE(ABORT,'fixture failure'); END`, gid)); err != nil {
		t.Fatal(err)
	}
	if err = s.ProveGoal(g, f, "fixture"); err == nil {
		t.Fatal("ignored edge write failure")
	}
	g, _ = s.GetNode(gid)
	if g.State != "open" {
		t.Fatal("goal state was committed without evidence")
	}
}

func TestGoalEditPreservesOrExplicitlyChangesCoverageAndInvalidatesProof(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "fixture.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	eid, _ := d.CreateExploration("fixture", "fixture")
	s := d.Exploration(eid)
	gid, _ := s.AddGoal(map[string]any{"text": "fixture", "coverage_min_pct": 95}, "human")
	fid, _ := s.AddNode(KindFact, map[string]any{"summary": "fixture"}, 0, "confirmed", "worker", nil)
	g, _ := s.GetNode(gid)
	f, _ := s.GetNode(fid)
	if err = s.ProveGoal(g, f, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateGoalPayload(gid, "revised", ""); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GetNode(gid)
	var p map[string]any
	json.Unmarshal(g.Payload, &p)
	ids, _ := s.GoalEvidenceIDs(gid)
	if g.State != "open" || p["coverage_min_pct"] != float64(95) || len(ids) != 0 {
		t.Fatal("edited goal retained stale proof or lost threshold", g, p, ids)
	}
	if err = s.UpdateGoalPayload(gid, "revised", "", nil); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GetNode(gid)
	p = nil
	json.Unmarshal(g.Payload, &p)
	if _, ok := p["coverage_min_pct"]; ok {
		t.Fatal("explicit removal ignored")
	}
}

package agent

import (
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	"testing"
)

func TestGoalProofCoverageAndShellGates(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	task, err := d.CreateTask("verifier", "fixture", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	ts := d.Exploration(task.ExplorationID)
	tools := NewToolSet(ts, "fixture")
	tools.SetTaskID(task.ID)
	tools.SetAssetStore(d.Assets(), d.Companies())
	a, err := d.Assets().UpsertIP(db.UpsertIPReq{IP: "192.0.2.27", TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Assets().AddAgentScope(task.ID, "ip", "192.0.2.27", "fixture", "manual"); err != nil {
		t.Fatal(err)
	}
	goal, err := ts.AddGoal(map[string]any{"text": "coverage", "coverage_min_pct": 100}, "human")
	if err != nil {
		t.Fatal(err)
	}
	fact, err := ts.AddNode(db.KindFact, map[string]any{"summary": "observed fixture"}, 0, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	prove := func(g, e int64) bool {
		r, err := tools.proveGoal().Call(t.Context(), json.RawMessage(fmt.Sprintf(`{"goal_id":%d,"evidence_id":%d}`, g, e)), nil)
		if err != nil {
			t.Fatal(err)
		}
		return !r.IsError
	}
	if prove(goal, fact) {
		t.Fatal("zero coverage proved 100 percent goal")
	}
	if err := ts.Anchor(fact, a); err != nil {
		t.Fatal(err)
	}
	if !prove(goal, fact) {
		t.Fatal("measured 100 percent coverage rejected")
	}
	shell, _ := ts.AddGoal(map[string]any{"text": "shell", "vulnclass": "getshell"}, "human")
	if prove(shell, fact) {
		t.Fatal("fact proved shell goal")
	}
	fn, _ := ts.AddNode(db.KindFinding, map[string]any{"vulnclass": "webshell", "evidence": "fixture interactive command and response"}, 0, "confirmed", "worker", nil)
	fid, err := d.AddFinding(task.ID, fn, "webshell", "fixture", "high", "fixture", "fixture interactive command and response", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if prove(shell, fn) {
		t.Fatal("pending finding proved shell goal")
	}
	if _, err := d.Exec(`UPDATE findings SET status='confirmed' WHERE id=$1`, fid); err != nil {
		t.Fatal(err)
	}
	if !prove(shell, fn) {
		t.Fatal("confirmed shell evidence rejected")
	}
	if met, err := tools.allGoalsVerified(); err != nil || !met {
		t.Fatal("valid proofs rejected", err)
	}
	if _, err := d.Exec(`UPDATE findings SET status='false_positive' WHERE id=$1`, fid); err != nil {
		t.Fatal(err)
	}
	if met, _ := tools.allGoalsVerified(); met {
		t.Fatal("stale proof accepted after triage changed")
	}
}

func TestWorkerFinalizerBoundedWriteback(t *testing.T) {
	f := &workerFinalizer{tools: NewToolSet(nil, "fixture")}
	if p, b, _ := f.Stop(t.Context(), nil); p || len(b) > 0 {
		t.Fatal("empty/non-probing worker blocked")
	}
	f.PostToolUse(t.Context(), "ExecuteExtraTool", []byte(`{"tool_name":"recipe__fixture"}`), nil, false)
	for range 2 {
		if p, b, _ := f.Stop(t.Context(), nil); p || len(b) == 0 {
			t.Fatal("missing writeback not resumed")
		}
	}
	if p, _, _ := f.Stop(t.Context(), nil); !p || !f.failed {
		t.Fatal("unbounded continuation")
	}
	f = &workerFinalizer{tools: NewToolSet(nil, "fixture")}
	f.probed.Store(true)
	f.tools.writes.Facts = 1
	if p, b, _ := f.Stop(t.Context(), nil); p || len(b) > 0 {
		t.Fatal("written result blocked")
	}
}

func TestSDKTranscriptRepairKeepsPairsAndDoesNotFabricateSuccess(t *testing.T) {
	msgs := []llm.Message{{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockToolUse, ID: "done", Name: "fixture"}, {Type: llm.BlockToolUse, ID: "interrupted", Name: "fixture"}}}, {Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("done", "actual output", false), llm.ToolResultText("orphan", "orphan output", false)}}}
	out := llm.MessagesForAPI(msgs)
	if len(out) != 2 || len(out[0].ToolUses()) != 1 || out[0].ToolUses()[0].ID != "done" || len(out[1].Content) != 1 {
		t.Fatalf("bad repaired transcript: %+v", out)
	}
	if len(msgs[0].ToolUses()) != 2 || len(msgs[1].Content) != 2 {
		t.Fatal("audit history was mutated")
	}
}

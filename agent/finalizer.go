package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/toolrun"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

func (t *ToolSet) verifyGoal(goal, ev *db.Node) error {
	if ev.State != "confirmed" {
		return fmt.Errorf("evidence must be confirmed; task roots and dismissed nodes are not proof")
	}
	if ev.Kind == db.KindFinding {
		confirmed, err := t.ts.FindingConfirmed(ev.ID)
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("finding evidence must have confirmed status and recorded evidence")
		}
	}
	var g struct {
		VulnClass string   `json:"vulnclass"`
		Coverage  *float64 `json:"coverage_min_pct"`
	}
	if err := json.Unmarshal(goal.Payload, &g); err != nil {
		return fmt.Errorf("invalid goal payload: %w", err)
	}
	if g.Coverage != nil {
		if *g.Coverage < 0 || *g.Coverage > 100 {
			return fmt.Errorf("coverage_min_pct must be between 0 and 100")
		}
		if t.as == nil || t.taskID <= 0 || t.coverageDisabled || !t.as.CoverageEnabled(t.taskID) {
			return fmt.Errorf("coverage verification unavailable or disabled")
		}
		cov, err := t.as.TaskCoverageWithSources(t.taskID)
		if err != nil {
			return fmt.Errorf("coverage lookup: %w", err)
		}
		if cov.Pct == nil {
			return fmt.Errorf("coverage scope has no assets; cannot verify")
		}
		if *cov.Pct*100+1e-9 < *g.Coverage {
			return fmt.Errorf("asset coverage %.2f%% is below required %.2f%%", *cov.Pct*100, *g.Coverage)
		}
	}
	if shellClass(g.VulnClass) {
		var e struct {
			VulnClass string `json:"vulnclass"`
			Evidence  string `json:"evidence"`
		}
		if json.Unmarshal(ev.Payload, &e) != nil || ev.Kind != db.KindFinding || !shellClass(e.VulnClass) || strings.TrimSpace(e.Evidence) == "" {
			return fmt.Errorf("shell goal requires a shell finding with recorded interactive evidence; a fact, RCE or upload alone is insufficient")
		}
	}
	return nil
}

func shellClass(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "getshell", "webshell", "reverse_shell":
		return true
	}
	return false
}

// Recheck proofs at whole-task completion: scope, direct-source links or finding
// triage can change after an earlier goal was marked met.
func (t *ToolSet) allGoalsVerified() (bool, error) {
	met, err := t.ts.GoalsMet()
	if err != nil || !met {
		return false, err
	}
	goals, err := t.ts.ListByKind(db.KindGoal, 10000)
	if err != nil {
		return false, err
	}
	for _, goal := range goals {
		ids, err := t.ts.GoalEvidenceIDs(goal.ID)
		if err != nil {
			return false, err
		}
		valid := false
		for _, id := range ids {
			ev, err := t.ts.GetNodeWithSources(id)
			if err != nil {
				return false, err
			}
			if ev != nil && (ev.Kind == db.KindFact || ev.Kind == db.KindFinding) && t.verifyGoal(goal, ev) == nil {
				valid = true
				break
			}
		}
		if !valid {
			return false, nil
		}
	}
	return len(goals) > 0, nil
}

// A stop check stays inside the SDK loop, so MaxTurns/MaxDuration still apply.
// Never initiate new probes just to satisfy a writeback check.
type workerFinalizer struct {
	base    harness.HookRunner
	tools   *ToolSet
	runs    *toolrun.Manager
	probed  atomic.Bool
	retries int
	failed  bool
}

func (f *workerFinalizer) PreToolUse(ctx context.Context, n string, in []byte) (bool, string, []byte) {
	if f.base != nil {
		return f.base.PreToolUse(ctx, n, in)
	}
	return false, "", nil
}
func (f *workerFinalizer) PostToolUse(ctx context.Context, n string, in, out []byte, isErr bool) {
	if n == "ExecuteExtraTool" {
		var call struct {
			Name string `json:"tool_name"`
		}
		if json.Unmarshal(in, &call) == nil && (strings.HasPrefix(call.Name, "mcp__") || strings.HasPrefix(call.Name, "recipe__")) {
			f.probed.Store(true)
		}
	}
	if n == "Bash" || n == "WebFetch" || strings.HasPrefix(n, "mcp__") || strings.HasPrefix(n, "recipe__") {
		f.probed.Store(true)
	}
	if f.base != nil {
		f.base.PostToolUse(ctx, n, in, out, isErr)
	}
}
func (f *workerFinalizer) Stop(ctx context.Context, msgs []llm.Message) (bool, []string, string) {
	if f.base != nil {
		p, e, m := f.base.Stop(ctx, msgs)
		if p || len(e) > 0 {
			return p, e, m
		}
	}
	if ctx.Err() != nil {
		return false, nil, ""
	}
	pending := f.runs != nil && f.runs.Pending() > 0
	missing := f.probed.Load() && f.tools.Writes().Total() == 0
	if !pending && !missing {
		return false, nil, ""
	}
	if f.retries >= 2 {
		f.failed = true
		return true, nil, "终态验收未通过：后台工具未收束或探测结论未写回"
	}
	f.retries++
	return false, []string{"结束前请 wait/cancel 尚在运行的工具；把已有结果或失败原因用 record_fact 写回图谱。不要为凑证据发起新探测，不要编造发现。"}, ""
}

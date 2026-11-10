// Package toolrun owns long-running remote/recipe calls for one agent session.
package toolrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/config"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/google/uuid"
)

type execution struct {
	id, tool, status string
	result           actool.Result
	cancel           context.CancelFunc
	done             chan struct{}
}

type Manager struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	runs   map[string]*execution
	shell  *actool.Manager
	opts   config.Runtime
	wg     sync.WaitGroup
	sem    chan struct{}
}

func New(ctx context.Context) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: ctx, cancel: cancel, runs: map[string]*execution{}, opts: config.RuntimeOptions(), sem: make(chan struct{}, 5)}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func (m *Manager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.runs {
		if r.status == "running" {
			n++
		}
	}
	if m.shell != nil {
		for _, t := range m.shell.List() {
			if t.Kind == actool.KindBash && !t.Status.Terminal() {
				n++
			}
		}
	}
	return n
}

func (m *Manager) bindShell(tc *actool.ToolContext) {
	if tc == nil || tc.Tasks == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shell != nil {
		return
	}
	m.shell = tc.Tasks
	if m.opts.NoOutputSeconds > 0 {
		go m.watchShell(tc.Tasks)
	}
}

func (m *Manager) watchShell(shell *actool.Manager) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-ticker.C:
			for _, task := range shell.List() {
				if task.Status.Terminal() || task.Kind != actool.KindBash {
					continue
				}
				last := task.StartTime
				if st, err := os.Stat(task.OutputPath); err == nil && st.ModTime().After(last) {
					last = st.ModTime()
				}
				if now.Sub(last) > time.Duration(m.opts.NoOutputSeconds)*time.Second {
					shell.Kill(task.ID)
				}
			}
		}
	}
}

type wrapped struct {
	actool.CoreTool
	manager *Manager
	async   bool
}

func (m *Manager) Wrap(t actool.CoreTool) actool.CoreTool {
	return &wrapped{CoreTool: t, manager: m, async: strings.HasPrefix(t.Name(), "mcp__") || strings.HasPrefix(t.Name(), "recipe__")}
}

func (t *wrapped) Call(ctx context.Context, input json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
	m := t.manager
	m.bindShell(tc)
	if !t.async {
		return t.CoreTool.Call(ctx, input, tc)
	}
	if err := ctx.Err(); err != nil {
		return actool.Errorf(err.Error()), nil
	}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return actool.Errorf("tool session closed"), nil
	}
	// Bound retained executions, including completed results. IDs remain readable
	// for the session, so completion never races eviction.
	if len(m.runs) >= 128 {
		m.mu.Unlock()
		return actool.Errorf("session execution limit reached (128)"), nil
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(m.opts.ToolTimeoutSeconds)*time.Second)
	stop := context.AfterFunc(m.ctx, cancel)
	r := &execution{id: uuid.NewString(), tool: t.Name(), status: "running", cancel: cancel, done: make(chan struct{})}
	m.runs[r.id] = r
	m.wg.Add(1)
	m.mu.Unlock()
	input = append(json.RawMessage(nil), input...)
	if tc != nil {
		copyTC := *tc
		tc = &copyTC
	}
	go func() {
		defer m.wg.Done()
		defer cancel()
		defer stop()
		res, err := actool.Result{}, runCtx.Err()
		select {
		case m.sem <- struct{}{}:
			res, err = t.CoreTool.Call(runCtx, input, tc)
			<-m.sem
		case <-runCtx.Done():
			err = runCtx.Err()
		}
		if err != nil {
			res = actool.Errorf(err.Error())
		}
		m.mu.Lock()
		r.status = "completed"
		if res.IsError {
			r.status = "failed"
		}
		if runCtx.Err() != nil {
			r.status = "cancelled"
			res = actool.Errorf(res.Flatten() + "\n" + runCtx.Err().Error())
		}
		r.result = res
		close(r.done)
		m.mu.Unlock()
	}()
	timer := time.NewTimer(time.Duration(m.opts.ToolWaitSeconds) * time.Second)
	defer timer.Stop()
	select {
	case <-r.done:
		return m.result(r), nil
	case <-ctx.Done():
		cancel()
		return actool.Errorf(ctx.Err().Error()), nil
	case <-timer.C:
		return m.result(r), nil
	}
}

func (m *Manager) result(r *execution) actool.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.status != "running" {
		return r.result
	}
	b, _ := json.Marshal(map[string]any{"status": r.status, "execution_id": r.id, "tool": r.tool, "note": "Use wait_tool_execution or cancel_tool_execution. Remote cancellation is best effort; do not repeat a side-effecting call."})
	return actool.Text(string(b))
}

func (m *Manager) Tools() []actool.CoreTool {
	var out []actool.CoreTool
	for _, name := range []string{"get_tool_execution", "wait_tool_execution", "cancel_tool_execution"} {
		out = append(out, actool.Build(actool.Spec{
			Name: name, Description: "Inspect, wait (up to 60 seconds), or cancel a background MCP/recipe call in this session. Cancellation of remote work is best effort.",
			Schema: map[string]any{"type": "object", "properties": map[string]any{"execution_id": map[string]any{"type": "string"}, "wait_seconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 60}}, "required": []string{"execution_id"}},
			Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
				return permission.Allowed()
			},
			Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
				var a struct {
					ID   string `json:"execution_id"`
					Wait int    `json:"wait_seconds"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return actool.Errorf(err.Error()), nil
				}
				m.mu.Lock()
				r := m.runs[a.ID]
				m.mu.Unlock()
				if r == nil {
					return actool.Errorf("execution_id not found in this session"), nil
				}
				if name == "cancel_tool_execution" {
					r.cancel()
					return actool.Text("Cancellation requested; remote server work may still be running."), nil
				}
				if name == "wait_tool_execution" {
					if a.Wait <= 0 {
						a.Wait = 10
					}
					if a.Wait > 60 {
						a.Wait = 60
					}
					timer := time.NewTimer(time.Duration(a.Wait) * time.Second)
					defer timer.Stop()
					select {
					case <-r.done:
					case <-ctx.Done():
						return actool.Errorf(ctx.Err().Error()), nil
					case <-timer.C:
					}
				}
				return m.result(r), nil
			},
		}))
	}
	return out
}

func (m *Manager) String() string { return fmt.Sprintf("%d pending tool executions", m.Pending()) }

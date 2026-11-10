package toolrun

import (
	"context"
	"encoding/json"
	"fmt"
	actool "github.com/Autumn-27/norma/tool"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAsyncExecutionOwnershipAndCancellation(t *testing.T) {
	m := New(t.Context())
	defer m.Close()
	m.opts.ToolWaitSeconds = 0
	stopped := make(chan struct{})
	tool := m.Wrap(actool.Build(actool.Spec{Name: "mcp__test__wait", Run: func(ctx context.Context, _ json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
		<-ctx.Done()
		close(stopped)
		return actool.Errorf(ctx.Err().Error()), nil
	}}))
	r, err := tool.Call(t.Context(), json.RawMessage(`{}`), nil)
	if err != nil || r.IsError {
		t.Fatal(r, err)
	}
	var status struct {
		ID string `json:"execution_id"`
	}
	if json.Unmarshal([]byte(r.Flatten()), &status) != nil || status.ID == "" {
		t.Fatal(r.Flatten())
	}
	other := New(t.Context())
	defer other.Close()
	in, _ := json.Marshal(map[string]string{"execution_id": status.ID})
	res, _ := other.Tools()[0].Call(t.Context(), in, nil)
	if !res.IsError {
		t.Fatal("cross-session execution readable")
	}
	m.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("execution leaked after session close")
	}
}

func TestRecipeProcessUsesLiteralArgv(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	arg := `literal $(echo injected); & space`
	r, err := RunCommand(t.Context(), exe, []string{"-test.run=TestRecipeProcessHelper", "--", arg}, &actool.ToolContext{WorkingDir: t.TempDir(), Env: []string{"ARTEX_RECIPE_HELPER=1"}}, 0)
	if err != nil || r.IsError || !strings.Contains(r.Flatten(), arg) {
		t.Fatalf("%v %s", err, r.Flatten())
	}
}
func TestRecipeProcessHelper(t *testing.T) {
	if os.Getenv("ARTEX_RECIPE_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "child-heartbeat" {
		for {
			os.WriteFile("heartbeat", []byte(time.Now().String()), 0600)
			time.Sleep(20 * time.Millisecond)
		}
	}
	if mode == "spawn-child" {
		cmd := exec.Command(os.Args[0], "-test.run=TestRecipeProcessHelper", "--", "child-heartbeat")
		if err := cmd.Start(); err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "quiet" {
		for {
			time.Sleep(time.Second)
		}
	}
	fmt.Print(mode)
	os.Exit(0)
}

func TestRecipeCancellationStopsDescendants(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan actool.Result, 1)
	go func() {
		r, _ := RunCommand(ctx, exe, []string{"-test.run=TestRecipeProcessHelper", "--", "spawn-child"}, &actool.ToolContext{WorkingDir: dir, Env: []string{"ARTEX_RECIPE_HELPER=1"}}, 0)
		done <- r
	}()
	path := filepath.Join(dir, "heartbeat")
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		select {
		case <-deadline.C:
			t.Fatal("child never started")
		case <-tick.C:
			if _, e := os.Stat(path); e == nil {
				break waiting
			}
		}
	}
	cancel()
	select {
	case r := <-done:
		if !r.IsError {
			t.Fatal("cancelled process returned success")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("process did not stop")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("descendant survived cancellation", err)
	}
}

func TestRecipeIdleWatchdog(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	r, e := RunCommand(ctx, exe, []string{"-test.run=TestRecipeProcessHelper", "--", "quiet"}, &actool.ToolContext{WorkingDir: t.TempDir(), Env: []string{"ARTEX_RECIPE_HELPER=1"}}, 50*time.Millisecond)
	if e != nil || !r.IsError || ctx.Err() != nil {
		t.Fatalf("idle watchdog failed: %v %s", e, r.Flatten())
	}
}

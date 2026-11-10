package toolrun

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

type outputWriter struct {
	mu     sync.Mutex
	file   *os.File
	size   int
	last   time.Time
	cancel context.CancelFunc
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.last = time.Now()
	if w.size+len(p) > 32<<20 {
		w.cancel()
		return 0, fmt.Errorf("tool output exceeds 32 MiB")
	}
	n, err := w.file.Write(p)
	w.size += n
	return n, err
}

// RunCommand always uses an argv vector. No shell, interpolation or eval.
func RunCommand(ctx context.Context, command string, args []string, tc *actool.ToolContext, idle time.Duration) (actool.Result, error) {
	if tc == nil || tc.WorkingDir == "" {
		return actool.Errorf("recipe requires a session working directory"), nil
	}
	dir := tc.OutputDir
	if dir == "" {
		dir = filepath.Join(tc.WorkingDir, "cmd-output")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return actool.Errorf(err.Error()), nil
	}
	f, err := os.CreateTemp(dir, "recipe-*.log")
	if err != nil {
		return actool.Errorf(err.Error()), nil
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := &outputWriter{file: f, last: time.Now(), cancel: cancel}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = tc.WorkingDir
	cmd.Env = append(os.Environ(), tc.Env...)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.WaitDelay = 2 * time.Second
	prepareProcess(cmd)
	if err := cmd.Start(); err != nil {
		return actool.Errorf(err.Error()), nil
	}
	cleanup, err := containProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return actool.Errorf("process isolation: " + err.Error()), nil
	}
	defer cleanup()
	stop := context.AfterFunc(ctx, func() { killProcess(cmd) })
	defer stop()
	finished := make(chan struct{})
	if idle > 0 {
		go func() {
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for {
				select {
				case <-finished:
					return
				case <-ctx.Done():
					return
				case <-timer.C:
					w.mu.Lock()
					quiet := time.Since(w.last) > idle
					w.mu.Unlock()
					if quiet {
						cancel()
						return
					}
				}
			}
		}()
	}
	err = cmd.Wait()
	close(finished)
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return actool.Errorf(e.Error()), nil
	}
	// Full output stays in the private file. Only a bounded preview reaches memory.
	preview, e := io.ReadAll(io.LimitReader(f, 30000))
	if e != nil {
		return actool.Errorf(e.Error()), nil
	}
	text := strings.ToValidUTF8(string(preview), "�")
	if w.size > len(preview) {
		text += fmt.Sprintf("\n<persisted-output>Full output: %s (%d bytes)</persisted-output>", f.Name(), w.size)
	}
	if err != nil {
		text += "\nExecution failed: " + err.Error() + "\nOutput file: " + f.Name()
	}
	r := actool.Text(text)
	r.IsError = err != nil
	return r, nil
}

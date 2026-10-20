package server

import (
	"context"
	"os"
	"testing"

	"github.com/Autumn-27/artex/internal/testdb"
)

func TestMain(m *testing.M) { os.Exit(testdb.Run(m)) }

// Register manager cleanup first; this cleanup then cancels and joins background
// work before the store and temporary files are closed by earlier cleanups.
func newTestServer(t *testing.T, parent context.Context, m *Manager, skills, data, keys string) *Server {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	s := New(ctx, m, skills, data, keys)
	t.Cleanup(func() {
		cancel()
		s.backgroundWG.Wait()
		s.archiveWG.Wait()
		for _, task := range m.List() {
			s.engine.StopTask(task.ID)
		}
	})
	return s
}

package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
)

func newTaskRevivalFixture(t *testing.T, status string, cached bool) (*Server, *Task) {
	t.Helper()
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	t.Cleanup(restoreConcurrencySetting(t, m))
	if err := m.SetConcurrency(false, defaultConcurrencyLimit); err != nil {
		t.Fatal(err)
	}
	task, err := m.CreateTask("terminal follow-up", "fresh execution budget", nil, 120, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := newAdmissionTestServer(m, nil)
	t.Cleanup(func() {
		s.engine.StopTask(task.ID)
		_, _ = m.DeleteTask(task.ID, DeleteTaskOptions{})
	})
	id := mustTaskID(t, task.ID)
	if _, err := m.pg.Exec(`UPDATE tasks SET first_run_at=time_add(now(), -240), deadline_at=time_add(now(), -120) WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	stored, err := m.pg.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	task.updateLifecycle(func(state *taskLifecycleState) {
		state.FirstRunAt = stored.FirstRunAt.Unix()
		state.DeadlineAt = stored.DeadlineAt.Unix()
	})
	if err := m.SetTaskStatus(task.ID, status); err != nil {
		t.Fatal(err)
	}
	if cached {
		s.engine.stamped.Store(task.ID, true)
		s.engine.deadline.Store(task.ID, stored.DeadlineAt.Unix())
	}
	return s, task
}

func assertTaskClockNotStarted(t *testing.T, s *Server, task *Task) {
	t.Helper()
	state := task.lifecycleSnapshot()
	if state.FirstRunAt != 0 || state.DeadlineAt != 0 || s.engine.taskDeadline(task) != 0 {
		t.Fatalf("clock started before execution: %+v", state)
	}
	if _, ok := s.engine.stamped.Load(task.ID); ok {
		t.Fatal("first-run cache was retained before execution")
	}
	stored, err := s.m.pg.GetTask(mustTaskID(t, task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.FirstRunAt != nil || stored.DeadlineAt != nil || stored.TimeoutSeconds != 120 {
		t.Fatalf("persisted clock/config: %+v", stored)
	}
}

func TestTerminalTaskRevivalStartsBudgetOnExecution(t *testing.T) {
	for _, status := range []string{"done", "failed", "timeout"} {
		for _, queued := range []bool{false, true} {
			for _, cached := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/queued=%v/cached=%v", status, queued, cached), func(t *testing.T) {
					s, task := newTaskRevivalFixture(t, status, cached)
					s.engine.settling.Store(task.ID, true)
					var holder *Task
					if queued {
						if err := s.m.SetConcurrency(true, 1); err != nil {
							t.Fatal(err)
						}
						var err error
						holder, err = s.m.CreateTask("slot holder", "hold", nil, 0, 0)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _, _ = s.m.DeleteTask(holder.ID, DeleteTaskOptions{}) })
					}
					gotQueued, err := s.admitTask(task, "resume")
					if err != nil || gotQueued != queued {
						t.Fatalf("admission: queued=%v want=%v err=%v", gotQueued, queued, err)
					}
					if state := task.lifecycleSnapshot(); state.Status != "running" || s.engine.isSettling(task.ID) {
						t.Fatalf("task did not leave terminal state: %+v", state)
					}
					assertTaskClockNotStarted(t, s, task)
					if queued {
						// A late call from the cancelled run cannot stamp while in FIFO.
						s.engine.stampFirstRun(task)
						if stillQueued, err := s.admitTask(task, "resume"); err != nil || !stillQueued {
							t.Fatalf("repeat queued admission: queued=%v err=%v", stillQueued, err)
						}
						assertTaskClockNotStarted(t, s, task)
						s.engine.Pause(holder.ID, agent.AbortPausedByUser)
						if err := s.m.ApplyTaskPause(holder.ID); err != nil {
							t.Fatal(err)
						}
						s.reconcileConcurrency()
						if task.lifecycleSnapshot().Queued || s.engine.IsPaused(task.ID) {
							t.Fatal("task did not leave FIFO")
						}
						assertTaskClockNotStarted(t, s, task)
					}
					startedAt := time.Now().Unix()
					s.engine.stampFirstRun(task)
					clock := task.lifecycleSnapshot()
					if clock.FirstRunAt < startedAt || clock.DeadlineAt < startedAt+119 || s.engine.taskDeadline(task) != clock.DeadlineAt {
						t.Fatalf("execution did not receive a fresh 120s budget: %+v", clock)
					}
					// Follow-up work and pause/resume preserve the current run's budget.
					if _, err := s.admitTask(task, "resume"); err != nil {
						t.Fatal(err)
					}
					s.engine.Pause(task.ID, agent.AbortPausedByUser)
					if err := s.m.ApplyTaskPause(task.ID); err != nil {
						t.Fatal(err)
					}
					if _, err := s.admitPausedTask(task); err != nil {
						t.Fatal(err)
					}
					s.engine.stampFirstRun(task)
					if after := task.lifecycleSnapshot(); after.FirstRunAt != clock.FirstRunAt || after.DeadlineAt != clock.DeadlineAt {
						t.Fatalf("active run's budget changed: before=%+v after=%+v", clock, after)
					}
				})
			}
		}
	}
}

func TestTerminalTaskRevivalClockResetIsAtomic(t *testing.T) {
	for _, status := range []string{"done", "failed", "timeout"} {
		t.Run(status, func(t *testing.T) {
			s, task := newTaskRevivalFixture(t, status, true)
			before := task.lifecycleSnapshot()
			id := mustTaskID(t, task.ID)
			trigger := fmt.Sprintf("reject_revival_%d", id)
			if _, err := s.m.pg.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE OF first_run_at ON tasks
FOR EACH ROW WHEN OLD.id=%d AND OLD.status IN ('done','failed','timeout') AND NEW.status='running'
BEGIN SELECT RAISE(ABORT,'clock reset failed'); END`, trigger, id)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = s.m.pg.Exec("DROP TRIGGER IF EXISTS " + trigger) })
			if _, err := s.admitTask(task, "resume"); err == nil {
				t.Fatal("admission succeeded despite clock reset failure")
			}
			after := task.lifecycleSnapshot()
			stored, err := s.m.pg.GetTask(id)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != status || after.FirstRunAt != before.FirstRunAt || after.DeadlineAt != before.DeadlineAt ||
				stored.Status != status || stored.FirstRunAt.Unix() != before.FirstRunAt || stored.DeadlineAt.Unix() != before.DeadlineAt ||
				s.engine.taskDeadline(task) != before.DeadlineAt || s.engine.Started(task.ID) {
				t.Fatalf("failed admission changed task/clock: before=%+v after=%+v stored=%+v", before, after, stored)
			}
			if _, err := s.m.pg.Exec("DROP TRIGGER " + trigger); err != nil {
				t.Fatal(err)
			}
			if _, err := s.admitTask(task, "resume"); err != nil {
				t.Fatal(err)
			}
			assertTaskClockNotStarted(t, s, task)
		})
	}
}

func TestTerminalTaskRevivalStopsPreviousCoordinator(t *testing.T) {
	for _, settling := range []bool{false, true} {
		t.Run(fmt.Sprintf("settling=%v", settling), func(t *testing.T) {
			s, task := newTaskRevivalFixture(t, "running", true)
			// Keep planner/worker loops idle; only the real deadline coordinator runs.
			s.engine.SetAuthoritativeAgentResolver(func(*Task) (*agent.Planner, *agent.Worker) { return nil, nil })
			s.engine.started.Store(task.ID, true)
			if !settling {
				s.engine.deadline.Store(task.ID, time.Now().Add(time.Hour).Unix())
			}
			s.engine.startDeadlineCoordinator(context.Background(), task)
			value, ok := s.engine.coordStarted.Load(task.ID)
			if !ok {
				t.Fatal("coordinator did not start")
			}
			previous := value.(*deadlineCoordinatorRun)
			if settling {
				until := time.Now().Add(3 * time.Second)
				for !s.engine.isSettling(task.ID) && time.Now().Before(until) {
					time.Sleep(time.Millisecond)
				}
				if !s.engine.isSettling(task.ID) {
					t.Fatal("coordinator did not enter settlement")
				}
			}
			if err := s.m.SetTaskStatus(task.ID, "done"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.admitTask(task, "resume"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-previous.done:
			default:
				t.Fatal("previous coordinator still running after revival")
			}
			if state := task.lifecycleSnapshot(); state.Status != "running" || s.engine.isSettling(task.ID) {
				t.Fatalf("previous settlement affected new run: %+v", state)
			}
			assertTaskClockNotStarted(t, s, task)
			current, ok := s.engine.coordStarted.Load(task.ID)
			if !ok || current == previous {
				t.Fatal("new run has no fresh coordinator")
			}
			s.engine.startDeadlineCoordinator(context.Background(), task)
			if next, _ := s.engine.coordStarted.Load(task.ID); next != current {
				t.Fatal("repeated start replaced the current coordinator")
			}
		})
	}
}

func TestTerminalTaskRevivalWithoutTimeout(t *testing.T) {
	s, task := newTaskRevivalFixture(t, "done", true)
	task.TimeoutSeconds = 0
	if _, err := s.m.pg.Exec(`UPDATE tasks SET timeout_seconds=0 WHERE id=$1`, mustTaskID(t, task.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.admitTask(task, "resume"); err != nil {
		t.Fatal(err)
	}
	s.engine.stampFirstRun(task)
	state := task.lifecycleSnapshot()
	if state.FirstRunAt != 0 || state.DeadlineAt != 0 || s.engine.taskDeadline(task) != 0 {
		t.Fatalf("unlimited task retained a clock: %+v", state)
	}
	if _, ok := s.engine.coordStarted.Load(task.ID); ok {
		t.Fatal("unlimited task started a coordinator")
	}
}

func TestRunningTaskAdmissionPreservesExpiredBudget(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprintf("paused=%v", paused), func(t *testing.T) {
			s, task := newTaskRevivalFixture(t, "running", true)
			before := task.lifecycleSnapshot()
			// An existing run keeps its elapsed budget even when more work arrives
			// or it is resumed after a pause. Only terminal revival starts a new run.
			if paused {
				s.engine.Pause(task.ID, agent.AbortPausedByUser)
				if err := s.m.ApplyTaskPause(task.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.admitPausedTask(task); err != nil {
					t.Fatal(err)
				}
			} else if _, err := s.admitTask(task, "resume"); err != nil {
				t.Fatal(err)
			}
			s.engine.stampFirstRun(task)
			after := task.lifecycleSnapshot()
			if after.FirstRunAt != before.FirstRunAt || after.DeadlineAt != before.DeadlineAt || s.engine.taskDeadline(task) != before.DeadlineAt {
				t.Fatalf("existing budget changed: before=%+v after=%+v", before, after)
			}
		})
	}
}

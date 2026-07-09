package transfer

import (
	"context"
	"log"
)

// DefaultMaxConcurrentTasks is the default number of transfer tasks run at
// once (FR-QUEUE-004: "default: 2 параллельных передачи").
// NewTransferService always sets TransferService.maxConcurrentTasks to
// this - there is no constructor parameter to override it yet (a future
// Settings screen, per the Этап 3 plan's constraint 7 precedent for the
// bandwidth limiter, is expected to add one without changing this
// default).
const DefaultMaxConcurrentTasks = 2

// dispatch starts as many Pending tasks as there are free concurrency slots
// (s.maxConcurrentTasks - len(s.running)). It is not a polling loop: per
// the Этап 3 plan's architecture decision, it is called opportunistically -
// from QueueUpload/QueueDownload/ResumeTask/RetryTask right after a task
// becomes runnable, and from runTask itself as soon as a slot frees up -
// never on a timer.
//
// Race safety: the Pending task list is read from SQLite (s.queueRepo.
// GetAll, already ordered by priority/created_at - FR-QUEUE-003) OUTSIDE
// s.mu, since a plain read does not need to coordinate with s.running at
// all. Then s.mu is taken ONCE for the entire admission loop: checking
// len(s.running) against the concurrency limit and calling startTask
// (which inserts into s.running) happen inside the very same critical
// section, for every task considered - so two goroutines calling dispatch()
// concurrently (e.g. one from QueueUpload racing with one from a task that
// just finished) can never both observe "one free slot" and both start a
// task into it: whichever acquires s.mu first fully re-evaluates and
// updates s.running before the other is allowed to look at it (classic
// check-then-act TOCTOU, closed by moving both the check and the act inside
// one lock instead of two separate ones).
func (s *TransferService) dispatch() {
	tasks, err := s.queueRepo.GetAll(context.Background())
	if err != nil {
		log.Printf("transfer: dispatch: list tasks: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range tasks {
		if len(s.running) >= s.maxConcurrentTasks {
			return
		}

		if task.Status != "pending" {
			continue
		}

		if _, alreadyRunning := s.running[task.ID]; alreadyRunning {
			// Defensive only: a task that is actually running always has
			// status "running" (set at the very start of runTask), never
			// "pending", so this should be unreachable in practice - kept
			// so a future change to that invariant cannot silently start
			// the same task twice.
			continue
		}

		s.startTask(task)
	}
}

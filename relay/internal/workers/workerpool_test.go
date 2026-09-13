package workers

import (
	"sync/atomic"
	"testing"
)

func TestWorkerPoolRunsQueuedJobs(t *testing.T) {
	pool := NewWorkerPool(2, 4)
	var completed atomic.Int32
	for i := 0; i < 4; i++ {
		if !pool.AddJob(func() { completed.Add(1) }) {
			t.Fatalf("job %d was unexpectedly rejected", i)
		}
	}
	pool.Wait()
	pool.Stop()
	if got := completed.Load(); got != 4 {
		t.Fatalf("completed jobs = %d, want 4", got)
	}
}

func TestWorkerPoolRejectsJobsWhenQueueIsFull(t *testing.T) {
	pool := NewWorkerPool(1, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	if !pool.AddJob(func() {
		close(started)
		<-release
	}) {
		t.Fatal("first job was unexpectedly rejected")
	}
	<-started
	if !pool.AddJob(func() {}) {
		t.Fatal("second job was unexpectedly rejected while the worker was active")
	}
	if pool.AddJob(func() {}) {
		t.Fatal("third job was accepted despite a full queue")
	}
	close(release)
	pool.Wait()
	pool.Stop()
}

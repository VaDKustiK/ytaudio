package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueRunsOneTaskAtATimeAndCaps(t *testing.T) {
	q := New(2)
	defer q.Close()
	var active int32
	var max int32
	task := func(context.Context) error {
		current := atomic.AddInt32(&active, 1)
		if current > atomic.LoadInt32(&max) {
			atomic.StoreInt32(&max, current)
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return nil
	}
	first, err := q.Submit("1", task)
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Submit("2", task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Submit("3", task); err == nil {
		t.Fatal("expected daily cap error")
	}
	<-first.Done
	<-second.Done
	if atomic.LoadInt32(&max) != 1 {
		t.Fatalf("max active = %d", max)
	}
}

func TestActiveTaskReservesCapacity(t *testing.T) {
	q := New(1)
	defer q.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	job, err := q.Submit("1", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err = q.Submit("2", func(context.Context) error { return nil }); err == nil {
		t.Fatal("active job did not reserve capacity")
	}
	close(release)
	<-job.Done
}
func TestCancelledPendingTaskDoesNotExecute(t *testing.T) {
	q := New(10)
	defer q.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	first, _ := q.Submit("1", func(context.Context) error { close(started); <-release; return nil })
	<-started
	var ran atomic.Bool
	next, _ := q.Submit("2", func(context.Context) error { ran.Store(true); return nil })
	next.Cancel()
	close(release)
	<-first.Done
	if err := <-next.Done; err != context.Canceled || ran.Load() {
		t.Fatalf("cancelled pending task ran: %v", err)
	}
}
func TestCloseWaitsForCancellation(t *testing.T) {
	q := New(10)
	started := make(chan struct{})
	finished := make(chan struct{})
	q.Submit("1", func(ctx context.Context) error { close(started); <-ctx.Done(); close(finished); return ctx.Err() })
	<-started
	q.Close()
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before task stopped")
	}
	q.Close()
}

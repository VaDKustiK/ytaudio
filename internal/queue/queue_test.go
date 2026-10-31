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

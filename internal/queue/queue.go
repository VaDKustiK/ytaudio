package queue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Task func(context.Context) error

type Job struct {
	ID     string
	Task   Task
	Cancel context.CancelFunc
	Done   chan error
	ctx    context.Context
}

type Queue struct {
	mu       sync.Mutex
	pending  []*Job
	working  bool
	current  *Job
	closed   bool
	dailyCap int
	day      string
	count    int
	wake     chan struct{}
	done     chan struct{}
}

func New(dailyCap int) *Queue {
	if dailyCap < 1 {
		dailyCap = 30
	}
	q := &Queue{dailyCap: dailyCap, day: time.Now().Format("2006-01-02"), wake: make(chan struct{}, 1), done: make(chan struct{})}
	go q.worker()
	return q
}

func (q *Queue) Submit(id string, task Task) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil, fmt.Errorf("queue is closed")
	}
	q.resetDayLocked()
	reserved := q.count + len(q.pending)
	if q.working {
		reserved++
	}
	if reserved >= q.dailyCap {
		return nil, fmt.Errorf("daily download limit reached")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: id, Task: task, Cancel: cancel, Done: make(chan error, 1), ctx: ctx}
	q.pending = append(q.pending, job)
	q.signal()
	return job, nil
}

func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		<-q.done
		return
	}
	q.closed = true
	for _, job := range q.pending {
		job.Cancel()
		job.Done <- context.Canceled
		close(job.Done)
	}
	if q.current != nil {
		q.current.Cancel()
	}
	q.pending = nil
	q.mu.Unlock()
	q.signal()
	<-q.done
}

func (q *Queue) worker() {
	defer close(q.done)
	for {
		q.mu.Lock()
		if q.closed && len(q.pending) == 0 && !q.working {
			q.mu.Unlock()
			return
		}
		if len(q.pending) == 0 || q.working {
			q.mu.Unlock()
			<-q.wake
			continue
		}
		job := q.pending[0]
		q.pending = q.pending[1:]
		q.working = true
		q.current = job
		q.mu.Unlock()

		var err error
		if job.ctx.Err() != nil {
			err = job.ctx.Err()
		} else {
			err = job.Task(job.ctx)
		}
		job.Cancel()
		q.mu.Lock()
		q.working = false
		q.current = nil
		if err == nil {
			q.count++
		}
		job.Done <- err
		close(job.Done)
		q.mu.Unlock()
		q.signal()
	}
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) resetDayLocked() {
	today := time.Now().Format("2006-01-02")
	if q.day != today {
		q.day, q.count = today, 0
	}
}

func (q *Queue) SetLimit(limit int) { q.mu.Lock(); defer q.mu.Unlock(); q.dailyCap = limit }

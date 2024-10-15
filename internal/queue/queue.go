package queue

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

type Job struct {
	Path string
	Run  func()
}

type ErrJob struct {
	Job *Job
	Err error
}

type Option interface {
	OnCreate(description string)
	OnAdd(total int)
	OnDone()
}

type Queue struct {
	jobs    chan *Job
	options []Option
	wg      sync.WaitGroup
	total   int
}

type progressBarOption struct {
	progressBar *progressbar.ProgressBar
}

func (p *progressBarOption) OnCreate(description string) {
	progressbar := progressbar.NewOptions(
		0,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetWidth(10),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(false),
	)

	p.progressBar = progressbar
}

func (p *progressBarOption) OnAdd(total int) {
	max := p.progressBar.GetMax()
	p.progressBar.ChangeMax(max + total)
}

func (p *progressBarOption) OnDone() {
	p.progressBar.Add(1)
}

func WithProgressBar() Option {
	return &progressBarOption{}
}

func NewQueue(description string, opts ...Option) *Queue {
	for _, opt := range opts {
		opt.OnCreate(description)
	}

	return &Queue{
		jobs:    make(chan *Job),
		options: opts,
	}
}

func (q *Queue) AddJobs(jobs []*Job) {
	total := len(jobs)
	q.wg.Add(total)
	q.total = total
	for _, opt := range q.options {
		opt.OnAdd(total)
	}

	for _, pageJob := range jobs {
		go func(job *Job) {
			q.jobs <- job
			q.wg.Done()
		}(pageJob)
	}

	go func() {
		q.wg.Wait()
	}()
}

func (q *Queue) Done() {
	for _, opt := range q.options {
		opt.OnDone()
	}
}

type WorkerPool struct {
	queue *Queue
	pool  chan struct{}
	wg    sync.WaitGroup
}

func NewWorkerPool(q *Queue, workerPoolSize int) *WorkerPool {
	return &WorkerPool{
		queue: q,
		pool:  make(chan struct{}, workerPoolSize),
	}
}

func (w *WorkerPool) DoWork(ctx context.Context) {
	w.wg.Add(w.queue.total)

	go func() {
		for {
			select {
			case <-ctx.Done():
			case job := <-w.queue.jobs:
				w.pool <- struct{}{}
				go func(j *Job) {
					defer w.wg.Done()
					defer func() { <-w.pool }()
					defer w.queue.Done()
					j.Run()
				}(job)
			}
		}
	}()

	w.wg.Wait()
}

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
	Run  func() error
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
	cancel  context.CancelFunc
	ctx     context.Context
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
	ctx, cancel := context.WithCancel(context.Background())

	for _, opt := range opts {
		opt.OnCreate(description)
	}

	return &Queue{
		jobs:    make(chan *Job),
		ctx:     ctx,
		cancel:  cancel,
		options: opts,
	}
}

func (q *Queue) AddJobs(jobs []*Job) {
	total := len(jobs)
	q.wg.Add(total)
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
		q.cancel()
	}()
}

func (q *Queue) Done() {
	for _, opt := range q.options {
		opt.OnDone()
	}
}

type Worker struct {
	Queue     *Queue
	ErrorJobs []ErrJob
}

func (w *Worker) DoWork() bool {
	for {
		select {
		case <-w.Queue.ctx.Done():
			fmt.Print("Finish migrating pages\n")
			return true
		case job := <-w.Queue.jobs:
			err := job.Run()
			if err != nil {
				errJob := ErrJob{
					Job: job,
					Err: err,
				}
				w.ErrorJobs = append(w.ErrorJobs, errJob)
			}
			w.Queue.Done()
		}
	}
}

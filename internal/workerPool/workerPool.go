package workerPool

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

type queue struct {
	jobs chan *Job
	wg   sync.WaitGroup
}

type WorkerPool struct {
	queue     *queue
	pool      chan struct{}
	options   []Option
	wg        sync.WaitGroup
	totalJobs int
}

func New(description string, workerPoolSize int, opts ...Option) *WorkerPool {
	for _, opt := range opts {
		opt.OnCreate(description)
	}

	return &WorkerPool{
		queue: &queue{
			jobs: make(chan *Job),
		},
		options: opts,
		pool:    make(chan struct{}, workerPoolSize),
	}
}

func (w *WorkerPool) AddJobs(jobs []*Job) {
	total := len(jobs)
	w.queue.wg.Add(total)
	w.totalJobs = total

	for _, opt := range w.options {
		opt.OnAdd(total)
	}

	for _, pageJob := range jobs {
		go func(job *Job) {
			w.queue.jobs <- job
			w.queue.wg.Done()
		}(pageJob)
	}

	go func() {
		w.queue.wg.Wait()
	}()
}

func (w *WorkerPool) DoWork(ctx context.Context) {
	w.wg.Add(w.totalJobs)

	go func() {
		for {
			select {
			case <-ctx.Done():
			case job := <-w.queue.jobs:
				w.pool <- struct{}{}
				go func(j *Job) {
					defer w.wg.Done()
					defer func() { <-w.pool }()
					defer func() {
						for _, opt := range w.options {
							opt.OnDone()
						}
					}()
					j.Run()
				}(job)
			}
		}
	}()

	w.wg.Wait()
}

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
	Job Job
	Err error
}

type Queue struct {
	jobs        chan Job
	progressBar *progressbar.ProgressBar
	wg          sync.WaitGroup
	cancel      context.CancelFunc
	ctx         context.Context
}

func NewQueue(description string) *Queue {
	ctx, cancel := context.WithCancel(context.Background())

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

	return &Queue{
		jobs:        make(chan Job),
		ctx:         ctx,
		cancel:      cancel,
		progressBar: progressbar,
	}
}

func (q *Queue) AddJobs(jobs []Job) {
	total := len(jobs)
	q.wg.Add(total)
	max := q.progressBar.GetMax()
	q.progressBar.ChangeMax(max + total)

	for _, pageJob := range jobs {
		go func(job Job) {
			q.jobs <- job
			if q.progressBar != nil {
				q.progressBar.Add(1)
			}
			q.wg.Done()
		}(pageJob)
	}

	go func() {
		q.wg.Wait()
		q.cancel()
	}()
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
				continue
			}
		}
	}
}

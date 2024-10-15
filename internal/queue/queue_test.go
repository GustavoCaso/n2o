package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/stretchr/testify/assert"
)

type testprogressBarOption struct {
	progressBar *progressbar.ProgressBar
	buf         *strings.Builder
}

func (p *testprogressBarOption) OnCreate(description string) {
	buf := &strings.Builder{}
	progress := progressbar.NewOptions(0, progressbar.OptionSetWriter(buf))

	p.progressBar = progress
	p.buf = buf
}

func (p *testprogressBarOption) OnAdd(total int) {
	max := p.progressBar.GetMax()
	p.progressBar.ChangeMax(max + total)
}

func (p *testprogressBarOption) OnDone() {
	p.progressBar.Add(1)
}

func TestQueue(t *testing.T) {
	option := &testprogressBarOption{}
	queue := NewQueue("testing", option)

	job1 := &Job{
		Path: "failed",
		Run: func() {
		},
	}

	job2 := &Job{
		Path: "success",
		Run: func() {
		},
	}

	jobs := []*Job{
		job1,
		job2,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	queue.AddJobs(jobs)

	workerPool := NewWorkerPool(queue, 10)

	workerPool.DoWork(ctx)

	result := strings.TrimSpace(option.buf.String())

	assert.True(t, strings.Contains(result, "50% |████████████████████                    |  [0s:0s]"))
	assert.True(t, strings.Contains(result, "100% |████████████████████████████████████████|"))
}

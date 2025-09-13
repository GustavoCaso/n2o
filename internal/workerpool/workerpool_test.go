package workerpool

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

func (p *testprogressBarOption) OnCreate(string) {
	buf := &strings.Builder{}
	progress := progressbar.NewOptions(0, progressbar.OptionSetWriter(buf))

	p.progressBar = progress
	p.buf = buf
}

func (p *testprogressBarOption) OnAdd(total int) {
	p.progressBar.ChangeMax(p.progressBar.GetMax() + total)
}

func (p *testprogressBarOption) OnDone() {
	p.progressBar.Add(1)
}

func TestWorkerPool(t *testing.T) {
	option := &testprogressBarOption{}
	pool := New("testing", 10, option)

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

	pool.AddJobs(jobs)

	pool.DoWork(ctx)

	result := strings.TrimSpace(option.buf.String())

	assert.Contains(t, result, "50% |████████████████████                    |  [0s:0s]")
	assert.Contains(t, result, "100% |████████████████████████████████████████|")
}

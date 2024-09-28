package queue

import (
	"errors"
	"strings"
	"testing"

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

	errJob := &Job{
		Path: "failed",
		Run: func() error {
			return errors.New("failed")
		},
	}

	successJob := &Job{
		Path: "success",
		Run: func() error {
			return nil
		},
	}

	jobs := []*Job{
		successJob,
		errJob,
	}
	queue.AddJobs(jobs)

	worker := Worker{
		Queue: queue,
	}

	worker.DoWork()

	result := strings.TrimSpace(option.buf.String())

	assert.True(t, strings.Contains(result, "50% |████████████████████                    |  [0s:0s]"))
	assert.True(t, strings.Contains(result, "100% |████████████████████████████████████████|"))

	assert.Equal(t, errJob, worker.ErrorJobs[0].Job)
}

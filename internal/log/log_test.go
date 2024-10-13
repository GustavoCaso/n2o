package log

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo(t *testing.T) {
	logger, buffer := MockLogger()

	logger.Info("Testing")
	bytes, err := io.ReadAll(buffer)
	assert.NoError(t, err)
	result := string(bytes)
	assert.Contains(t, result, "[INFO]")
	assert.Contains(t, result, "Testing")
}

func TestWarn(t *testing.T) {
	logger, buffer := MockLogger()

	logger.Warn("Testing")
	bytes, err := io.ReadAll(buffer)
	assert.NoError(t, err)
	result := string(bytes)
	assert.Contains(t, result, "[WARNING]")
	assert.Contains(t, result, "Testing")
}

func TestError(t *testing.T) {
	originalStderr := os.Stderr
	r, wr, _ := os.Pipe()
	os.Stderr = wr

	logger, _ := MockLogger()

	logger.Error("Testing")
	wr.Close()

	bytes, err := io.ReadAll(r)
	assert.NoError(t, err)
	result := string(bytes)
	assert.Contains(t, result, "[ERROR]")
	assert.Contains(t, result, "Testing")

	os.Stderr = originalStderr
}

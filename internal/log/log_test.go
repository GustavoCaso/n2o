package log

import (
	"io"
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
	logger, buffer := MockLogger()

	logger.Error("Testing")
	bytes, err := io.ReadAll(buffer)
	assert.NoError(t, err)
	result := string(bytes)
	assert.Contains(t, result, "[ERROR]")
	assert.Contains(t, result, "Testing")
}

package log

// go:build test

import (
	"bytes"
	"io"
)

func MockLogger() (Log, io.ReadWriter) {
	buf := bytes.Buffer{}
	return New(&buf), &buf
}

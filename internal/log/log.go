package log

import (
	"io"
	"log"
)

type Log interface {
	Info(msd string)
	Warn(msg string)
	Error(msg string)
}

type logger struct {
	out   io.Writer
	war   *log.Logger
	info  *log.Logger
	error *log.Logger
}

func (l *logger) Info(msg string) {
	l.info.Println(msg)
}
func (l *logger) Warn(msg string) {
	l.war.Println(msg)
}

func (l *logger) Error(msg string) {
	l.error.Println(msg)
}

func New(out io.Writer) Log {
	return &logger{
		out:   out,
		war:   log.New(out, "[WARNING] ", log.LstdFlags),
		info:  log.New(out, "[INFO] ", log.LstdFlags),
		error: log.New(out, "[ERROR] ", log.LstdFlags),
	}
}

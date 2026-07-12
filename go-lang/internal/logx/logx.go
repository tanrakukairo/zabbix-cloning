package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Logger struct {
	level  int
	quiet  bool
	debug  *log.Logger
	info   *log.Logger
	warn   *log.Logger
	error  *log.Logger
	closer io.Closer
}

var levels = map[string]int{
	"DEBUG": 0, "INFO": 1, "WARNING": 2, "ERROR": 3, "CRITICAL": 4,
}

func New(name, level, filename string, quiet bool) (*Logger, error) {
	var outputs []io.Writer
	var closer io.Closer
	if !quiet {
		outputs = append(outputs, os.Stdout)
	}
	if filename != "" {
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, file)
		closer = file
	}
	if len(outputs) == 0 {
		outputs = append(outputs, io.Discard)
	}
	out := io.MultiWriter(outputs...)
	prefix := name + " "
	flags := log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC
	return &Logger{
		level: levels[strings.ToUpper(level)], quiet: quiet, closer: closer,
		debug: log.New(out, prefix+"[DEBUG] ", flags|log.Lshortfile),
		info:  log.New(out, prefix+"[INFO] ", flags),
		warn:  log.New(out, prefix+"[WARNING] ", flags),
		error: log.New(out, prefix+"[ERROR] ", flags|log.Lshortfile),
	}, nil
}

func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

func (l *Logger) Debugf(format string, args ...any) {
	if l.level <= 0 {
		_ = l.debug.Output(2, fmt.Sprintf(format, args...))
	}
}
func (l *Logger) Infof(format string, args ...any) {
	if l.level <= 1 {
		l.info.Printf(format, args...)
	}
}
func (l *Logger) Warningf(format string, args ...any) {
	if l.level <= 2 {
		l.warn.Printf(format, args...)
	}
}
func (l *Logger) Errorf(format string, args ...any) {
	if l.level <= 3 {
		_ = l.error.Output(2, fmt.Sprintf(format, args...))
	}
}
func (l *Logger) Progress(format string, args ...any) {
	if !l.quiet {
		fmt.Printf(format, args...)
	}
}
